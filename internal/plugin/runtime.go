package plugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Limits on what a plugin's server part may use.
const (
	memoryLimitPages = 16384 // 1 GiB, in 64 KiB pages
	maxMessage       = 64 << 20
	maxFetchBody     = 32 << 20
	callTimeout      = 2 * time.Minute
)

// runtime runs plugins' server modules. One wazero runtime is shared; each
// plugin's module is compiled once and instantiated per use, with only the
// folders that use needs.
type runtime struct {
	rt     wazero.Runtime
	client *http.Client
	log    *slog.Logger
}

func newRuntime(ctx context.Context, cacheDir string, client *http.Client, log *slog.Logger) (*runtime, error) {
	cfg := wazero.NewRuntimeConfig().
		WithMemoryLimitPages(memoryLimitPages).
		WithCloseOnContextDone(true)
	if cacheDir != "" {
		if cache, err := wazero.NewCompilationCacheWithDir(cacheDir); err == nil {
			cfg = cfg.WithCompilationCache(cache)
		}
	}
	rt := wazero.NewRuntimeWithConfig(ctx, cfg)
	r := &runtime{rt: rt, client: client, log: log}
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		rt.Close(ctx)
		return nil, err
	}
	_, err := rt.NewHostModuleBuilder("matinee").
		NewFunctionBuilder().WithFunc(r.hostLog).Export("log").
		NewFunctionBuilder().WithFunc(r.hostFetch).Export("fetch").
		NewFunctionBuilder().WithFunc(r.hostTake).Export("take").
		Instantiate(ctx)
	if err != nil {
		rt.Close(ctx)
		return nil, err
	}
	return r, nil
}

func (r *runtime) close(ctx context.Context) error { return r.rt.Close(ctx) }

// compile checks a server module and gets it ready to run.
func (r *runtime) compile(ctx context.Context, wasm []byte) (wazero.CompiledModule, error) {
	mod, err := r.rt.CompileModule(ctx, wasm)
	if err != nil {
		return nil, err
	}
	exports := mod.ExportedFunctions()
	for _, name := range []string{"matinee_malloc", "matinee_free", "matinee_handle"} {
		if _, ok := exports[name]; !ok {
			mod.Close(ctx)
			return nil, fmt.Errorf("the server module doesn't export %s; build it with the Matinee plugin SDK", name)
		}
	}
	return mod, nil
}

// mount is a host folder a plugin instance sees.
type mount struct {
	host, guest string
	writable    bool
}

// instance is one running copy of a plugin's server module. Calls to it
// take turns.
type instance struct {
	p   *Plugin
	mod api.Module
	mu  sync.Mutex
	// pending is a fetch's answer, waiting for the plugin to take it.
	pending []byte
	mounts  []mount
}

type instanceKey struct{}

func (r *runtime) instantiate(ctx context.Context, p *Plugin, mounts []mount) (*instance, error) {
	in := &instance{p: p, mounts: mounts}
	fsc := wazero.NewFSConfig()
	for _, m := range mounts {
		if m.writable {
			fsc = fsc.WithDirMount(m.host, m.guest)
		} else {
			fsc = fsc.WithReadOnlyDirMount(m.host, m.guest)
		}
	}
	out := &logWriter{log: r.log, plugin: p.ID}
	cfg := wazero.NewModuleConfig().
		WithName("").
		WithStartFunctions("_initialize").
		WithFSConfig(fsc).
		WithStdout(out).WithStderr(out).
		WithSysWalltime().WithSysNanotime().WithSysNanosleep().
		WithRandSource(rand.Reader).
		WithArgs(p.ID).
		WithEnv("MATINEE_PLUGIN", p.ID)
	ictx, cancel := context.WithTimeout(context.WithValue(ctx, instanceKey{}, in), callTimeout)
	defer cancel()
	mod, err := r.rt.InstantiateModule(ictx, p.compiled, cfg)
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", p.ID, err)
	}
	in.mod = mod
	return in, nil
}

func (in *instance) close(ctx context.Context) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.mod.Close(ctx)
}

type wireRequest struct {
	Method string `json:"method"`
	Params any    `json:"params"`
}

type wireResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// errCrashed is a call that ended the instance, such as a panic outside the
// SDK's recover or running out of memory. The instance can't be used again.
var errCrashed = errors.New("the plugin stopped working")

// call sends one request to the plugin and decodes its answer into out
// (which may be nil).
func (in *instance) call(ctx context.Context, method string, params, out any) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.mod.IsClosed() {
		return errCrashed
	}
	req, err := json.Marshal(wireRequest{method, params})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.WithValue(ctx, instanceKey{}, in), callTimeout)
	defer cancel()
	malloc := in.mod.ExportedFunction("matinee_malloc")
	free := in.mod.ExportedFunction("matinee_free")
	handle := in.mod.ExportedFunction("matinee_handle")
	res, err := malloc.Call(ctx, uint64(len(req)))
	if err != nil {
		return in.crashed(err)
	}
	ptr := uint32(res[0])
	if !in.mod.Memory().Write(ptr, req) {
		return fmt.Errorf("%s: request out of memory bounds", in.p.ID)
	}
	res, err = handle.Call(ctx, uint64(ptr), uint64(len(req)))
	if err != nil {
		return in.crashed(err)
	}
	rptr, rlen := uint32(res[0]>>32), uint32(res[0])
	if rlen > maxMessage {
		return fmt.Errorf("%s: answer too large", in.p.ID)
	}
	b, ok := in.mod.Memory().Read(rptr, rlen)
	if !ok {
		return fmt.Errorf("%s: answer out of memory bounds", in.p.ID)
	}
	var wr wireResponse
	err = json.Unmarshal(b, &wr) // b is the plugin's memory: decode before freeing
	if _, ferr := free.Call(ctx, uint64(rptr)); ferr != nil {
		return in.crashed(ferr)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", in.p.ID, err)
	}
	if wr.Error != "" {
		return fmt.Errorf("%s: %s", in.p.ID, wr.Error)
	}
	if out != nil && len(wr.Result) > 0 && string(wr.Result) != "null" {
		return json.Unmarshal(wr.Result, out)
	}
	return nil
}

func (in *instance) crashed(err error) error {
	in.mod.Close(context.Background())
	return fmt.Errorf("%w: %s: %v", errCrashed, in.p.ID, err)
}

func callerInstance(ctx context.Context) *instance {
	in, _ := ctx.Value(instanceKey{}).(*instance)
	return in
}

func (r *runtime) hostLog(ctx context.Context, m api.Module, level, ptr, size uint32) {
	in := callerInstance(ctx)
	b, ok := m.Memory().Read(ptr, min(size, 64<<10))
	if in == nil || !ok {
		return
	}
	lvl := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}[min(level, 3)]
	r.log.Log(ctx, lvl, string(b), "plugin", in.p.ID)
}

type fetchRequest struct {
	Method  string            `json:"method,omitempty"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`
}

type fetchResponse struct {
	Status  int               `json:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// hostFetch makes an HTTP request for the plugin and returns the length of
// the answer, which the plugin then copies out with take.
func (r *runtime) hostFetch(ctx context.Context, m api.Module, ptr, size uint32) uint32 {
	in := callerInstance(ctx)
	if in == nil {
		return 0
	}
	var res fetchResponse
	b, ok := m.Memory().Read(ptr, size)
	var req fetchRequest
	switch {
	case !ok || size > maxMessage:
		res.Error = "request out of memory bounds"
	default:
		if err := json.Unmarshal(b, &req); err != nil {
			res.Error = err.Error()
		} else {
			res = r.fetch(ctx, in.p, req)
		}
	}
	in.pending, _ = json.Marshal(res)
	return uint32(len(in.pending))
}

func (r *runtime) hostTake(ctx context.Context, m api.Module, ptr uint32) {
	in := callerInstance(ctx)
	if in == nil {
		return
	}
	m.Memory().Write(ptr, in.pending)
	in.pending = nil
}

func (r *runtime) fetch(ctx context.Context, p *Plugin, req fetchRequest) fetchResponse {
	u, err := url.Parse(req.URL)
	switch {
	case err != nil:
		return fetchResponse{Error: err.Error()}
	case u.Scheme != "https":
		return fetchResponse{Error: "only https URLs can be fetched"}
	case !p.Permissions.AllowsHost(u.Hostname()):
		return fetchResponse{Error: fmt.Sprintf("%s isn't in the plugin's network permissions", u.Hostname())}
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	hreq, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(req.Body))
	if err != nil {
		return fetchResponse{Error: err.Error()}
	}
	for k, v := range req.Headers {
		hreq.Header.Set(k, v)
	}
	if hreq.Header.Get("User-Agent") == "" {
		hreq.Header.Set("User-Agent", "Matinee plugin "+p.ID+"/"+p.Version)
	}
	hres, err := r.client.Do(hreq)
	if err != nil {
		return fetchResponse{Error: err.Error()}
	}
	defer hres.Body.Close()
	body, err := io.ReadAll(io.LimitReader(hres.Body, maxFetchBody+1))
	if err != nil {
		return fetchResponse{Error: err.Error()}
	}
	if len(body) > maxFetchBody {
		return fetchResponse{Error: "response larger than 32 MB"}
	}
	headers := map[string]string{}
	for k := range hres.Header {
		headers[strings.ToLower(k)] = hres.Header.Get(k)
	}
	return fetchResponse{Status: hres.StatusCode, Headers: headers, Body: body}
}

// noRedirectsElsewhere keeps a redirect on the hosts the plugin may reach.
func noRedirectsElsewhere(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("too many redirects")
	}
	in := callerInstance(req.Context())
	if in != nil && (req.URL.Scheme != "https" || !in.p.Permissions.AllowsHost(req.URL.Hostname())) {
		return fmt.Errorf("redirect to %s isn't allowed", req.URL.Host)
	}
	return nil
}

// logWriter sends what a plugin prints to the server's log, a line at a time.
type logWriter struct {
	log    *slog.Logger
	plugin string
	mu     sync.Mutex
	buf    []byte
}

func (w *logWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, b...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.log.Info(string(w.buf[:i]), "plugin", w.plugin)
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > 4096 {
		w.log.Info(string(w.buf), "plugin", w.plugin)
		w.buf = nil
	}
	return len(b), nil
}

// guestPath is where a host folder appears inside the sandbox: the same
// path on Linux and macOS, and /C/Media for C:\Media on Windows.
func guestPath(host string) string {
	host = filepath.Clean(host)
	vol := filepath.VolumeName(host)
	rest := filepath.ToSlash(host[len(vol):])
	if vol != "" {
		return "/" + strings.TrimSuffix(strings.TrimPrefix(filepath.ToSlash(vol), "//"), ":") + rest
	}
	return rest
}

// hostPath turns a path the plugin gave back into a host path, if it's
// inside one of the instance's mounts.
func (in *instance) hostPath(guest string) (string, bool) {
	guest = filepath.ToSlash(filepath.Clean(filepath.FromSlash(guest)))
	for _, m := range in.mounts {
		if guest == m.guest || strings.HasPrefix(guest, strings.TrimSuffix(m.guest, "/")+"/") {
			rel := strings.TrimPrefix(guest, m.guest)
			p := filepath.Join(m.host, filepath.FromSlash(rel))
			if _, err := os.Stat(p); err == nil {
				return p, true
			}
		}
	}
	return "", false
}

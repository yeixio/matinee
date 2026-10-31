// Package download prepares files for offline playback. Unlike streaming, a
// download is a job in the database: it is converted in the background (or
// taken as is), checksummed, and then fetched by the device at its own pace,
// with resumable range requests. Nothing depends on the device staying
// connected while the file is prepared.
package download

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
)

const (
	maxAttempts = 3
	// keepFor is how long a converted file stays on the server after every
	// device that asked for it has the whole file, in case one needs it again.
	keepFor = 7 * 24 * time.Hour
)

// Quality is a conversion preset.
type Quality struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Height   int    `json:"height,omitempty"`   // video is scaled to fit; 0 keeps the original
	VideoBPS int    `json:"videoBps,omitempty"` // peak video bitrate
	AudioBPS int    `json:"audioBps,omitempty"`
}

// Qualities are the choices offered for downloads, best first.
var Qualities = []Quality{
	{Name: store.QualityOriginal, Label: "Original file"},
	{Name: "1080p", Label: "1080p (8 Mbps)", Height: 1080, VideoBPS: 8_000_000, AudioBPS: 192_000},
	{Name: "720p", Label: "720p (4 Mbps)", Height: 720, VideoBPS: 4_000_000, AudioBPS: 160_000},
	{Name: "480p", Label: "480p (1.5 Mbps)", Height: 480, VideoBPS: 1_500_000, AudioBPS: 128_000},
}

func QualityByName(name string) (Quality, bool) {
	for _, q := range Qualities {
		if q.Name == name {
			return q, true
		}
	}
	return Quality{}, false
}

// Manager runs the conversion queue. One file is prepared at a time, at low
// CPU priority, so downloads never get in the way of someone watching.
type Manager struct {
	Store  *store.Store
	FFmpeg string
	Dir    string // converted files
	Log    *slog.Logger
	// SubtitleStream finds the embedded stream of a picture subtitle track
	// to draw into the video, by the track's id.
	SubtitleStream func(ctx context.Context, it store.Item, track string) (int, bool)

	wake chan struct{}

	mu      sync.Mutex
	current string // file being prepared
	cancel  context.CancelFunc
}

func NewManager(st *store.Store, ffmpeg, dir string, log *slog.Logger) *Manager {
	os.MkdirAll(dir, 0o755)
	return &Manager{Store: st, FFmpeg: ffmpeg, Dir: dir, Log: log, wake: make(chan struct{}, 1)}
}

// Plan fills in which file a download of it at quality q will be: the
// original, or a conversion in the cache named after everything that
// affects its contents, so identical requests share it.
func (m *Manager) Plan(d *store.Download, it store.Item, q Quality) {
	d.SourceSize, d.SourceMtime = it.Size, it.Mtime
	if store.IsRemote(it.Path) && q.Name == store.QualityOriginal {
		// An episode on the internet: fetched into the cache as it is.
		sum := sha256.Sum256([]byte(it.Path))
		mime := stream.MimeType(it.Path)
		ext := filepath.Ext(store.EpisodeURL(it.Path))
		if u, err := url.Parse(store.EpisodeURL(it.Path)); err == nil {
			ext = strings.ToLower(filepath.Ext(u.Path))
		}
		if ext == "" || len(ext) > 6 {
			ext = ".mp3"
		}
		d.File, d.MimeType = filepath.Join(m.Dir, hex.EncodeToString(sum[:12])+ext), mime
		return
	}
	if q.Name == store.QualityOriginal {
		d.File, d.MimeType = it.Path, stream.MimeType(it.Path)
		return
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d\x00%s\x00%s", it.Path, it.Size, it.Mtime, q.Name, d.Subtitle)))
	ext, mime := ".mp4", "video/mp4"
	if it.VideoCodec == "" {
		ext, mime = ".m4a", "audio/mp4"
	}
	d.File, d.MimeType = filepath.Join(m.Dir, hex.EncodeToString(sum[:12])+ext), mime
}

// Converted says whether a download's file is one Matinee made (and may delete).
func (m *Manager) Converted(d store.Download) bool {
	return filepath.Dir(d.File) == filepath.Clean(m.Dir)
}

// Wake tells the queue there's new work.
func (m *Manager) Wake() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Forget stops preparing and deletes a download's file once no download needs it.
func (m *Manager) Forget(ctx context.Context, d store.Download) {
	if inUse, err := m.Store.FileInUse(ctx, d.File); err != nil || inUse {
		return
	}
	m.mu.Lock()
	if m.current == d.File && m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
	if m.Converted(d) {
		os.Remove(d.File)
	}
}

// Run works through the queue until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	if err := m.Store.RequeueInterrupted(ctx); err != nil {
		m.Log.Error("downloads: requeue", "err", err)
	}
	m.recheck(ctx)
	m.cleanUp(ctx)
	retry := time.NewTicker(30 * time.Second)
	defer retry.Stop()
	clean := time.NewTicker(time.Hour)
	defer clean.Stop()
	for {
		for ctx.Err() == nil {
			d, err := m.Store.NextDownload(ctx)
			if errors.Is(err, store.ErrNotFound) {
				break
			}
			if err != nil {
				m.Log.Error("downloads: next", "err", err)
				break
			}
			m.prepare(ctx, d)
		}
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
		case <-retry.C:
		case <-clean.C:
			m.cleanUp(ctx)
		}
	}
}

func (m *Manager) prepare(ctx context.Context, d store.Download) {
	jobCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.current, m.cancel = d.File, cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		m.current, m.cancel = "", nil
		m.mu.Unlock()
	}()

	log := m.Log.With("download", d.ID, "item", d.ItemID, "quality", d.Quality)
	log.Info("download preparing")
	start := time.Now()
	sum, mtime, err := m.build(jobCtx, d)
	if ctx.Err() != nil {
		return // shutting down; RequeueInterrupted picks it up next time
	}
	if jobCtx.Err() != nil {
		log.Info("download cancelled")
		return
	}
	if err != nil {
		log.Warn("download failed", "err", err)
		if err := m.Store.DownloadFailed(ctx, d.File, err.Error(), maxAttempts); err != nil {
			log.Error("downloads: record failure", "err", err)
		}
		return
	}
	if err := m.Store.DownloadPrepared(ctx, d.File, sum.size, sum.sha256, sum.md5, mtime); err != nil {
		log.Error("downloads: record ready", "err", err)
		return
	}
	log.Info("download ready", "size", sum.size, "took", time.Since(start).Round(time.Second))
}

// build makes the file if needed and checksums it. For an original it also
// returns the mtime the checksum is for, which the file is served against.
func (m *Manager) build(ctx context.Context, d store.Download) (sums, int64, error) {
	if !m.Converted(d) {
		before, err := os.Stat(d.File)
		if err != nil {
			return sums{}, 0, errors.New("the file isn't available on the server right now")
		}
		sum, err := m.checksum(ctx, d.File, 0, 1000)
		if err != nil {
			return sums{}, 0, err
		}
		if after, err := os.Stat(d.File); err != nil || !after.ModTime().Equal(before.ModTime()) {
			return sums{}, 0, errors.New("the file changed while it was being read")
		}
		return sum, before.ModTime().Unix(), nil
	}
	if _, err := os.Stat(d.File); err == nil {
		// Made earlier and still on disk (a download of it was removed and asked for again).
		sum, err := m.checksum(ctx, d.File, 0, 1000)
		return sum, 0, err
	}
	it, err := m.Store.Item(ctx, 0, d.ItemID)
	if err != nil {
		return sums{}, 0, fmt.Errorf("item: %w", err)
	}
	q, ok := QualityByName(d.Quality)
	if !ok {
		return sums{}, 0, fmt.Errorf("unknown quality %q", d.Quality)
	}
	burn := -1
	if d.Subtitle != "" && m.SubtitleStream != nil {
		if burn, ok = m.SubtitleStream(ctx, it, d.Subtitle); !ok {
			return sums{}, 0, errors.New("that subtitle track is gone")
		}
	}
	part := d.File + ".part"
	defer os.Remove(part)
	if store.IsRemote(it.Path) && q.Name == store.QualityOriginal {
		err = m.fetch(ctx, it, part)
	} else {
		err = m.convert(ctx, it, q, burn, part)
	}
	if err != nil {
		return sums{}, 0, err
	}
	// Only a finished conversion gets the real name, so a file under that
	// name is always whole.
	if err := os.Rename(part, d.File); err != nil {
		return sums{}, 0, err
	}
	// Converting is 95% of the work; checksumming the result is the rest.
	sum, err := m.checksum(ctx, d.File, 950, 1000)
	return sum, 0, err
}

// ConvertArgs is the FFmpeg command line converting it to a single MP4 (or
// M4A, for music) at quality q, writing progress to stdout.
func ConvertArgs(it store.Item, q Quality, burnSub int, out string) []string {
	args := append([]string{"-hide_banner", "-loglevel", "error", "-nostdin", "-nostats", "-progress", "pipe:1"}, stream.Input(it)...)
	if it.VideoCodec == "" {
		args = append(args, "-vn", "-map", "0:a:0", "-c:a", "aac", "-b:a", "256k", "-movflags", "+faststart", "-f", "ipod", "-y", out)
		return args
	}
	scale := fmt.Sprintf("scale=-2:'min(%d,ih)'", q.Height)
	encode := []string{"-c:v", "libx264", "-preset", "medium", "-crf", "21",
		"-maxrate", strconv.Itoa(q.VideoBPS), "-bufsize", strconv.Itoa(2 * q.VideoBPS),
		"-pix_fmt", "yuv420p", "-profile:v", "high"}
	switch {
	case burnSub >= 0:
		graph := fmt.Sprintf("[0:s:%d][0:v:0]scale2ref[sub][vid];[vid][sub]overlay=eof_action=pass,%s[v]", burnSub, scale)
		args = append(args, "-filter_complex", graph, "-map", "[v]", "-map", "0:a:0?")
		args = append(args, encode...)
	case canCopyVideo(it, q):
		// Already small enough: keep the picture as is, which is quick and lossless.
		args = append(args, "-map", "0:v:0", "-map", "0:a:0?", "-c:v", "copy")
	default:
		args = append(args, "-map", "0:v:0", "-map", "0:a:0?", "-vf", scale)
		args = append(args, encode...)
	}
	args = append(args, "-sn", "-c:a", "aac", "-ac", "2", "-b:a", strconv.Itoa(q.AudioBPS),
		"-movflags", "+faststart", "-f", "mp4", "-y", out)
	return args
}

// canCopyVideo says whether the original H.264 picture already fits quality
// q, judging its bitrate from the file's size (an overestimate, since the
// size includes the audio).
func canCopyVideo(it store.Item, q Quality) bool {
	if it.VideoCodec != "h264" || it.Height == nil || *it.Height > q.Height || it.DurationMs == nil || *it.DurationMs <= 0 {
		return false
	}
	bps := it.Size * 8 * 1000 / *it.DurationMs
	return bps <= int64(q.VideoBPS)*115/100
}

func (m *Manager) convert(ctx context.Context, it store.Item, q Quality, burnSub int, out string) error {
	name, args := m.FFmpeg, ConvertArgs(it, q, burnSub, out)
	if runtime.GOOS != "windows" {
		if nice, err := exec.LookPath("nice"); err == nil {
			name, args = nice, append([]string{"-n", "10", m.FFmpeg}, args...)
		}
	}
	cmd := exec.CommandContext(ctx, name, args...)
	stderr := &tailBuffer{}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}
	var durUs int64
	if it.DurationMs != nil {
		durUs = *it.DurationMs * 1000
	}
	var last time.Time
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		v, ok := strings.CutPrefix(sc.Text(), "out_time_us=")
		if !ok || durUs <= 0 || time.Since(last) < 2*time.Second {
			continue
		}
		if us, err := strconv.ParseInt(v, 10, 64); err == nil && us > 0 {
			last = time.Now()
			m.Store.SetDownloadProgress(ctx, strings.TrimSuffix(out, ".part"), int(min(950, us*950/durUs)))
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if msg := stderr.last(); msg != "" {
			return fmt.Errorf("conversion failed: %s", msg)
		}
		return fmt.Errorf("conversion failed: %w", err)
	}
	return nil
}

// fetch copies an episode on the internet into the cache as it is.
func (m *Manager) fetch(ctx context.Context, it store.Item, out string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, store.EpisodeURL(it.Path), nil)
	if err != nil {
		return err
	}
	resp, err := stream.Remote.Do(req)
	if err != nil {
		return fmt.Errorf("the show's host isn't answering: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the show's host answered %s", resp.Status)
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	total := resp.ContentLength
	if total <= 0 {
		total = it.Size
	}
	var done int64
	last := time.Now()
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			done += int64(n)
			if total > 0 && time.Since(last) > 2*time.Second {
				last = time.Now()
				m.Store.SetDownloadProgress(ctx, strings.TrimSuffix(out, ".part"), int(min(950, done*950/total)))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return f.Close()
}

// sums are a prepared file's checksums.
type sums struct {
	size        int64
	sha256, md5 string
}

// checksum hashes a file, reporting progress between from and to thousandths.
func (m *Manager) checksum(ctx context.Context, path string, from, to int) (sums, error) {
	f, err := os.Open(path)
	if err != nil {
		return sums{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return sums{}, err
	}
	sh, mh := sha256.New(), md5.New()
	h := io.MultiWriter(sh, mh)
	buf := make([]byte, 1<<20)
	var done int64
	last := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			return sums{}, err
		}
		n, err := f.Read(buf)
		h.Write(buf[:n])
		done += int64(n)
		if err == io.EOF {
			break
		}
		if err != nil {
			return sums{}, fmt.Errorf("reading the file: %w", err)
		}
		if time.Since(last) > 2*time.Second && info.Size() > 0 {
			last = time.Now()
			m.Store.SetDownloadProgress(ctx, path, from+int(done*int64(to-from)/info.Size()))
		}
	}
	if done != info.Size() {
		return sums{}, errors.New("the file changed while it was being read")
	}
	return sums{done, hex.EncodeToString(sh.Sum(nil)), hex.EncodeToString(mh.Sum(nil))}, nil
}

// recheck queues ready downloads whose converted file is gone again, as
// after a restart with the download cache on a scratch volume.
func (m *Manager) recheck(ctx context.Context) {
	all, err := m.Store.AllDownloads(ctx)
	if err != nil {
		m.Log.Error("downloads: recheck", "err", err)
		return
	}
	for _, d := range all {
		if d.State != store.DownloadReady || !m.Converted(d) {
			continue
		}
		if _, err := os.Stat(d.File); errors.Is(err, os.ErrNotExist) {
			if err := m.Store.RequeueDownload(ctx, d); err != nil {
				m.Log.Error("downloads: requeue", "download", d.ID, "err", err)
			}
		}
	}
}

// cleanUp deletes converted files nothing needs any more: ones every device
// has had for a while, and leftovers from removed downloads or a crash.
func (m *Manager) cleanUp(ctx context.Context) {
	expired, err := m.Store.ExpireFinishedDownloads(ctx, keepFor)
	if err != nil {
		m.Log.Error("downloads: expire", "err", err)
		return
	}
	for _, f := range expired {
		if filepath.Dir(f) == filepath.Clean(m.Dir) {
			os.Remove(f)
		}
	}
	used, err := m.Store.DownloadFiles(ctx)
	if err != nil {
		m.Log.Error("downloads: list files", "err", err)
		return
	}
	entries, _ := os.ReadDir(m.Dir)
	m.mu.Lock()
	current := m.current
	m.mu.Unlock()
	for _, e := range entries {
		p := filepath.Join(m.Dir, e.Name())
		if used[p] || p == current+".part" {
			continue
		}
		os.Remove(p)
	}
}

// tailBuffer keeps the end of FFmpeg's error output for the failure message.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4096 {
		t.buf = t.buf[len(t.buf)-4096:]
	}
	return len(p), nil
}

func (t *tailBuffer) last() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(string(t.buf)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

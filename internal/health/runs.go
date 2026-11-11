package health

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Each run of the server keeps its log in files of its own in the data
// folder's logs folder, so what happened before a crash or a restart (or
// in an earlier container) can still be read after it:
//
//	<id>.log      the run's lines, one JSON object each, as they're logged
//	<id>.old.log  the lines before those, once <id>.log grew past MaxRunBytes
//	<id>.crash    what Go printed when the server crashed (a panic or a
//	              fatal error), if it did
//
// <id> is when the run started, in milliseconds. The first line says which
// version and machine (or pod) it was; the last says it stopped cleanly,
// when it did.
const (
	// KeepRuns is how many runs' logs are kept, this one included.
	KeepRuns = 10
	// MaxRunBytes is how big a run's log grows before it starts a new
	// part; a run keeps its newest two parts.
	MaxRunBytes = 16 << 20
)

// RunInfo describes one run of the server.
type RunInfo struct {
	ID       string `json:"id"`
	Started  int64  `json:"started"`         // ms
	Ended    int64  `json:"ended,omitempty"` // ms: its last line, or when it stopped
	Version  string `json:"version,omitempty"`
	Host     string `json:"host,omitempty"` // the machine or pod it ran on
	Current  bool   `json:"current,omitempty"`
	Clean    bool   `json:"clean,omitempty"`   // it shut down cleanly
	Crashed  bool   `json:"crashed,omitempty"` // Go printed a crash
	Lines    int    `json:"lines"`
	Warnings int    `json:"warnings"`
	Errors   int    `json:"errors"`
}

// header and footer are the first and last lines of a run's file.
type header struct {
	Run     int64  `json:"run"`
	Version string `json:"version,omitempty"`
	Host    string `json:"host,omitempty"`
}

type footer struct {
	Stopped int64 `json:"stopped"`
}

// Runs writes this run's log lines to its file, and reads earlier runs'.
type Runs struct {
	dir string
	id  string

	mu    sync.Mutex
	f     *os.File
	size  int64
	crash *os.File
	err   error // the last write's error, logged once

	cacheMu sync.Mutex
	cache   map[string]cachedRun
}

type cachedRun struct {
	size int64
	mod  time.Time
	info RunInfo
}

// StartRuns starts this run's log file in dir, and has Go write a crash
// there too. Logs that keep their lines in memory pass each one on to it
// (Logs.Keep).
func StartRuns(dir, version string, now time.Time) (*Runs, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	r := &Runs{dir: dir, id: strconv.FormatInt(now.UnixMilli(), 10), cache: map[string]cachedRun{}}
	f, err := os.OpenFile(r.path(".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	r.f = f
	host, _ := os.Hostname()
	r.writeJSON(header{Run: now.UnixMilli(), Version: version, Host: host})
	if c, err := os.OpenFile(r.path(".crash"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
		if debug.SetCrashOutput(c, debug.CrashOptions{}) == nil {
			r.crash = c
		} else {
			c.Close()
			os.Remove(r.path(".crash"))
		}
	}
	r.prune()
	return r, nil
}

// ID is this run's.
func (r *Runs) ID() string { return r.id }

// Dir is where the runs' logs are.
func (r *Runs) Dir() string { return r.dir }

func (r *Runs) path(ext string) string { return filepath.Join(r.dir, r.id+ext) }

// write adds a line to this run's file.
func (r *Runs) write(e Entry) error {
	return r.writeJSON(e)
}

func (r *Runs) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	if r.size > 0 && r.size+int64(len(b)) > MaxRunBytes {
		r.rotate()
	}
	n, err := r.f.Write(b)
	r.size += int64(n)
	if err != nil && r.err == nil {
		r.err = err
		return err
	}
	return nil
}

// rotate keeps the run's file to its newest two parts.
func (r *Runs) rotate() {
	r.f.Close()
	os.Rename(r.path(".log"), r.path(".old.log"))
	f, err := os.OpenFile(r.path(".log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		r.f = nil
		return
	}
	r.f, r.size = f, 0
}

// Close closes the file, noting that the server stopped cleanly when it
// did. A run that never says so crashed, was killed, lost power, or stopped
// with an error.
func (r *Runs) Close(clean bool) {
	if r == nil {
		return
	}
	if clean {
		r.writeJSON(footer{Stopped: time.Now().UnixMilli()})
	}
	r.mu.Lock()
	if r.f != nil {
		r.f.Close()
		r.f = nil
	}
	r.mu.Unlock()
	if r.crash != nil {
		debug.SetCrashOutput(nil, debug.CrashOptions{})
		r.crash.Close()
		os.Remove(r.path(".crash"))
	}
}

// prune deletes the oldest runs' logs past KeepRuns.
func (r *Runs) prune() {
	ids := r.ids()
	if len(ids) <= KeepRuns {
		return
	}
	for _, id := range ids[KeepRuns:] {
		if id == r.id {
			continue
		}
		for _, ext := range []string{".log", ".old.log", ".crash"} {
			os.Remove(filepath.Join(r.dir, id+ext))
		}
	}
}

// ids lists the runs in dir, newest first.
func (r *Runs) ids() []string {
	entries, _ := os.ReadDir(r.dir)
	seen := map[string]bool{}
	var ids []string
	for _, e := range entries {
		id, _, _ := strings.Cut(e.Name(), ".")
		if !validRunID(id) || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, _ := strconv.ParseInt(ids[i], 10, 64)
		b, _ := strconv.ParseInt(ids[j], 10, 64)
		return a > b
	})
	return ids
}

func validRunID(id string) bool {
	if id == "" || len(id) > 20 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// List describes the kept runs, newest (this one) first.
func (r *Runs) List() []RunInfo {
	out := []RunInfo{}
	for _, id := range r.ids() {
		info, ok := r.info(id)
		if ok {
			out = append(out, info)
		}
	}
	return out
}

// Has is whether the run's log is kept.
func (r *Runs) Has(id string) bool {
	if !validRunID(id) {
		return false
	}
	_, err := os.Stat(filepath.Join(r.dir, id+".log"))
	return err == nil
}

func (r *Runs) info(id string) (RunInfo, bool) {
	st, err := os.Stat(filepath.Join(r.dir, id+".log"))
	if err != nil {
		return RunInfo{}, false
	}
	crashed := false
	if c, err := os.Stat(filepath.Join(r.dir, id+".crash")); err == nil && c.Size() > 0 {
		crashed = true
	}
	current := id == r.id
	r.cacheMu.Lock()
	c, ok := r.cache[id]
	r.cacheMu.Unlock()
	if ok && c.size == st.Size() && c.mod.Equal(st.ModTime()) {
		c.info.Crashed = crashed
		return c.info, true
	}
	info := RunInfo{ID: id, Current: current, Crashed: crashed}
	info.Started, _ = strconv.ParseInt(id, 10, 64)
	r.read(id, func(h *header, e *Entry, f *footer) {
		switch {
		case h != nil:
			info.Version, info.Host = h.Version, h.Host
		case f != nil:
			info.Clean, info.Ended = true, f.Stopped
		case e != nil:
			info.Lines++
			info.Ended = e.Time
			switch e.Level {
			case "warn":
				info.Warnings++
			case "error":
				info.Errors++
			}
		}
	})
	if !current {
		r.cacheMu.Lock()
		r.cache[id] = cachedRun{size: st.Size(), mod: st.ModTime(), info: info}
		r.cacheMu.Unlock()
	}
	return info, true
}

// read calls fn with each line of a run's files, oldest first.
func (r *Runs) read(id string, fn func(*header, *Entry, *footer)) {
	for _, name := range []string{id + ".old.log", id + ".log"} {
		f, err := os.Open(filepath.Join(r.dir, name))
		if err != nil {
			continue
		}
		readRun(f, fn)
		f.Close()
	}
}

func readRun(rd io.Reader, fn func(*header, *Entry, *footer)) {
	br := bufio.NewReaderSize(rd, 64<<10)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 1 {
			var probe struct {
				Run     *int64 `json:"run"`
				Stopped *int64 `json:"stopped"`
			}
			// A line cut short by a crash or power loss is skipped.
			if json.Unmarshal(line, &probe) == nil {
				switch {
				case probe.Run != nil:
					var h header
					json.Unmarshal(line, &h)
					fn(&h, nil, nil)
				case probe.Stopped != nil:
					fn(nil, nil, &footer{Stopped: *probe.Stopped})
				default:
					var e Entry
					if json.Unmarshal(line, &e) == nil && e.Seq > 0 {
						fn(nil, &e, nil)
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// Lines returns an earlier run's matching lines, newest first, and whether
// older ones match too.
func (r *Runs) Lines(id string, q Query) ([]Entry, bool, error) {
	if !r.Has(id) {
		return nil, false, errors.New("no such run")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	min := LevelRank(q.Level)
	text := strings.ToLower(strings.TrimSpace(q.Text))
	var all []Entry
	r.read(id, func(_ *header, e *Entry, _ *footer) {
		if e == nil || q.Before > 0 && e.Seq >= q.Before {
			return
		}
		if LevelRank(e.Level) < min || text != "" && !matches(*e, text) {
			return
		}
		all = append(all, *e)
	})
	out := []Entry{}
	for i := len(all) - 1; i >= 0; i-- {
		if len(out) == limit {
			return out, true, nil
		}
		out = append(out, all[i])
	}
	return out, false, nil
}

// maxCrash is how much of a crash's output is shown: the panic and the
// goroutine that crashed come first.
const maxCrash = 256 << 10

// Crash is what Go printed when the run crashed, or "".
func (r *Runs) Crash(id string) string {
	if !validRunID(id) || id == r.id {
		return ""
	}
	f, err := os.Open(filepath.Join(r.dir, id+".crash"))
	if err != nil {
		return ""
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, maxCrash))
	return string(b)
}

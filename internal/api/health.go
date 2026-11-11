package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/analyze"
	"github.com/yeixio/matinee/internal/diagnose"
	"github.com/yeixio/matinee/internal/health"
	"github.com/yeixio/matinee/internal/hwaccel"
	"github.com/yeixio/matinee/internal/live"
	"github.com/yeixio/matinee/internal/netstate"
	"github.com/yeixio/matinee/internal/remote"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
	"github.com/yeixio/matinee/internal/version"
)

// The admin dashboard (Settings → Server health): how busy the server is,
// what's playing and converting, background jobs, problems, and the latest
// log lines. Everything is read on this server; nothing is sent anywhere.

// StoragePath is a folder the server keeps things in, for the dashboard's
// disk space list. Use names it: data, transcode, thumbnails, images or
// downloads.
type StoragePath struct {
	Use  string
	Path string
}

func (s *Server) health() *health.Monitor {
	s.healthOnce.Do(func() {
		if s.Health == nil {
			s.Health = health.NewMonitor(health.NewLogs(0))
		}
		if s.Health.Streams == nil {
			s.Health.Streams = s.StreamCounts
		}
	})
	return s.Health
}

// StreamCounts is how many players are playing now, and how many
// transcodes are running.
func (s *Server) StreamCounts() (playing, transcodes int) {
	for _, c := range s.live().Clients(func(live.Client) bool { return true }) {
		if c.Online && c.Playing != nil && !c.Playing.Paused {
			playing++
		}
	}
	if s.Transcoder != nil {
		transcodes = s.Transcoder.Active()
	}
	return playing, transcodes
}

type healthServer struct {
	Name         string         `json:"name"`
	Version      string         `json:"version"`
	APIVersion   int            `json:"apiVersion"`
	State        string         `json:"state"`
	FFmpeg       string         `json:"ffmpeg,omitempty"`
	FFmpegVer    string         `json:"ffmpegVersion,omitempty"`
	PublicURL    string         `json:"publicUrl,omitempty"`
	Remote       *remote.Status `json:"remote,omitempty"`
	ScanInterval int64          `json:"scanIntervalMs"`
	Watching     scan.WatchMode `json:"watching,omitempty"`
	// Internet says which sites outside the server aren't answering, and
	// whether the internet as a whole looks down.
	Internet *netstate.Status `json:"internet,omitempty"`
}

type healthDisk struct {
	health.Disk
	// Reachable is false when the folder can't be read, as when a network
	// share has dropped.
	Reachable bool        `json:"reachable"`
	Uses      []healthUse `json:"uses"`
}

type healthUse struct {
	Use     string `json:"use"`               // data, transcode, thumbnails, images, downloads or library
	Library string `json:"library,omitempty"` // the library's name, for library folders
	Path    string `json:"path"`
}

type healthStream struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Platform string        `json:"platform"`
	App      string        `json:"app,omitempty"`
	Version  string        `json:"version,omitempty"`
	User     live.User     `json:"user"`
	Remote   bool          `json:"remote,omitempty"`
	Playing  *live.Playing `json:"playing"`
	// Stalled is why this stream last stopped to buffer, in the last few
	// minutes (see diagnose.go).
	Stalled *diagnose.Cause `json:"stalled,omitempty"`
}

type healthTranscode struct {
	stream.SessionInfo
	User      string `json:"user,omitempty"`
	Title     string `json:"title,omitempty"`
	ShowTitle string `json:"showTitle,omitempty"`
}

type healthScan struct {
	LibraryID int64            `json:"libraryId"`
	Name      string           `json:"name"`
	Kind      string           `json:"kind"`
	Scanning  bool             `json:"scanning"`
	Since     int64            `json:"since,omitempty"`
	Last      *scan.ScanReport `json:"last,omitempty"`
}

type healthJobs struct {
	Scans     []healthScan    `json:"scans"`
	Analysis  *analyze.Status `json:"analysis,omitempty"`
	Downloads map[string]int  `json:"downloads"`
	Importing bool            `json:"importing,omitempty"`
}

type healthReport struct {
	Server   healthServer        `json:"server"`
	System   health.System       `json:"system"`
	Database *store.DatabaseInfo `json:"database,omitempty"`
	// DatabaseError says why the database couldn't be read.
	DatabaseError string            `json:"databaseError,omitempty"`
	Disks         []healthDisk      `json:"disks"`
	HW            *hwaccel.Status   `json:"hw,omitempty"`
	Streams       []healthStream    `json:"streams"`
	Transcodes    []healthTranscode `json:"transcodes"`
	Jobs          healthJobs        `json:"jobs"`
	Problems      []health.Problem  `json:"problems"`
	LogLines      int               `json:"logLines"`
	LogsSince     int64             `json:"logsSince,omitempty"`
}

func (s *Server) getHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m := s.health()
	out := healthReport{
		Server: healthServer{Name: s.Name, Version: version.Version, APIVersion: APIVersion, State: s.serverState(),
			PublicURL: s.PublicURL, ScanInterval: s.scanInterval(ctx).Milliseconds()},
		System:     m.System(),
		Streams:    []healthStream{},
		Transcodes: []healthTranscode{},
		Problems:   []health.Problem{},
		Disks:      []healthDisk{},
	}
	if s.Transcoder != nil {
		out.Server.FFmpeg = s.Transcoder.FFmpeg
		out.Server.FFmpegVer = s.ffmpegVersion()
	}
	if s.Remote != nil {
		st := s.Remote.Status()
		st.Connect = nil
		out.Server.Remote = &st
	}
	if s.Watcher != nil {
		if mode, err := s.Watcher.Mode(ctx); err == nil {
			out.Server.Watching = mode
		}
	}
	if s.Connections != nil {
		st := s.Connections.Status()
		out.Server.Internet = &st
	}
	if m.Logs != nil {
		// Calls skipped while a site is down are told by Internet, not
		// listed one by one.
		for _, p := range m.Logs.Problems(50) {
			if !strings.Contains(p.Detail, netstate.Marker) {
				out.Problems = append(out.Problems, p)
			}
		}
		out.LogLines, out.LogsSince = m.Logs.Kept()
	}

	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if db, err := s.Store.Health(dbCtx); err != nil {
		out.DatabaseError = err.Error()
	} else {
		out.Database = &db
	}
	if s.HW != nil {
		st := s.HW.Status()
		out.HW = &st
	}

	// What's playing, and what the server converts for it.
	for _, c := range s.live().Clients(func(live.Client) bool { return true }) {
		if !c.Online || c.Playing == nil {
			continue
		}
		st := healthStream{ID: c.ID, Name: c.Name, Platform: c.Platform, App: c.App, Version: c.Version,
			User: c.User, Remote: c.Remote, Playing: c.Playing}
		if cause, ok := s.causes().Latest(c.User.ID, c.Playing.Item.ID, causeShownFor); ok {
			st.Stalled = &cause
		}
		out.Streams = append(out.Streams, st)
	}
	if s.Transcoder != nil {
		names := map[int64]string{}
		for _, t := range s.Transcoder.Sessions() {
			ht := healthTranscode{SessionInfo: t}
			if it, err := s.Store.Item(dbCtx, 0, t.ItemID); err == nil {
				ht.Title, ht.ShowTitle = it.Title, it.ShowTitle
			}
			if name, ok := names[t.UserID]; ok {
				ht.User = name
			} else if u, err := s.Store.UserByID(dbCtx, t.UserID); err == nil {
				names[t.UserID], ht.User = u.Username, u.Username
			}
			out.Transcodes = append(out.Transcodes, ht)
		}
	}

	// Background jobs.
	libs, err := s.Store.Libraries(dbCtx)
	if err != nil && out.DatabaseError == "" {
		out.DatabaseError = err.Error()
	}
	out.Jobs.Scans = []healthScan{}
	for _, l := range libs {
		hs := healthScan{LibraryID: l.ID, Name: l.Name, Kind: l.Kind}
		if s.Scanner != nil {
			since, last := s.Scanner.Report(l.ID)
			hs.Scanning, hs.Last = !since.IsZero(), last
			if hs.Scanning {
				hs.Since = since.UnixMilli()
			}
		}
		out.Jobs.Scans = append(out.Jobs.Scans, hs)
	}
	if s.Analyzer != nil {
		if st, err := s.Analyzer.Status(dbCtx); err == nil {
			out.Jobs.Analysis = &st
		}
	}
	out.Jobs.Downloads = map[string]int{}
	if dls, err := s.Store.AllDownloads(dbCtx); err == nil {
		for _, d := range dls {
			if d.State != store.DownloadExpired {
				out.Jobs.Downloads[d.State]++
			}
		}
	}
	s.imports.mu.Lock()
	out.Jobs.Importing = s.imports.running
	s.imports.mu.Unlock()

	out.Disks = s.disks(s.storageUses(libs))
	writeJSON(w, http.StatusOK, out)
}

// storageUses lists the server's own folders and every library folder, for
// their disk space.
func (s *Server) storageUses(libs []store.Library) []healthUse {
	var uses []healthUse
	for _, p := range s.Storage {
		uses = append(uses, healthUse{Use: p.Use, Path: p.Path})
	}
	for _, l := range libs {
		for _, p := range l.Paths {
			uses = append(uses, healthUse{Use: "library", Library: l.Name, Path: p})
		}
	}
	return uses
}

// disks looks up the space on each folder's disk and puts folders on the
// same disk together.
func (s *Server) disks(uses []healthUse) []healthDisk {
	type result struct {
		d  health.Disk
		ok bool
	}
	// Folders not made yet fall back to the same existing parent, so each
	// distinct path is read once.
	paths := make([]string, len(uses))
	byPath := map[string]*result{}
	for i, u := range uses {
		paths[i] = u.Path
		if u.Use != "library" {
			// The server makes its folders when it first needs them.
			paths[i] = existingParent(u.Path)
		}
		byPath[paths[i]] = &result{}
	}
	var wg sync.WaitGroup
	for path, r := range byPath {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.d, r.ok = s.diskSpace(path)
		}()
	}
	wg.Wait()
	results := make([]result, len(uses))
	for i := range uses {
		results[i] = *byPath[paths[i]]
	}
	out := []healthDisk{}
	for i, u := range uses {
		res := results[i]
		if res.ok {
			if j := slices.IndexFunc(out, func(o healthDisk) bool { return o.Reachable && sameDisk(o.Disk, res.d) }); j >= 0 {
				out[j].Uses = append(out[j].Uses, u)
				continue
			}
		}
		out = append(out, healthDisk{Disk: res.d, Reachable: res.ok, Uses: []healthUse{u}})
	}
	return out
}

// sameDisk says whether two folders' readings are of one disk: the same
// size, and free space a moment apart. Shares of one NAS each report its
// whole disk, but files written between the readings change the free
// space, and comparing it exactly listed (and alerted about) one disk once
// per share.
func sameDisk(a, b health.Disk) bool {
	const drift = 1 << 30 // 1 GiB
	if a.Total != b.Total {
		return false
	}
	return max(a.Free, b.Free)-min(a.Free, b.Free) <= drift
}

// diskWait is how long the dashboard waits for a folder's disk. A network
// share that dropped can hang for minutes.
const diskWait = 2 * time.Second

// diskSpace reads a folder's disk space, giving up after diskWait. A
// folder that's still being read from an earlier ask isn't asked again.
func (s *Server) diskSpace(path string) (health.Disk, bool) {
	s.diskMu.Lock()
	if s.diskBusy == nil {
		s.diskBusy = map[string]bool{}
	}
	if s.diskBusy[path] {
		s.diskMu.Unlock()
		return health.Disk{}, false
	}
	s.diskBusy[path] = true
	s.diskMu.Unlock()
	done := make(chan struct{})
	var d health.Disk
	var ok bool
	go func() {
		d, ok = health.DiskUsage(path)
		s.diskMu.Lock()
		delete(s.diskBusy, path)
		s.diskMu.Unlock()
		close(done)
	}()
	select {
	case <-done:
		return d, ok
	case <-time.After(diskWait):
		return health.Disk{}, false
	}
}

// ffmpegVersion is FFmpeg's version line, asked once.
func (s *Server) ffmpegVersion() string {
	s.ffmpegOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, s.Transcoder.FFmpeg, "-hide_banner", "-version").Output()
		if err != nil {
			return
		}
		line, _, _ := strings.Cut(string(out), "\n")
		// "ffmpeg version 7.1 Copyright (c) ..." → "7.1"
		if f := strings.Fields(line); len(f) >= 3 && f[1] == "version" {
			s.ffmpegVer = f[2]
		}
	})
	return s.ffmpegVer
}

func logQuery(r *http.Request) health.Query {
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit > 1000 {
		limit = 1000
	}
	return health.Query{Level: q.Get("level"), Text: q.Get("q"), Before: before, Limit: limit}
}

// getLogs lists the latest log lines, newest first.
func (s *Server) getLogs(w http.ResponseWriter, r *http.Request) {
	m := s.health()
	if m.Logs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"lines": []health.Entry{}, "more": false})
		return
	}
	lines, more := m.Logs.Lines(logQuery(r))
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines, "more": more})
}

// downloadLogs sends the matching log lines as a text file, oldest first,
// to attach to a bug report.
func (s *Server) downloadLogs(w http.ResponseWriter, r *http.Request) {
	q := logQuery(r)
	q.Limit = health.DefaultLines * 4
	var lines []health.Entry
	if m := s.health(); m.Logs != nil {
		lines, _ = m.Logs.Lines(q)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="matinee-%s.log"`, time.Now().Format("2006-01-02-150405")))
	fmt.Fprintf(w, "# Matinee %s, %s\n", version.Version, time.Now().Format(time.RFC3339))
	for i := len(lines) - 1; i >= 0; i-- {
		e := lines[i]
		var b strings.Builder
		b.WriteString(time.UnixMilli(e.Time).Format("2006-01-02T15:04:05.000Z07:00"))
		b.WriteString(" " + strings.ToUpper(e.Level) + " " + e.Message)
		for _, a := range e.Attrs {
			v := a.Value
			if strings.ContainsAny(v, " \t\n\"=") {
				v = strconv.Quote(v)
			}
			b.WriteString(" " + a.Key + "=" + v)
		}
		b.WriteString("\n")
		w.Write([]byte(b.String()))
	}
}

// stopTranscode stops a running transcode, such as one that's stuck.
func (s *Server) stopTranscode(w http.ResponseWriter, r *http.Request) {
	if s.Transcoder == nil {
		writeError(w, http.StatusNotFound, "no such transcode")
		return
	}
	id := r.PathValue("session")
	for _, t := range s.Transcoder.Sessions() {
		if t.ID == id {
			s.Transcoder.Kill(id)
			s.Log.Info("transcode stopped by an admin", "session", id, "by", currentUser(r).Username)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	writeError(w, http.StatusNotFound, "no such transcode")
}

// existingParent is path, or the nearest folder above it that exists.
func existingParent(path string) string {
	for p := path; ; {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		up := filepath.Dir(p)
		if up == p {
			return path
		}
		p = up
	}
}

// noInternet answers for something that needs the internet and couldn't
// have it: the call was skipped because its site is down (see
// internal/netstate), or the server knows the internet is down. It says
// false, having written nothing, for any other failure.
func (s *Server) noInternet(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, netstate.ErrDown) && (s.Connections == nil || !s.Connections.Status().Offline) {
		return false
	}
	writeError(w, http.StatusServiceUnavailable, "no internet connection right now. Your server is fine; this needs the internet, and works again once it's back.")
	return true
}

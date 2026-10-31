package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/analyze"
	"github.com/yeixio/matinee/internal/artwork"
	"github.com/yeixio/matinee/internal/download"
	"github.com/yeixio/matinee/internal/hwaccel"
	"github.com/yeixio/matinee/internal/podcast"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/spectrum"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
	"github.com/yeixio/matinee/internal/stream"
	"github.com/yeixio/matinee/internal/subtitle"
)

type client struct {
	t     *testing.T
	base  string
	token string
	srv   *Server
}

func (c *client) do(method, path string, body any, out any) int {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, r)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			c.t.Fatalf("%s %s: decode: %v", method, path, err)
		}
	}
	return res.StatusCode
}

// makeMedia writes a short test file with ffmpeg.
func makeMedia(t *testing.T, path string, args ...string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	full := append([]string{"-v", "error", "-y"}, args...)
	full = append(full, path)
	if out, err := exec.Command("ffmpeg", full...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, out)
	}
}

// newTestServer starts a server; opts can change it before it starts.
func newTestServer(t *testing.T, opts ...func(*Server)) (*client, string) {
	t.Helper()
	c, tmp, _ := newTestServerStore(t, opts...)
	return c, tmp
}

// newTestServerStore also returns the server's store, for tests that set up
// what a scan or lookup would.
func newTestServerStore(t *testing.T, opts ...func(*Server)) (*client, string, *store.Store) {
	t.Helper()
	db := storetest.Open(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tmp := t.TempDir()
	hw := hwaccel.NewManager("ffmpeg", log)
	tc := stream.NewTranscoder("ffmpeg", filepath.Join(tmp, "transcode"), hw, log)
	t.Cleanup(tc.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	dl := download.NewManager(db, "ffmpeg", filepath.Join(tmp, "downloads"), log)
	srv := &Server{
		MediaRoot: tmp,
		Store:     db, Scanner: &scan.Scanner{Store: db, Prober: probe.Prober{Path: "ffprobe"}, Log: log},
		Transcoder: tc, HW: hw, Downloads: dl, Art: artwork.NewCache(filepath.Join(tmp, "images"), "ffmpeg"),
		Subtitles: subtitle.NewConverter("ffmpeg", filepath.Join(tmp, "subtitles")), Log: log, BackgroundCtx: ctx,
		Levels: spectrum.New("ffmpeg", filepath.Join(tmp, "levels")),
	}
	dl.SubtitleStream = srv.BurnStream
	srv.Podcasts = &podcast.Manager{Store: db, Log: log}
	srv.Scanner.Refresh = srv.Podcasts.RefreshStale
	// Not running: tests that want it start srv.Analyzer.Run themselves.
	srv.Analyzer = &analyze.Analyzer{Store: db, Prober: probe.Prober{Path: "ffprobe"}, Log: log}
	for _, o := range opts {
		o(srv)
	}
	done := make(chan struct{})
	go func() { dl.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &client{t: t, base: ts.URL, srv: srv}, tmp, db
}

func waitForScan(t *testing.T, c *client) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var libs []libraryView
		c.do("GET", "/api/libraries", nil, &libs)
		busy := false
		for _, l := range libs {
			busy = busy || l.Scanning
		}
		if !busy {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("scan did not finish")
}

func TestEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)

	// Setup creates the admin once.
	var setup struct{ NeedsSetup bool }
	c.do("GET", "/api/setup", nil, &setup)
	if !setup.NeedsSetup {
		t.Fatal("fresh server should need setup")
	}
	if code := c.do("GET", "/api/libraries", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("libraries without a session: got %d", code)
	}
	var login struct{ Token string }
	if code := c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login); code != 200 {
		t.Fatalf("setup: %d", code)
	}
	if code := c.do("POST", "/api/setup", credentials{"evil", "password123"}, nil); code != http.StatusConflict {
		t.Fatalf("second setup should be refused, got %d", code)
	}
	c.token = login.Token

	// Media on disk: an H.264 movie in MP4, an HEVC-free MKV episode (MPEG-4 video), and a tagged FLAC track.
	lavfi := []string{"-f", "lavfi", "-i", "testsrc=duration=10:size=320x240:rate=24", "-f", "lavfi", "-i", "sine=duration=10"}
	makeMedia(t, filepath.Join(tmp, "movies", "Big Test Movie (2021)", "Big Test Movie (2021).mp4"),
		append(lavfi, "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest")...)
	makeMedia(t, filepath.Join(tmp, "tv", "Test Show", "Season 1", "Test Show - S01E01 - Pilot.mkv"),
		append(lavfi, "-c:v", "mpeg4", "-c:a", "mp3", "-shortest")...)
	makeMedia(t, filepath.Join(tmp, "music", "x", "y", "track.flac"),
		"-f", "lavfi", "-i", "sine=duration=3", "-metadata", "artist=Test Artist", "-metadata", "album=Test Album",
		"-metadata", "title=Test Song", "-metadata", "track=4/10")

	for _, lib := range []struct{ name, kind, dir string }{
		{"Movies", "movies", "movies"}, {"TV", "tv", "tv"}, {"Music", "music", "music"},
	} {
		body := map[string]any{"name": lib.name, "kind": lib.kind, "paths": []string{filepath.Join(tmp, lib.dir)}}
		if code := c.do("POST", "/api/libraries", body, nil); code != http.StatusCreated {
			t.Fatalf("create %s library: %d", lib.kind, code)
		}
	}
	waitForScan(t, c)

	var libs []libraryView
	c.do("GET", "/api/libraries", nil, &libs)
	byKind := map[string]int64{}
	for _, l := range libs {
		byKind[l.Kind] = l.ID
	}

	var movies []store.Item
	c.do("GET", "/api/libraries/"+itoa(byKind["movies"])+"/items", nil, &movies)
	if len(movies) != 1 || movies[0].Title != "Big Test Movie" || movies[0].Year == nil || *movies[0].Year != 2021 {
		t.Fatalf("movies: %+v", movies)
	}
	movie := movies[0]
	if movie.VideoCodec != "h264" || movie.DurationMs == nil {
		t.Fatalf("movie was not probed: %+v", movie)
	}

	var shows []store.Item
	c.do("GET", "/api/libraries/"+itoa(byKind["tv"])+"/items", nil, &shows)
	if len(shows) != 1 || shows[0].Title != "Test Show" || shows[0].Kind != "show" {
		t.Fatalf("shows: %+v", shows)
	}
	var seasons, episodes []store.Item
	c.do("GET", "/api/items/"+itoa(shows[0].ID)+"/children", nil, &seasons)
	if len(seasons) != 1 || seasons[0].Title != "Season 1" {
		t.Fatalf("seasons: %+v", seasons)
	}
	c.do("GET", "/api/items/"+itoa(seasons[0].ID)+"/children", nil, &episodes)
	if len(episodes) != 1 || episodes[0].Title != "Pilot" || episodes[0].ShowTitle != "Test Show" || *episodes[0].SeasonIndex != 1 {
		t.Fatalf("episodes: %+v", episodes)
	}

	var artists, albums, tracks []store.Item
	c.do("GET", "/api/libraries/"+itoa(byKind["music"])+"/items", nil, &artists)
	if len(artists) != 1 || artists[0].Title != "Test Artist" {
		t.Fatalf("artists from tags: %+v", artists)
	}
	c.do("GET", "/api/items/"+itoa(artists[0].ID)+"/children", nil, &albums)
	c.do("GET", "/api/items/"+itoa(albums[0].ID)+"/children", nil, &tracks)
	if len(tracks) != 1 || tracks[0].Title != "Test Song" || *tracks[0].IndexNumber != 4 {
		t.Fatalf("tracks: %+v", tracks)
	}
	// A player names what's playing: a track's album and artist, an album's artist.
	if tracks[0].Album != "Test Album" || tracks[0].Artist != "Test Artist" || tracks[0].ArtistID == nil || *tracks[0].ArtistID != artists[0].ID {
		t.Fatalf("track names its album and artist: %+v", tracks[0])
	}
	if albums[0].Artist != "Test Artist" || *albums[0].ArtistID != artists[0].ID || albums[0].Album != "" {
		t.Fatalf("album names its artist: %+v", albums[0])
	}
	// Every track of an artist, of an album, or a track's album.
	for _, id := range []int64{artists[0].ID, albums[0].ID, tracks[0].ID} {
		var all []store.Item
		c.do("GET", "/api/items/"+itoa(id)+"/tracks", nil, &all)
		if len(all) != 1 || all[0].ID != tracks[0].ID {
			t.Fatalf("tracks of %d: %+v", id, all)
		}
	}

	// The visualizer's levels for a song, and none for anything else.
	var levels struct {
		FPS, Bands int
		Levels     []byte
	}
	if code := c.do("GET", "/api/items/"+itoa(tracks[0].ID)+"/levels", nil, &levels); code != 200 || levels.FPS != spectrum.FPS || levels.Bands != spectrum.Bands || len(levels.Levels) == 0 || len(levels.Levels)%levels.Bands != 0 {
		t.Fatalf("levels: %d %d fps, %d bands, %d bytes", code, levels.FPS, levels.Bands, len(levels.Levels))
	}
	if code := c.do("GET", "/api/items/"+itoa(albums[0].ID)+"/levels", nil, nil); code != 400 {
		t.Fatalf("an album's levels: %d, want 400", code)
	}

	// Search by word prefix.
	var found []store.Item
	c.do("GET", "/api/search?q=big+te", nil, &found)
	if len(found) != 1 || found[0].ID != movie.ID {
		t.Fatalf("search: %+v", found)
	}

	// Item detail carries its parents for breadcrumbs.
	var ep itemView
	c.do("GET", "/api/items/"+itoa(episodes[0].ID), nil, &ep)
	if len(ep.Parents) != 2 || ep.Parents[0].Title != "Test Show" {
		t.Fatalf("parents: %+v", ep.Parents)
	}

	// Direct play supports range requests, which makes seeking instant.
	req, _ := http.NewRequest("GET", c.base+"/api/items/"+itoa(movie.ID)+"/file?token="+c.token, nil)
	req.Header.Set("Range", "bytes=0-99")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusPartialContent || res.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("direct play: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}

	// HLS: the MKV episode needs a real transcode.
	var hls struct{ URL string }
	if code := c.do("POST", "/api/items/"+itoa(episodes[0].ID)+"/hls", nil, &hls); code != 200 {
		t.Fatalf("start hls: %d", code)
	}
	playlist := get(t, c, hls.URL)
	if !strings.Contains(playlist, "seg00000.ts") {
		t.Fatalf("playlist has no segments:\n%s", playlist)
	}
	if seg := get(t, c, strings.Replace(hls.URL, "index.m3u8", "seg00000.ts", 1)); len(seg) < 1000 {
		t.Fatalf("segment too small: %d bytes", len(seg))
	}
	if code := c.do("GET", strings.Replace(hls.URL, "index.m3u8", "../../etc/passwd", 1), nil, nil); code == 200 {
		t.Fatal("hls path escaped the session directory")
	}

	// Progress shows up in continue watching, and finishing marks it played.
	c.do("PUT", "/api/items/"+itoa(movie.ID)+"/progress", map[string]any{"positionMs": 3000}, nil)
	var home struct{ ContinueWatching, RecentlyAdded []store.Item }
	c.do("GET", "/api/home", nil, &home)
	if len(home.ContinueWatching) != 1 || home.ContinueWatching[0].PositionMs != 3000 {
		t.Fatalf("continue watching: %+v", home.ContinueWatching)
	}
	if len(home.RecentlyAdded) != 3 {
		t.Fatalf("recently added should list the movie, show and album: %+v", home.RecentlyAdded)
	}
	c.do("PUT", "/api/items/"+itoa(movie.ID)+"/progress", map[string]any{"positionMs": 9800}, nil)
	var watched store.Item
	c.do("GET", "/api/items/"+itoa(movie.ID), nil, &watched)
	if !watched.Played {
		t.Fatal("watching past the credits should mark the movie played")
	}

	// The folder picker opens at the media root when no path is given.
	var root struct {
		Path  string
		Dirs  []string
		Paths []string
	}
	c.do("GET", "/api/fs", nil, &root)
	if root.Path != tmp {
		t.Fatalf("folder picker starts at %q, want the media root %q", root.Path, tmp)
	}
	// Each subfolder comes with its full path, written the server's way.
	if len(root.Dirs) == 0 || len(root.Paths) != len(root.Dirs) || root.Paths[0] != filepath.Join(tmp, root.Dirs[0]) {
		t.Fatalf("folder picker paths: dirs %q, paths %q", root.Dirs, root.Paths)
	}

	// Non-admins can browse but not manage libraries.
	if code := c.do("POST", "/api/users", map[string]any{"username": "kid", "password": "password123"}, nil); code != http.StatusCreated {
		t.Fatalf("create user: %d", code)
	}
	kid := &client{t: t, base: c.base}
	var kidLogin struct{ Token string }
	kid.do("POST", "/api/auth/login", credentials{"kid", "password123"}, &kidLogin)
	kid.token = kidLogin.Token
	if code := kid.do("GET", "/api/libraries", nil, nil); code != 200 {
		t.Fatalf("kid browse: %d", code)
	}
	if code := kid.do("DELETE", "/api/libraries/"+itoa(byKind["movies"]), nil, nil); code != http.StatusForbidden {
		t.Fatalf("kid delete library: %d", code)
	}

	// A library folder that turns up empty looks like an unmounted share, so
	// its items are kept.
	rescanTV := func() {
		c.do("POST", "/api/libraries/"+itoa(byKind["tv"])+"/scan", nil, nil)
		time.Sleep(50 * time.Millisecond)
		waitForScan(t, c)
		c.do("GET", "/api/libraries/"+itoa(byKind["tv"])+"/items", nil, &shows)
	}
	os.RemoveAll(filepath.Join(tmp, "tv", "Test Show"))
	rescanTV()
	if len(shows) != 1 {
		t.Fatalf("an empty library folder should keep its items: %+v", shows)
	}
	// Once the folder holds anything, a deleted file goes on the next scan,
	// along with its empty show.
	os.WriteFile(filepath.Join(tmp, "tv", "notes.txt"), []byte("x"), 0o644)
	rescanTV()
	if len(shows) != 0 {
		t.Fatalf("show should be gone: %+v", shows)
	}
}

func get(t *testing.T, c *client, path string) string {
	t.Helper()
	req, _ := http.NewRequest("GET", c.base+path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("GET %s: %d %s", path, res.StatusCode, b)
	}
	return string(b)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/metadata"
	"github.com/yeixio/matinee/internal/netstate"
	"github.com/yeixio/matinee/internal/store"
)

// internet stands in for the internet: while down, calls hang until their
// caller gives up, the way they do when the connection drops but the home
// network stays up; while up, they reach a fake TMDB.
type internet struct {
	down atomic.Bool
	tmdb *httptest.Server
}

func (n *internet) RoundTrip(req *http.Request) (*http.Response, error) {
	// A connection that hangs goes through if the line comes back first.
	for n.down.Load() {
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	u, _ := url.Parse(n.tmdb.URL)
	r := req.Clone(req.Context())
	r.URL.Scheme, r.URL.Host, r.Host = u.Scheme, u.Host, ""
	return http.DefaultTransport.RoundTrip(r)
}

// With no internet, everything that doesn't need it keeps working at full
// speed: signing in, scanning new files, browsing, search, playback and
// admin settings. Lookups wait, and catch up by themselves once the
// connection is back.
func TestWorksWithNoInternet(t *testing.T) {
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/3/search/movie") {
			json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"id": 949, "overview": "Found once the internet was back."}}})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(tmdb.Close)
	net := &internet{tmdb: tmdb}
	net.down.Store(true)
	tracker := netstate.New()
	tracker.FirstWait = 300 * time.Millisecond
	client := &http.Client{Transport: tracker.Wrap(net), Timeout: 2 * time.Second}
	tm := metadata.NewTMDB("key")
	tm.BaseURL, tm.ImageURL, tm.Client = "https://api.themoviedb.org/3", "https://image.tmdb.org/t/p/", client
	fetcher := &metadata.Fetcher{Providers: []metadata.Provider{tm}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	c, tmp, db := newTestServerStore(t, func(s *Server) {
		fetcher.Store = s.Store
		s.Store.SetMetadataKinds(fetcher.Kinds()...)
		s.Scanner.AfterScan = fetcher.FetchLibrary
		s.Art.Client = client
		s.TrailerSource, s.People, s.Keywords, s.Matcher = tm, tm, tm, tm
		s.Connections = tracker
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	tracker.Back = func() {
		libs, _ := db.Libraries(ctx)
		for _, lib := range libs {
			fetcher.FetchLibrary(ctx, lib)
		}
	}
	go tracker.Run(ctx, net)

	// quick runs step and fails the test if it took longer than limit.
	quick := func(what string, limit time.Duration, step func()) {
		t.Helper()
		start := time.Now()
		step()
		if took := time.Since(start); took > limit {
			t.Errorf("%s took %s with no internet, want under %s", what, took.Round(time.Millisecond), limit)
		}
	}

	var login struct{ Token string }
	quick("setup and sign-in", 3*time.Second, func() {
		if code := c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login); code != 200 {
			t.Fatalf("setup: %d", code)
		}
	})
	c.token = login.Token

	lavfi := []string{"-f", "lavfi", "-i", "testsrc=duration=4:size=320x240:rate=24", "-f", "lavfi", "-i", "sine=duration=4",
		"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest"}
	for _, name := range []string{"Heat (1995)", "Ronin (1998)", "Collateral (2004)"} {
		makeMedia(t, filepath.Join(tmp, "movies", name, name+".mp4"), lavfi...)
	}
	// The first lookup waits out its timeout; the rest fail at once.
	quick("a scan of three new movies", 10*time.Second, func() {
		body := map[string]any{"name": "Movies", "kind": "movies", "paths": []string{filepath.Join(tmp, "movies")}}
		if code := c.do("POST", "/api/libraries", body, nil); code != http.StatusCreated {
			t.Fatalf("create library: %d", code)
		}
		waitForScan(t, c)
	})
	var libs []libraryView
	c.do("GET", "/api/libraries", nil, &libs)
	var movies []store.Item
	quick("browsing a library", 2*time.Second, func() {
		c.do("GET", "/api/libraries/"+itoa(libs[0].ID)+"/items", nil, &movies)
	})
	if len(movies) != 3 || movies[0].Title != "Collateral" {
		t.Fatalf("movies go by their file names: %+v", movies)
	}
	heat := movies[1]

	// Artwork found online earlier but never downloaded.
	db.ApplyMetadata(ctx, movies[2].ID, &store.Metadata{Poster: "https://image.tmdb.org/t/p/w500/ronin.jpg"})
	db.ApplyMetadata(ctx, movies[2].ID, nil)
	quick("Home, an item page, search, trailers and a poster", 5*time.Second, func() {
		c.do("GET", "/api/home", nil, nil)
		for range 3 {
			c.do("GET", "/api/items/"+itoa(heat.ID), nil, nil)
			c.do("GET", "/api/items/"+itoa(heat.ID)+"/trailers", nil, nil)
			c.do("GET", "/api/search?q=ron", nil, nil)
			c.do("GET", "/api/items/"+itoa(movies[2].ID)+"/image/poster", nil, nil)
		}
	})

	quick("starting playback", 3*time.Second, func() {
		req, _ := http.NewRequest("GET", c.base+"/api/items/"+itoa(heat.ID)+"/file?token="+c.token, nil)
		req.Header.Set("Range", "bytes=0-99")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusPartialContent {
			t.Fatalf("direct play: %d", res.StatusCode)
		}
	})
	quick("changing a setting", 2*time.Second, func() {
		if code := c.do("PUT", "/api/settings", map[string]any{"holidayRows": false}, nil); code != 200 {
			t.Fatalf("settings: %d", code)
		}
	})

	// The health page says what's going on, without a lookup error per title.
	var report healthReport
	c.do("GET", "/api/admin/health", nil, &report)
	if in := report.Server.Internet; in == nil || !in.Offline || len(in.Down) == 0 {
		t.Fatalf("internet: %+v", in)
	}
	for _, p := range report.Problems {
		if strings.Contains(p.Detail, netstate.Marker) {
			t.Fatalf("listed as a problem: %+v", p)
		}
	}

	// Back online: details are filled in without a rescan.
	net.down.Store(false)
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		it, _ := db.Item(ctx, 0, heat.ID)
		if it.Overview == "Found once the internet was back." {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("details never came: %+v", it)
		}
	}
	c.do("GET", "/api/admin/health", nil, &report)
	if report.Server.Internet.Offline {
		t.Fatalf("still offline: %+v", report.Server.Internet)
	}
}

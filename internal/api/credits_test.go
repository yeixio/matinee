package api

import (
	"context"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/analyze"
	"github.com/yeixio/matinee/internal/credits"
	"github.com/yeixio/matinee/internal/store"
)

// After the analysis, the server finds where episodes' end credits start
// from the music they share, and a movie's from its picture going dark.
func TestEndCreditsDetection(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	if code := c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login); code != 200 {
		t.Fatalf("setup: %d", code)
	}
	c.token = login.Token
	c.srv.Analyzer.Credits = &credits.Detector{Store: c.srv.Store, FFmpeg: "ffmpeg", Log: c.srv.Log}

	tune := "aevalsrc='0.3*sin(2*PI*t*220*pow(2,mod(floor(t*3),12)/12))+0.2*sin(2*PI*t*330*pow(2,mod(floor(t*5),7)/12))':s=44100:d=25"
	speech := func(seed string, d int) string {
		return "anoisesrc=color=pink:seed=" + seed + ":d=" + strconv.Itoa(d) + ":a=0.4,volume='0.3+0.7*abs(sin(t*2.3))':eval=frame"
	}
	// Each episode: its own story, then the credits' music to the end.
	episode := func(n string, story int) {
		makeMedia(t, filepath.Join(tmp, "tv", "Tune Show", "Season 1", "Tune Show - S01E0"+n+".mkv"),
			"-f", "lavfi", "-i", "testsrc2=s=64x64:r=5:d="+strconv.Itoa(story+25),
			"-f", "lavfi", "-i", speech("1"+n, story), "-f", "lavfi", "-i", tune,
			"-filter_complex", "[1]aresample=44100[a];[a][2]concat=n=2:v=0:a=1[out]",
			"-map", "0:v", "-map", "[out]", "-shortest", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac")
	}
	episode("1", 35)
	episode("2", 40)
	episode("3", 30)
	// A movie: 50 seconds of picture, then 40 of black.
	makeMedia(t, filepath.Join(tmp, "movies", "Dark End (2020)", "Dark End (2020).mkv"),
		"-f", "lavfi", "-i", "testsrc2=s=64x64:r=5:d=50", "-f", "lavfi", "-i", "color=black:s=64x64:r=5:d=40",
		"-f", "lavfi", "-i", speech("9", 90),
		"-filter_complex", "[0][1]concat=n=2:v=1:a=0[v]", "-map", "[v]", "-map", "2:a",
		"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac")
	for _, lib := range []map[string]any{
		{"name": "TV", "kind": "tv", "paths": []string{filepath.Join(tmp, "tv")}},
		{"name": "Movies", "kind": "movies", "paths": []string{filepath.Join(tmp, "movies")}},
	} {
		if code := c.do("POST", "/api/v1/libraries", lib, nil); code != http.StatusCreated {
			t.Fatalf("create library: %d", code)
		}
		waitForScan(t, c)
	}
	var st analyze.Status
	if code := c.do("PUT", "/api/v1/analysis", map[string]string{"mode": "normal"}, &st); code != 200 || st.Credits == nil || !st.Credits.On || st.Credits.Total != 4 {
		t.Fatalf("status: %d %+v %+v", code, st, st.Credits)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { c.srv.Analyzer.Run(ctx); close(stopped) }()
	t.Cleanup(func() { cancel(); <-stopped })
	deadline := time.Now().Add(90 * time.Second)
	for {
		c.do("GET", "/api/v1/analysis", nil, &st)
		if st.Credits.Done == 4 && st.State == analyze.StateIdle {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("credits not found: %+v %+v", st, st.Credits)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if st.Credits.Found != 4 {
		t.Fatalf("found: %+v", st.Credits)
	}

	var libs []libraryView
	c.do("GET", "/api/v1/libraries", nil, &libs)
	lib := map[string]int64{}
	for _, l := range libs {
		lib[l.Name] = l.ID
	}
	var seasons, eps []store.Item
	c.do("GET", "/api/v1/items/"+itoa(mustShow(t, c, lib["TV"]).ID)+"/children", nil, &seasons)
	c.do("GET", "/api/v1/items/"+itoa(seasons[0].ID)+"/children", nil, &eps)
	for i, want := range []time.Duration{35 * time.Second, 40 * time.Second, 30 * time.Second} {
		var v itemView
		c.do("GET", "/api/v1/items/"+itoa(eps[i].ID), nil, &v)
		if v.EndCredits == nil || v.EndCredits.Source != credits.FromAudio || v.EndCredits.EndMs != 0 {
			t.Fatalf("episode %d: credits %+v", i+1, v.EndCredits)
		}
		if start := time.Duration(v.EndCredits.StartMs) * time.Millisecond; (start - want).Abs() > 1500*time.Millisecond {
			t.Errorf("episode %d: credits at %v, want %v", i+1, start, want)
		}
		// Apps from before endCredits read mediaInfo.creditsMs.
		if v.MediaInfo == nil || v.MediaInfo.CreditsMs != v.EndCredits.StartMs {
			t.Errorf("episode %d: mediaInfo %+v", i+1, v.MediaInfo)
		}
	}

	var movies []store.Item
	c.do("GET", "/api/v1/libraries/"+itoa(lib["Movies"])+"/items", nil, &movies)
	if len(movies) != 1 {
		t.Fatalf("movies: %+v", movies)
	}
	var movie itemView
	c.do("GET", "/api/v1/items/"+itoa(movies[0].ID), nil, &movie)
	if movie.EndCredits == nil || movie.EndCredits.Source != credits.FromVideo || movie.EndCredits.EndMs != 0 ||
		(time.Duration(movie.EndCredits.StartMs)*time.Millisecond-50*time.Second).Abs() > 2*time.Second {
		t.Fatalf("movie: credits %+v", movie.EndCredits)
	}

	// Reaching the credits marks the movie watched.
	if code := c.do("PUT", "/api/v1/items/"+itoa(movies[0].ID)+"/progress", map[string]any{"positionMs": movie.EndCredits.StartMs + 1000}, nil); code != http.StatusNoContent {
		t.Fatalf("progress: %d", code)
	}
	c.do("GET", "/api/v1/items/"+itoa(movies[0].ID), nil, &movie)
	if !movie.Played {
		t.Fatalf("not played after reaching the credits: %+v", movie.Item)
	}

	// Admins can turn it off without touching the analysis speed.
	if code := c.do("PUT", "/api/v1/analysis", map[string]bool{"credits": false}, &st); code != 200 || st.Credits.On || st.Mode != analyze.Normal {
		t.Fatalf("credits off: %d %+v %+v", code, st, st.Credits)
	}
}

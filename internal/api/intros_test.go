package api

import (
	"context"
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/analyze"
	"github.com/yeixio/matinee/internal/intro"
	"github.com/yeixio/matinee/internal/store"
)

// After the analysis, the server finds the intro the season's episodes
// share and items say where it is.
func TestIntroDetection(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	if code := c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login); code != 200 {
		t.Fatalf("setup: %d", code)
	}
	c.token = login.Token
	c.srv.Analyzer.Intros = &intro.Detector{Store: c.srv.Store, FFmpeg: "ffmpeg", Log: c.srv.Log}

	tune := "aevalsrc='0.3*sin(2*PI*t*220*pow(2,mod(floor(t*3),12)/12))+0.2*sin(2*PI*t*330*pow(2,mod(floor(t*5),7)/12))':s=44100:d=25"
	// Each episode: its own cold open, the theme, then its own story.
	episode := func(n, coldOpen string) {
		speech := func(seed, d string) string {
			return "anoisesrc=color=pink:seed=" + seed + ":d=" + d + ":a=0.4,volume='0.3+0.7*abs(sin(t*2.3))':eval=frame"
		}
		makeMedia(t, filepath.Join(tmp, "tv", "Tune Show", "Season 1", "Tune Show - S01E0"+n+".mkv"),
			"-f", "lavfi", "-i", "testsrc2=s=64x64:r=5:d=60",
			"-f", "lavfi", "-i", speech("1"+n, coldOpen), "-f", "lavfi", "-i", tune, "-f", "lavfi", "-i", speech("2"+n, "40"),
			"-filter_complex", "[1]aresample=44100[a];[3]aresample=44100[c];[a][2][c]concat=n=3:v=0:a=1[out]",
			"-map", "0:v", "-map", "[out]", "-shortest", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac")
	}
	episode("1", "5")
	episode("2", "20")
	episode("3", "12")
	if code := c.do("POST", "/api/v1/libraries", map[string]any{"name": "TV", "kind": "tv", "paths": []string{filepath.Join(tmp, "tv")}}, nil); code != http.StatusCreated {
		t.Fatalf("create library: %d", code)
	}
	waitForScan(t, c)
	var st analyze.Status
	if code := c.do("PUT", "/api/v1/analysis", map[string]string{"mode": "normal"}, &st); code != 200 || st.Intros == nil || !st.Intros.On || st.Intros.Total != 3 {
		t.Fatalf("status: %d %+v %+v", code, st, st.Intros)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { c.srv.Analyzer.Run(ctx); close(stopped) }()
	t.Cleanup(func() { cancel(); <-stopped })
	deadline := time.Now().Add(60 * time.Second)
	for {
		c.do("GET", "/api/v1/analysis", nil, &st)
		if st.Intros.Done == 3 && st.State == analyze.StateIdle {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("intros not found: %+v %+v", st, st.Intros)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if st.Intros.Found != 3 {
		t.Fatalf("found: %+v", st.Intros)
	}

	var libs []libraryView
	c.do("GET", "/api/v1/libraries", nil, &libs)
	var seasons, eps []store.Item
	c.do("GET", "/api/v1/items/"+itoa(mustShow(t, c, libs[0].ID).ID)+"/children", nil, &seasons)
	c.do("GET", "/api/v1/items/"+itoa(seasons[0].ID)+"/children", nil, &eps)
	for i, want := range []time.Duration{5 * time.Second, 20 * time.Second, 12 * time.Second} {
		var v itemView
		c.do("GET", "/api/v1/items/"+itoa(eps[i].ID), nil, &v)
		if v.Intro == nil || v.Intro.Source != "audio" {
			t.Fatalf("episode %d: intro %+v", i+1, v.Intro)
		}
		start, end := time.Duration(v.Intro.StartMs)*time.Millisecond, time.Duration(v.Intro.EndMs)*time.Millisecond
		if (start-want).Abs() > time.Second || (end-want-25*time.Second).Abs() > 1500*time.Millisecond {
			t.Errorf("episode %d: intro %v–%v, want %v–%v", i+1, start, end, want, want+25*time.Second)
		}
	}
	// Shows, seasons and movies have none.
	var show itemView
	c.do("GET", "/api/v1/items/"+itoa(seasons[0].ID), nil, &show)
	if show.Intro != nil {
		t.Fatalf("season intro: %+v", show.Intro)
	}

	// Admins can turn it off without touching the analysis speed.
	if code := c.do("PUT", "/api/v1/analysis", map[string]bool{"intros": false}, &st); code != 200 || st.Intros.On || st.Mode != analyze.Normal {
		t.Fatalf("intros off: %d %+v %+v", code, st, st.Intros)
	}
}

// Skipping intros by itself is each person's choice, kept when an app that
// doesn't know it changes the others.
func TestSkipIntrosPreference(t *testing.T) {
	c, _ := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	var p playbackPrefs
	if c.do("GET", "/api/v1/me/playback", nil, &p); p.SkipIntros || !p.AutoPlay {
		t.Fatalf("defaults: %+v", p)
	}
	c.do("PUT", "/api/v1/me/playback", map[string]bool{"skipIntros": true}, &p)
	if !p.SkipIntros || !p.AutoPlay {
		t.Fatalf("after skipIntros: %+v", p)
	}
	c.do("PUT", "/api/v1/me/playback", map[string]bool{"autoPlay": true, "binge": true}, &p)
	p = playbackPrefs{}
	if c.do("GET", "/api/v1/me/playback", nil, &p); !p.SkipIntros || !p.Binge {
		t.Fatalf("after an older app: %+v", p)
	}
}

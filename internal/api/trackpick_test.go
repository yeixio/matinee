package api

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/analyze"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/subtitle"
)

// A film whose first, default track is a dub starts in the person's
// language, in the best of its tracks in that language.
func TestPickAudio(t *testing.T) {
	dune := []probe.AudioStream{
		{Index: 0, Codec: "ac3", Channels: 6, Language: "hin", Default: true},
		{Index: 1, Codec: "aac", Channels: 2, Language: "eng"},
		{Index: 2, Codec: "eac3", Channels: 6, Language: "eng", Bitrate: 640000},
		{Index: 3, Codec: "truehd", Channels: 8, Language: "eng", Atmos: true, Commentary: true},
		{Index: 4, Codec: "ac3", Channels: 6, Language: "eng", Bitrate: 448000},
		{Index: 5, Codec: "aac", Channels: 2, Language: "fre"},
	}
	for _, c := range []struct {
		langs []string
		want  int
	}{
		{[]string{"en", "English"}, 2},  // best English mix, not the commentary
		{[]string{"en-GB"}, 2},          // regions don't matter
		{[]string{"fr", "English"}, 5},  // the person's language first
		{[]string{"de", "English"}, 2},  // else the film's own
		{[]string{"de", "Japanese"}, 0}, // else the file's default
		{[]string{"", ""}, 0},           // nothing known: the file's default
		{[]string{"hi", "English"}, 0},  // the dub when that's the person's language
	} {
		if a, ok := pickAudio(dune, c.langs...); !ok || a.Index != c.want {
			t.Errorf("pickAudio(%v) = %d, want %d", c.langs, a.Index, c.want)
		}
	}
	atmos := []probe.AudioStream{
		{Index: 0, Codec: "dts", Profile: "DTS-HD MA", Channels: 8, Language: "eng", Default: true},
		{Index: 1, Codec: "truehd", Channels: 8, Language: "eng", Atmos: true},
		{Index: 2, Codec: "flac", Channels: 2, Language: "eng"},
	}
	if a, _ := pickAudio(atmos, "en"); a.Index != 1 {
		t.Errorf("Atmos should win: %d", a.Index)
	}
	if _, ok := pickAudio(nil, "en"); ok {
		t.Error("no tracks, no pick")
	}
}

// Subtitles start as the person's subtitle choices say, against the sound
// that plays.
func TestPickSubtitle(t *testing.T) {
	tracks := []subtitle.Track{
		{ID: "e0", Language: "en", Forced: true},
		{ID: "e1", Language: "en", SDH: true},
		{ID: "e2", Language: "en"},
		{ID: "e3", Language: "es", BurnIn: true},
		{ID: "e4", Language: "es"},
		{ID: "e5", Language: "fr", Forced: true, Default: true},
	}
	for _, c := range []struct {
		prefs      *subtitlePrefs
		app, audio string
		want       string
	}{
		{nil, "en", "eng", "e0"},                            // forced lines in the sound's language
		{nil, "en", "jpn", "e5"},                            // else the default forced ones
		{&subtitlePrefs{Mode: "off"}, "en", "jpn", ""},      // off is off
		{&subtitlePrefs{Mode: "always"}, "en", "eng", "e2"}, // plain before SDH
		{&subtitlePrefs{Mode: "always"}, "es", "eng", "e4"}, // text before pictures
		{&subtitlePrefs{Mode: "always", Languages: []string{"de", "es"}}, "en", "eng", "e4"},
		{&subtitlePrefs{Mode: "foreign"}, "en", "eng", "e0"}, // the sound is the person's
		{&subtitlePrefs{Mode: "foreign"}, "en", "hin", "e2"}, // a dub they don't speak
	} {
		if got := pickSubtitle(tracks, c.prefs, c.app, c.audio); got != c.want {
			t.Errorf("pickSubtitle(%+v, %s, %s) = %q, want %q", c.prefs, c.app, c.audio, got, c.want)
		}
	}
}

// Tracks picked while watching are played again next time, and carry over
// to the rest of a show by language.
func TestTrackChoices(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	c.srv.Scanner.AfterScan = func(context.Context, store.Library) { c.srv.Analyzer.Wake() }

	// A dub first and marked default, then English in stereo and in 5.1.
	tracks := []string{"-f", "lavfi", "-i", "testsrc2=s=320x240:d=0.3", "-f", "lavfi", "-i", "sine=d=0.3",
		"-map", "0:v", "-map", "1:a", "-map", "1:a", "-map", "1:a", "-c:v", "libx264", "-preset", "ultrafast",
		"-c:a", "aac", "-ac:a:0", "2", "-ac:a:1", "2", "-ac:a:2", "6",
		"-metadata:s:a:0", "language=hin", "-metadata:s:a:1", "language=eng", "-metadata:s:a:2", "language=eng",
		"-disposition:a:0", "default", "-disposition:a:1", "0", "-disposition:a:2", "0"}
	srt := "1\n00:00:00,000 --> 00:00:00,200\nHi\n"
	for _, n := range []string{"1", "2"} {
		file := filepath.Join(tmp, "tv", "Dunes", "Season 1", "Dunes - S01E0"+n+".mkv")
		makeMedia(t, file, tracks...)
		for _, l := range []string{"en", "es"} {
			os.WriteFile(file[:len(file)-4]+"."+l+".srt", []byte(srt), 0o644)
		}
	}
	if code := c.do("POST", "/api/v1/libraries", map[string]any{"name": "TV", "kind": "tv", "paths": []string{filepath.Join(tmp, "tv")}}, nil); code != http.StatusCreated {
		t.Fatalf("create library: %d", code)
	}
	waitForScan(t, c)
	c.do("PUT", "/api/v1/analysis", map[string]string{"mode": "normal"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { c.srv.Analyzer.Run(ctx); close(stopped) }()
	t.Cleanup(func() { cancel(); <-stopped })
	deadline := time.Now().Add(30 * time.Second)
	for {
		var st analyze.Status
		c.do("GET", "/api/v1/analysis", nil, &st)
		if st.Done == 2 && st.State == analyze.StateIdle {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("analysis didn't finish: %+v", st)
		}
		time.Sleep(50 * time.Millisecond)
	}

	var libs []libraryView
	c.do("GET", "/api/v1/libraries", nil, &libs)
	var shows, seasons, eps []store.Item
	c.do("GET", "/api/v1/libraries/"+itoa(libs[0].ID)+"/items", nil, &shows)
	c.do("GET", "/api/v1/items/"+itoa(shows[0].ID)+"/children", nil, &seasons)
	c.do("GET", "/api/v1/items/"+itoa(seasons[0].ID)+"/children", nil, &eps)
	if len(eps) != 2 {
		t.Fatalf("episodes: %+v", eps)
	}
	type view struct {
		MediaInfo *probe.MediaInfo
		Subtitles []subtitle.Track
		TrackPick *trackPickView
	}
	get := func(id int64) view {
		t.Helper()
		var v view
		if code := c.do("GET", "/api/v1/items/"+itoa(id), nil, &v); code != 200 || v.TrackPick == nil || v.TrackPick.Audio == nil {
			t.Fatalf("item %d: %d %+v", id, code, v.TrackPick)
		}
		return v
	}
	subIn := func(v view, lang string) string {
		for _, s := range v.Subtitles {
			if s.Language == lang {
				return s.ID
			}
		}
		t.Fatalf("no %s subtitles in %+v", lang, v.Subtitles)
		return ""
	}

	// Nothing known about the person's language: the file's own default.
	if v := get(eps[0].ID); *v.TrackPick.Audio != 0 {
		t.Fatalf("no language: %+v", v.TrackPick)
	}
	// English, in 5.1, over the Hindi default; no subtitles for English sound.
	if code := c.do("PUT", "/api/v1/me/language", map[string]string{"language": "en"}, nil); code != 200 {
		t.Fatalf("set language: %d", code)
	}
	if v := get(eps[0].ID); *v.TrackPick.Audio != 2 || v.TrackPick.Subtitle != "" || v.TrackPick.AudioRemembered {
		t.Fatalf("first pick: %+v", v.TrackPick)
	}
	// Someone who reads Spanish and always wants subtitles.
	c.do("PUT", "/api/v1/me/playback", map[string]any{"subtitles": map[string]any{"mode": "always", "languages": []string{"es"}}}, nil)
	v := get(eps[0].ID)
	if v.TrackPick.Subtitle != subIn(v, "es") {
		t.Fatalf("Spanish subtitles: %+v", v.TrackPick)
	}

	// They switch to the stereo track and English subtitles.
	en := subIn(v, "en")
	if code := c.do("PUT", "/api/v1/items/"+itoa(eps[0].ID)+"/tracks", map[string]any{"audio": 1}, nil); code != http.StatusNoContent {
		t.Fatalf("pick audio: %d", code)
	}
	if code := c.do("PUT", "/api/v1/items/"+itoa(eps[0].ID)+"/tracks", map[string]any{"subtitle": en}, nil); code != http.StatusNoContent {
		t.Fatalf("pick subtitle: %d", code)
	}
	if v := get(eps[0].ID); *v.TrackPick.Audio != 1 || !v.TrackPick.AudioRemembered || v.TrackPick.Subtitle != en || !v.TrackPick.SubtitleRemembered {
		t.Fatalf("remembered: %+v", v.TrackPick)
	}
	// The next episode starts in the same language (its best track) with
	// English subtitles too.
	if v := get(eps[1].ID); *v.TrackPick.Audio != 2 || v.TrackPick.Subtitle != subIn(v, "en") {
		t.Fatalf("next episode: %+v", v.TrackPick)
	}
	// Hindi on the second, with subtitles off.
	c.do("PUT", "/api/v1/items/"+itoa(eps[1].ID)+"/tracks", map[string]any{"audio": 0, "subtitle": "off"}, nil)
	if v := get(eps[1].ID); *v.TrackPick.Audio != 0 || v.TrackPick.Subtitle != "" || !v.TrackPick.SubtitleRemembered {
		t.Fatalf("second episode: %+v", v.TrackPick)
	}
	// The first keeps its own picks.
	if v := get(eps[0].ID); *v.TrackPick.Audio != 1 || v.TrackPick.Subtitle != en {
		t.Fatalf("first episode after: %+v", v.TrackPick)
	}

	for _, bad := range []map[string]any{{"audio": 9}, {"subtitle": "x123"}, {}} {
		if code := c.do("PUT", "/api/v1/items/"+itoa(eps[0].ID)+"/tracks", bad, nil); code != http.StatusBadRequest {
			t.Fatalf("bad pick %v: %d", bad, code)
		}
	}
	if code := c.do("PUT", "/api/v1/items/"+itoa(shows[0].ID)+"/tracks", map[string]any{"audio": 0}, nil); code != http.StatusBadRequest {
		t.Fatalf("pick on a show: %d", code)
	}
}

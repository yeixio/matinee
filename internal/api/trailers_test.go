package api

import (
	"context"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/metadata"
	"github.com/yeixio/matinee/internal/store"
)

type fakeTrailers struct{ calls int }

func (f *fakeTrailers) Trailers(ctx context.Context, kind string, id int64, season int) ([]metadata.Video, error) {
	f.calls++
	if kind == "show" && season == 1 {
		return nil, nil // no season trailers, so the show's are used
	}
	return []metadata.Video{{Name: kind + " trailer", Site: "YouTube", Key: kind + "-key", Type: "Trailer"}}, nil
}

func TestTrailers(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	remote := &fakeTrailers{}
	var srv *Server
	c, tmp := newTestServer(t, func(s *Server) { s.TrailerSource, srv = remote, s })
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	h264 := []string{"-f", "lavfi", "-i", "testsrc=duration=2:size=160x120:rate=24", "-c:v", "libx264", "-preset", "ultrafast"}
	makeMedia(t, filepath.Join(tmp, "movies", "Heat (1995)", "Heat (1995).mp4"), h264...)
	makeMedia(t, filepath.Join(tmp, "movies", "Heat (1995)", "Heat (1995)-trailer.mp4"), h264...)
	makeMedia(t, filepath.Join(tmp, "movies", "Heat (1995)", "Trailers", "Teaser.mkv"),
		"-f", "lavfi", "-i", "testsrc=duration=2:size=160x120:rate=24", "-c:v", "mpeg4")
	makeMedia(t, filepath.Join(tmp, "tv", "Severance", "Season 1", "Severance S01E01.mkv"), h264...)
	makeMedia(t, filepath.Join(tmp, "tv", "Severance", "trailer.mp4"), h264...)

	var movies, shows store.Library
	c.do("POST", "/api/libraries", map[string]any{"name": "Movies", "kind": "movies", "paths": []string{filepath.Join(tmp, "movies")}}, &movies)
	c.do("POST", "/api/libraries", map[string]any{"name": "TV", "kind": "tv", "paths": []string{filepath.Join(tmp, "tv")}}, &shows)
	waitForScan(t, c)

	var items []store.Item
	c.do("GET", "/api/libraries/"+itoa(movies.ID)+"/items", nil, &items)
	if len(items) != 1 {
		t.Fatalf("trailers were listed as movies: %+v", items)
	}
	movie := items[0]
	ctx := context.Background()
	srv.Store.ApplyMetadata(ctx, movie.ID, &store.Metadata{TMDBID: 949})

	var trailers []trailerView
	c.do("GET", "/api/items/"+itoa(movie.ID)+"/trailers", nil, &trailers)
	if len(trailers) != 3 {
		t.Fatalf("trailers: %+v", trailers)
	}
	local, teaser, online := trailers[0], trailers[1], trailers[2]
	if local.Name != "Trailer" || local.Source != "local" || local.VideoCodec != "h264" || local.MimeType != "video/mp4" || !strings.Contains(local.Container, "mp4") {
		t.Errorf("local trailer: %+v", local)
	}
	if teaser.Name != "Teaser" || teaser.VideoCodec != "mpeg4" {
		t.Errorf("trailers folder: %+v", teaser)
	}
	if online.Source != "youtube" || online.Key != "movie-key" || online.URL != "https://www.youtube.com/watch?v=movie-key" {
		t.Errorf("online trailer: %+v", online)
	}

	// Local trailers play like the movie: straight from the file, or converted.
	req, _ := http.NewRequest("GET", c.base+"/api/items/"+itoa(movie.ID)+"/trailers/"+local.ID+"/file", nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Range", "bytes=0-3")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusPartialContent || len(b) != 4 {
		t.Errorf("trailer file: %d, %d bytes", res.StatusCode, len(b))
	}
	var hls struct{ URL string }
	if code := c.do("POST", "/api/items/"+itoa(movie.ID)+"/trailers/"+teaser.ID+"/hls", nil, &hls); code != 200 || hls.URL == "" {
		t.Errorf("trailer transcode: %d %+v", code, hls)
	}
	if code := c.do("GET", "/api/items/"+itoa(movie.ID)+"/trailers/local-9/file", nil, nil); code != http.StatusNotFound {
		t.Errorf("missing trailer: %d", code)
	}

	// Online trailers are asked for once, then remembered.
	c.do("GET", "/api/items/"+itoa(movie.ID)+"/trailers", nil, &trailers)
	if remote.calls != 1 {
		t.Errorf("TMDB asked %d times", remote.calls)
	}

	// A season without trailers of its own shows its show's.
	c.do("GET", "/api/libraries/"+itoa(shows.ID)+"/items", nil, &items)
	show := items[0]
	srv.Store.ApplyMetadata(ctx, show.ID, &store.Metadata{TMDBID: 95396})
	var seasons []store.Item
	c.do("GET", "/api/items/"+itoa(show.ID)+"/children", nil, &seasons)
	c.do("GET", "/api/items/"+itoa(seasons[0].ID)+"/trailers", nil, &trailers)
	if len(trailers) != 2 || trailers[0].Source != "local" || trailers[1].Key != "show-key" {
		t.Errorf("season trailers: %+v", trailers)
	}

	// An admin can turn online trailers off.
	var set serverSettings
	c.do("PUT", "/api/settings", map[string]any{"remoteTrailers": false}, &set)
	if set.RemoteTrailers {
		t.Fatal("setting didn't stick")
	}
	c.do("GET", "/api/items/"+itoa(movie.ID)+"/trailers", nil, &trailers)
	if len(trailers) != 2 {
		t.Errorf("with online trailers off: %+v", trailers)
	}
}

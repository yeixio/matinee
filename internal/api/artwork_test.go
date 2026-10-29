package api

import (
	"context"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// A cover embedded in an audiobook is served as the book's, and finding
// artwork looks up online covers again, but not covers from the files.
func TestFindArtwork(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp, db := newTestServerStore(t)
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	makeMedia(t, filepath.Join(tmp, "books", "Jane Doe", "Covered", "Covered.m4b"),
		"-f", "lavfi", "-i", "sine=duration=2", "-f", "lavfi", "-i", "color=c=red:s=64x64:d=1",
		"-map", "0:a", "-map", "1:v", "-frames:v", "1", "-c:a", "aac", "-c:v", "mjpeg", "-disposition:v:0", "attached_pic", "-f", "mp4")
	makeMedia(t, filepath.Join(tmp, "books", "Jane Doe", "Bare", "01.mp3"), "-f", "lavfi", "-i", "sine=duration=2")
	body := map[string]any{"name": "Audiobooks", "kind": "audiobooks", "paths": []string{filepath.Join(tmp, "books")}}
	if code := c.do("POST", "/api/libraries", body, nil); code != http.StatusCreated {
		t.Fatalf("create library: %d", code)
	}
	var libs []libraryView
	c.do("GET", "/api/libraries", nil, &libs)
	var books []store.Item
	// The scan starts in the background; wait for both books.
	for deadline := time.Now().Add(30 * time.Second); len(books) < 2 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		waitForScan(t, c)
		c.do("GET", "/api/libraries/"+itoa(libs[0].ID)+"/items?kind=book", nil, &books)
	}
	byTitle := map[string]store.Item{}
	for _, b := range books {
		byTitle[b.Title] = b
	}
	covered, bare := byTitle["Covered"], byTitle["Bare"]
	if !covered.HasPoster || bare.HasPoster {
		t.Fatalf("books: %+v", books)
	}
	req, _ := http.NewRequest("GET", c.base+"/api/v1/items/"+itoa(covered.ID)+"/image/poster", nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	img, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || http.DetectContentType(img) != "image/jpeg" {
		t.Fatalf("embedded cover: %d %s", res.StatusCode, http.DetectContentType(img))
	}

	ctx := context.Background()
	db.ApplyMetadata(ctx, bare.ID, &store.Metadata{Poster: "https://covers.example/bare.jpg"})
	var found struct{ LookingUp int64 }
	if code := c.do("POST", "/api/v1/libraries/"+itoa(libs[0].ID)+"/artwork", nil, &found); code != http.StatusAccepted {
		t.Fatalf("find artwork: %d", code)
	}
	// The author and the bare book; the covered book keeps its own cover.
	if found.LookingUp != 2 {
		t.Fatalf("looking up %d items", found.LookingUp)
	}
	waitForScan(t, c)
	if b, _ := db.Item(ctx, 0, bare.ID); b.Poster != "https://covers.example/bare.jpg" || b.MetaAt != 0 {
		t.Fatalf("an online cover should be looked up again, and kept until then: %+v", b)
	}
	if b, _ := db.Item(ctx, 0, covered.ID); !strings.HasPrefix(b.Poster, "embedded:") {
		t.Fatalf("own cover changed: %q", b.Poster)
	}
}

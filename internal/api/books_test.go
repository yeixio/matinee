package api

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

// An audiobook plays as one listen across its files: the book lists its
// parts and chapters on one clock, keeps each person's place, shows up in
// Continue listening, and finishing it plays its parts too.
func TestAudiobooks(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	dir := filepath.Join(tmp, "books", "Jane Doe", "Night Watch", "Book 2 - The Tower")
	for _, f := range []string{"01.mp3", "02.mp3", "03.mp3"} {
		makeMedia(t, filepath.Join(dir, f), "-f", "lavfi", "-i", "sine=duration=40", "-metadata", "composer=Sam Poe")
	}
	body := map[string]any{"name": "Audiobooks", "kind": "audiobooks", "paths": []string{filepath.Join(tmp, "books")}}
	if code := c.do("POST", "/api/libraries", body, nil); code != http.StatusCreated {
		t.Fatalf("create library: %d", code)
	}
	waitForScan(t, c)
	var libs []libraryView
	c.do("GET", "/api/libraries", nil, &libs)

	var books []store.Item
	c.do("GET", "/api/libraries/"+itoa(libs[0].ID)+"/items?kind=book&series=Night+Watch&sort=series", nil, &books)
	if len(books) != 1 || books[0].Title != "The Tower" || books[0].Author != "Jane Doe" || books[0].DurationMs == nil || *books[0].DurationMs < 110_000 {
		t.Fatalf("books: %+v", books)
	}
	id := itoa(books[0].ID)

	var b bookView
	if code := c.do("GET", "/api/v1/items/"+id+"/book", nil, &b); code != 200 {
		t.Fatalf("book: %d", code)
	}
	if len(b.Parts) != 3 || len(b.Chapters) != 3 || b.Parts[2].StartMs < 70_000 || b.Parts[0].MimeType != "audio/mpeg" || b.Chapters[1].PartID != b.Parts[1].ID {
		t.Fatalf("book: %+v", b)
	}
	// A part's id finds its book.
	var viaPart bookView
	c.do("GET", "/api/v1/items/"+itoa(b.Parts[1].ID)+"/book", nil, &viaPart)
	if viaPart.ID != b.ID {
		t.Fatalf("part's book: %d", viaPart.ID)
	}

	var groups []store.BookGroup
	c.do("GET", "/api/v1/libraries/"+itoa(libs[0].ID)+"/series", nil, &groups)
	if len(groups) != 1 || groups[0].Name != "Night Watch" || groups[0].Books != 1 {
		t.Fatalf("series: %+v", groups)
	}
	c.do("GET", "/api/v1/libraries/"+itoa(libs[0].ID)+"/narrators", nil, &groups)
	if len(groups) != 1 || groups[0].Name != "Sam Poe" {
		t.Fatalf("narrators: %+v", groups)
	}

	// Halfway through is a place to come back to, on Home.
	c.do("PUT", "/api/v1/items/"+id+"/progress", map[string]any{"positionMs": 60_000}, nil)
	var home struct{ Rows []HomeRow }
	c.do("GET", "/api/v1/home", nil, &home)
	if len(home.Rows) == 0 || home.Rows[0].ID != "continueListening" || home.Rows[0].Items[0].PositionMs != 60_000 {
		t.Fatalf("home rows: %+v", home.Rows)
	}
	var started []store.Item
	c.do("GET", "/api/v1/items?kind=book&watched=inProgress", nil, &started)
	if len(started) != 1 {
		t.Fatalf("in progress: %+v", started)
	}

	// The last minute finishes it.
	c.do("PUT", "/api/v1/items/"+id+"/progress", map[string]any{"positionMs": *books[0].DurationMs - 30_000}, nil)
	c.do("GET", "/api/v1/items/"+id+"/book", nil, &b)
	if !b.Played || b.PositionMs != 0 {
		t.Fatalf("finished: %+v", b.Item)
	}
	var finished []store.Item
	c.do("GET", "/api/v1/items?kind=book&watched=watched", nil, &finished)
	if len(finished) != 1 {
		t.Fatalf("finished books: %+v", finished)
	}
	c.do("GET", "/api/v1/home", nil, &home)
	if len(home.Rows) > 0 && home.Rows[0].ID == "continueListening" {
		t.Fatal("a finished book left on Continue listening")
	}
	// Listening again starts it over.
	c.do("PUT", "/api/v1/items/"+id+"/progress", map[string]any{"positionMs": 5_000}, nil)
	c.do("GET", "/api/v1/items?kind=book&watched=inProgress", nil, &started)
	if len(started) != 1 {
		t.Fatalf("started over: %+v", started)
	}
}

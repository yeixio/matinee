package scan

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store/storetest"
)

func TestParseBookPath(t *testing.T) {
	idx := func(f *float64) float64 {
		if f == nil {
			return -1
		}
		return *f
	}
	for _, c := range []struct {
		path                  string
		author, series, book  string
		index                 float64
		part                  string
		partIndex, disc, year int
	}{
		{"/books/Jane Doe/The Long Road (2019)/01 - Departure.mp3", "Jane Doe", "", "The Long Road", -1, "Departure", 1, 0, 2019},
		{"/books/Jane Doe/Night Watch/Book 2 - The Tower/Part 03.mp3", "Jane Doe", "Night Watch", "The Tower", 2, "Part 03", 3, 0, 0},
		{"/books/Jane Doe/Night Watch/The Gate (Book 1.5).m4b", "Jane Doe", "Night Watch", "The Gate", 1.5, "The Gate (Book 1.5)", 0, 0, 0},
		{"/books/Jane Doe/Quiet Hours/Quiet Hours.m4b", "Jane Doe", "", "Quiet Hours", -1, "Quiet Hours", 0, 0, 0},
		{"/books/Jane Doe/Quiet Hours.m4b", "Jane Doe", "", "Quiet Hours", -1, "Quiet Hours", 0, 0, 0},
		{"/books/Big Book/CD 2/Track 04.mp3", "", "", "Big Book", -1, "Track 04", 4, 2, 0},
		{"/books/Loose.m4b", "", "", "Loose", -1, "Loose", 0, 0, 0},
	} {
		b := ParseBookPath("/books", c.path)
		if b.Author != c.author || b.Series != c.series || b.Book != c.book || idx(b.SeriesIndex) != c.index ||
			b.Part != c.part || b.PartIndex != c.partIndex || b.Disc != c.disc || b.Year != c.year {
			t.Errorf("%s: %+v (index %v)", c.path, b, idx(b.SeriesIndex))
		}
	}
}

// Tags win over folders; narrators are split into people.
func TestBookFromTags(t *testing.T) {
	b := bookFromTags(map[string]string{
		"artist": "Jane Doe", "album": "Night Watch 3: The Keep", "title": "Chapter 7",
		"composer": "Sam Poe & Ann Lee", "series": "Night Watch", "series-part": "3", "track": "7/20", "date": "2021",
	}, ParseBookPath("/books", "/books/x/y.mp3"))
	if b.Author != "Jane Doe" || b.Book != "Night Watch 3: The Keep" || b.Part != "Chapter 7" || b.PartIndex != 7 ||
		len(b.Narrators) != 2 || b.Narrators[1] != "Ann Lee" || b.Series != "Night Watch" || *b.SeriesIndex != 3 || b.Year != 2021 {
		t.Fatalf("%+v", b)
	}
	whole := bookFromTags(map[string]string{"title": "The Real Title"}, ParseBookPath("/books", "/books/A/rip.m4b"))
	if whole.Book != "The Real Title" || whole.Author != "A" {
		t.Fatalf("whole book: %+v", whole)
	}
}

// A book in one file keeps its chapters; a book in a folder of files has a
// chapter per file. Both add up to the whole book's length.
func TestScanAudiobooks(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	root := t.TempDir()
	mk := func(rel string) string {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		return p
	}
	meta := filepath.Join(t.TempDir(), "meta.txt")
	os.WriteFile(meta, []byte(";FFMETADATA1\ntitle=One File\nartist=Jane Doe\ncomposer=Sam Poe\n"+
		"[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=1000\ntitle=Opening\n"+
		"[CHAPTER]\nTIMEBASE=1/1000\nSTART=1000\nEND=2000\ntitle=Ending\n"), 0o644)
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "sine=d=2", "-i", meta,
		"-map_metadata", "1", "-map_chapters", "1", "-c:a", "aac", "-f", "mp4", mk("Jane Doe/One File.m4b")).CombinedOutput(); err != nil {
		t.Fatalf("make m4b: %v\n%s", err, out)
	}
	for _, rel := range []string{"Jane Doe/Night Watch/Book 1 - The Gate/02.mp3", "Jane Doe/Night Watch/Book 1 - The Gate/01.mp3"} {
		if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "sine=d=1", mk(rel)).CombinedOutput(); err != nil {
			t.Fatalf("make mp3: %v\n%s", err, out)
		}
	}
	os.WriteFile(mk("Jane Doe/Night Watch/Book 1 - The Gate/reader.txt"), []byte("Ann Lee\n"), 0o644)

	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Books", "audiobooks", []string{root})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: ffprobe}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if stats, err := s.Scan(ctx, lib); err != nil || stats.Added != 3 {
		t.Fatalf("scan: %+v %v", stats, err)
	}
	authors, _ := st.TopLevel(ctx, 0, lib.ID)
	if len(authors) != 1 || authors[0].Title != "Jane Doe" || authors[0].Kind != "author" {
		t.Fatalf("authors: %+v", authors)
	}
	books, _ := st.Children(ctx, 0, authors[0].ID)
	if len(books) != 2 {
		t.Fatalf("books: %+v", books)
	}
	// Series books come after the rest, in series order.
	one, gate := books[0], books[1]
	if one.Title != "One File" || len(one.Narrators) != 1 || one.Narrators[0] != "Sam Poe" || one.Author != "Jane Doe" {
		t.Fatalf("one file: %+v", one)
	}
	if gate.Title != "The Gate" || gate.Series != "Night Watch" || gate.SeriesIndex == nil || *gate.SeriesIndex != 1 || len(gate.Narrators) != 1 {
		t.Fatalf("the gate: %+v", gate)
	}
	b, err := st.Book(ctx, 0, one.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Parts) != 1 || len(b.Chapters) != 2 || b.Chapters[1].Title != "Ending" || b.Chapters[1].StartMs != 1000 || b.DurationMs == nil || *b.DurationMs < 1900 {
		t.Fatalf("one file's book: %+v", b)
	}
	b, _ = st.Book(ctx, 0, gate.ID)
	if len(b.Parts) != 2 || len(b.Chapters) != 2 || b.Parts[1].StartMs < 900 || b.Chapters[1].StartMs != b.Parts[1].StartMs || b.Parts[0].Title != "01" {
		t.Fatalf("the gate's book: %+v", b)
	}
}

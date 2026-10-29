package scan

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
)

// ---- audiobooks ----

// An audiobook library holds authors, their books, and each book's parts:
// one file with chapters marked inside (usually .m4b), or a folder of
// files, one per chapter or disc. Folders can be laid out as
// Author/Book/…, Author/Series/Book/… or Book/…, with CD 1, CD 2 folders
// under a book; tags in the files win over folder names.
type audiobooks struct{}

func (audiobooks) Name() string                  { return "audiobooks" }
func (audiobooks) Wants(path string) bool        { return isAudio(path) || isWholeBook(path) }
func (audiobooks) Playable(pr probe.Result) bool { return pr.AudioCodec != "" }

func (audiobooks) Index(ctx context.Context, st *store.Store, f File) (store.Item, error) {
	b := bookFromTags(f.Probe.Tags, ParseBookPath(f.Root, f.Path))
	if b.BookDir != "" {
		if b.Overview == "" {
			b.Overview = readNote(b.BookDir, "desc.txt", "description.txt")
		}
		if len(b.Narrators) == 0 {
			b.Narrators = splitNames(readNote(b.BookDir, "reader.txt", "narrator.txt"))
		}
	}
	authorID, err := st.EnsureContainer(ctx, f.Library.ID, nil, "author", b.Author, nil, nil)
	if err != nil {
		return store.Item{}, err
	}
	if b.AuthorDir != "" {
		if photo := findNamed(b.AuthorDir, "author", "folder", "photo"); photo != "" {
			if err := st.SetArt(ctx, authorID, photo, ""); err != nil {
				return store.Item{}, err
			}
		}
	}
	bookID, err := st.EnsureContainer(ctx, f.Library.ID, &authorID, "book", b.Book, nil, nonZero(b.Year))
	if err != nil {
		return store.Item{}, err
	}
	cover := bookCover(b, f.Path)
	if cover == "" && f.Probe.HasCover {
		cover = "embedded:" + f.Path
	}
	if cover != "" {
		if err := st.SetArt(ctx, bookID, cover, ""); err != nil {
			return store.Item{}, err
		}
	}
	err = st.SetBookInfo(ctx, bookID, store.BookInfo{Narrators: b.Narrators, Series: b.Series, SeriesIndex: b.SeriesIndex, Overview: b.Overview})
	if err != nil {
		return store.Item{}, err
	}
	return store.Item{
		Kind: "part", ParentID: &bookID, Title: b.Part,
		IndexNumber: nonZero(b.PartIndex), DiscNumber: nonZero(b.Disc), Chapters: f.Probe.Chapters,
	}, nil
}

// FinishScan adds up each book's length once its parts are in, and picks
// up covers added to book folders since their files were scanned.
func (audiobooks) FinishScan(ctx context.Context, st *store.Store, lib store.Library) error {
	if err := st.SumBookLengths(ctx, lib.ID); err != nil {
		return err
	}
	return fillFolderArt(ctx, st, lib, "book", func(root, path string) string {
		return bookCover(ParseBookPath(root, path), path)
	})
}

// bookCover finds a book's cover: an image named after the file (as when
// several books share a folder), or the usual cover images in the book's
// own folder or the CD folder the file is in.
func bookCover(b BookFile, path string) string {
	if c := findNamed(filepath.Dir(path), stem(path)); c != "" {
		return c
	}
	if b.BookDir == "" {
		return "" // the folder's other images belong to other books or the author
	}
	if c := findCover(b.BookDir, b.Book); c != "" {
		return c
	}
	if dir := filepath.Dir(path); dir != b.BookDir {
		return findCover(dir)
	}
	return ""
}

// BookFile is what an audiobook file's place on disk and its tags say.
type BookFile struct {
	Author      string
	Book        string
	Series      string
	SeriesIndex *float64
	Year        int
	Narrators   []string
	Overview    string
	Part        string // the file's own title
	PartIndex   int
	Disc        int
	Whole       bool // the file is the whole book, chapters and all
	// The book's and author's own folders, for artwork and notes; empty
	// when the book or author has no folder of its own.
	BookDir, AuthorDir string
}

var (
	// "CD 1", "Disc 02", "Part 3" folders under a book.
	discDirRe = regexp.MustCompile(`(?i)^(?:cd|dis[ck]|part)[\s._-]*(\d{1,3})$`)
	// "Book 2 - Title", "02 - Title", "#2. Title", "Vol. 2: Title".
	seriesLeadRe = regexp.MustCompile(`(?i)^(?:book|vol(?:ume)?\.?|#)?\s*(\d{1,3}(?:\.\d{1,2})?)\s*[-.:]\s+(.+)$`)
	// "Title (Book 2)", "Title, Book 2", "Title - Vol 2".
	seriesTailRe = regexp.MustCompile(`(?i)^(.+?)[\s,(\[-]+(?:book|vol(?:ume)?\.?|#)\s*(\d{1,3}(?:\.\d{1,2})?)[)\]]?$`)
	lastNumberRe = regexp.MustCompile(`(\d{1,4})\D*$`)
	namesSepRe   = regexp.MustCompile(`\s*(?:[,;&/]|\band\b)\s*`)
)

// isWholeBook is an audiobook file format, a whole book with its chapters inside.
func isWholeBook(path string) bool { return strings.EqualFold(filepath.Ext(path), ".m4b") }

// ParseBookPath reads the author, series and book from the folders above
// a file, for files without tags.
func ParseBookPath(libraryRoot, path string) BookFile {
	var dirs []string
	if rel, err := filepath.Rel(libraryRoot, filepath.Dir(path)); err == nil && rel != "." {
		dirs = strings.Split(filepath.ToSlash(rel), "/")
	}
	at := func(n int) string { return filepath.Join(append([]string{libraryRoot}, dirs[:n]...)...) }
	b := BookFile{Part: bookTitle(stem(path))}
	if m := trackRe.FindStringSubmatch(stem(path)); m != nil {
		b.PartIndex, _ = strconv.Atoi(m[2])
		b.Part = bookTitle(strings.TrimLeft(m[3], " -."))
	} else if m := lastNumberRe.FindStringSubmatch(stem(path)); m != nil {
		b.PartIndex, _ = strconv.Atoi(m[1])
	}
	if n := len(dirs); n > 0 {
		if m := discDirRe.FindStringSubmatch(dirs[n-1]); m != nil && n > 1 {
			b.Disc, _ = strconv.Atoi(m[1])
			dirs = dirs[:n-1]
		}
	}
	switch {
	case isWholeBook(path):
		// The file is the book. It may still sit in a folder of its own.
		b.Whole, b.Book, b.PartIndex = true, bookTitle(stem(path)), 0
		if n := len(dirs); n > 0 && sameTitle(dirs[n-1], b.Book) {
			b.Book, b.BookDir = dirs[n-1], at(n)
			dirs = dirs[:n-1]
		}
	case len(dirs) > 0:
		b.Book, b.BookDir = dirs[len(dirs)-1], at(len(dirs))
		dirs = dirs[:len(dirs)-1]
	default:
		b.Book = b.Part
	}
	if len(dirs) > 0 {
		b.Author, b.AuthorDir = bookTitle(dirs[0]), at(1)
	}
	if len(dirs) > 1 {
		b.Series = bookTitle(dirs[len(dirs)-1])
	}
	b.Book = bookTitle(b.Book)
	if mi := parseMovieName(b.Book); mi.Year != 0 {
		b.Book, b.Year = mi.Title, mi.Year
	}
	if b.Series != "" {
		b.Book, b.SeriesIndex = splitSeriesIndex(b.Book)
	}
	return b
}

// bookTitle tidies a file or folder name into a title. Unlike a movie's,
// dots stay: they're in names like "Mr. Smith" or "Book 1.5".
func bookTitle(s string) string {
	return spacesRe.ReplaceAllString(strings.TrimSpace(strings.ReplaceAll(s, "_", " ")), " ")
}

// splitSeriesIndex takes a book's place in its series out of its title.
func splitSeriesIndex(title string) (string, *float64) {
	if m := seriesLeadRe.FindStringSubmatch(title); m != nil {
		if n, err := strconv.ParseFloat(m[1], 64); err == nil {
			return bookTitle(m[2]), &n
		}
	}
	if m := seriesTailRe.FindStringSubmatch(title); m != nil {
		if n, err := strconv.ParseFloat(m[2], 64); err == nil {
			return bookTitle(m[1]), &n
		}
	}
	return title, nil
}

// sameTitle is true when a folder is named after the book in it, give or
// take a year or a series number.
func sameTitle(dir, book string) bool {
	norm := func(s string) string {
		if mi := parseMovieName(s); mi.Year != 0 {
			s = mi.Title
		}
		s, _ = splitSeriesIndex(bookTitle(s))
		return strings.ToLower(s)
	}
	return norm(dir) == norm(book)
}

// bookFromTags prefers what the file's tags say over its folders.
func bookFromTags(tags map[string]string, b BookFile) BookFile {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(tags[k]); v != "" {
				return v
			}
		}
		return ""
	}
	if v := get("author", "album_artist", "albumartist", "artist"); v != "" {
		b.Author = v
	}
	if v := get("album"); v != "" {
		b.Book = v
	} else if v := get("title"); v != "" && b.Whole {
		b.Book = v
	}
	if v := get("title"); v != "" {
		b.Part = v
	}
	if v := get("narrator", "narratedby", "narrated_by", "reader", "composer"); v != "" {
		b.Narrators = splitNames(v)
	}
	if v := get("series", "mvnm", "movementname"); v != "" {
		b.Series = v
	}
	if v := get("series-part", "series_part", "seriespart", "mvin", "movement"); v != "" {
		if n, err := strconv.ParseFloat(strings.SplitN(v, "/", 2)[0], 64); err == nil {
			b.SeriesIndex = &n
		}
	}
	if b.Series != "" && b.SeriesIndex == nil {
		b.Book, b.SeriesIndex = splitSeriesIndex(b.Book)
	}
	if v := get("description", "synopsis", "long_description"); v != "" {
		b.Overview = v
	}
	if n := probe.LeadingInt(tags["track"]); n > 0 {
		b.PartIndex = n
	}
	if n := probe.LeadingInt(tags["disc"]); n > 0 {
		b.Disc = n
	}
	if n := probe.LeadingInt(get("date", "year")); n > 1000 {
		b.Year = n
	}
	if b.Author == "" {
		b.Author = "Unknown Author"
	}
	if b.Book == "" {
		b.Book = "Unknown Book"
	}
	return b
}

// splitNames reads "Jane Doe, John Roe & Sam Poe" as three people.
func splitNames(s string) []string {
	var out []string
	for _, n := range namesSepRe.Split(strings.TrimSpace(s), -1) {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// readNote reads the first of names in dir, a short text file next to a
// book, such as its description. It returns "" when there's none.
func readNote(dir string, names ...string) string {
	for _, n := range names {
		f, err := os.Open(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(f, 16<<10))
		f.Close()
		return strings.TrimSpace(string(b))
	}
	return ""
}

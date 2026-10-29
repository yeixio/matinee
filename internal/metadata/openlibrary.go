package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// OpenLibrary finds covers for audiobooks and photos of their authors on
// openlibrary.org, which needs no API key. Its covers are of the printed
// book, which is what most audiobook covers are made from.
type OpenLibrary struct {
	BaseURL   string // defaults to the public services; tests point them elsewhere
	CoverURL  string
	UserAgent string
	Client    *http.Client
	// Interval spaces requests, to be a polite user of a free service.
	Interval time.Duration

	mu   sync.Mutex
	last time.Time
}

func NewOpenLibrary(version string) *OpenLibrary {
	return &OpenLibrary{
		BaseURL:   "https://openlibrary.org",
		CoverURL:  "https://covers.openlibrary.org",
		UserAgent: "Matinee/" + version + " ( https://github.com/yeixio/matinee )",
		Client:    &http.Client{Timeout: 15 * time.Second},
		Interval:  500 * time.Millisecond,
	}
}

func (o *OpenLibrary) Name() string    { return "openlibrary" }
func (o *OpenLibrary) Kinds() []string { return []string{"author", "book"} }

func (o *OpenLibrary) Lookup(ctx context.Context, q Query) (*store.Metadata, error) {
	switch q.Kind {
	case "book":
		return o.book(ctx, q)
	case "author":
		return o.author(ctx, q.Title)
	}
	return nil, nil
}

func (o *OpenLibrary) book(ctx context.Context, q Query) (*store.Metadata, error) {
	titles := []string{q.Title}
	// "The Hobbit (Unabridged)" is printed as "The Hobbit".
	if c := plainBookTitle(q.Title); c != "" && c != q.Title {
		titles = append(titles, c)
	}
	for _, title := range titles {
		params := url.Values{"title": {title}, "limit": {"10"}, "fields": {"title,author_name,cover_i,first_publish_year"}}
		if q.Artist != "" {
			params.Set("author", q.Artist)
		}
		var res struct {
			Docs []struct {
				Title     string   `json:"title"`
				Authors   []string `json:"author_name"`
				CoverID   int64    `json:"cover_i"`
				FirstYear int      `json:"first_publish_year"`
			} `json:"docs"`
		}
		if err := o.get(ctx, "/search.json", params, &res); err != nil {
			return nil, err
		}
		// Search matches words loosely, so only the same title counts, by the
		// same author when there is one, and only an edition with a cover.
		for _, d := range res.Docs {
			if d.CoverID <= 0 || !sameName(d.Title, title) || q.Artist != "" && !anySameName(d.Authors, q.Artist) {
				continue
			}
			return &store.Metadata{
				Year:   d.FirstYear,
				Poster: o.CoverURL + "/b/id/" + strconv.FormatInt(d.CoverID, 10) + "-L.jpg",
			}, nil
		}
	}
	return nil, nil
}

func (o *OpenLibrary) author(ctx context.Context, name string) (*store.Metadata, error) {
	var res struct {
		Docs []struct {
			Key       string `json:"key"`
			Name      string `json:"name"`
			WorkCount int    `json:"work_count"`
		} `json:"docs"`
	}
	if err := o.get(ctx, "/search/authors.json", url.Values{"q": {name}, "limit": {"10"}}, &res); err != nil {
		return nil, err
	}
	// Several entries can share a name; the one with the most books is
	// most likely the author someone has audiobooks of.
	key, works := "", -1
	for _, d := range res.Docs {
		if sameName(d.Name, name) && d.WorkCount > works {
			key, works = d.Key, d.WorkCount
		}
	}
	if key == "" {
		return nil, nil
	}
	// default=false makes a missing photo a 404 rather than a blank image,
	// so the image endpoint falls back to the author's initials.
	return &store.Metadata{Poster: o.CoverURL + "/a/olid/" + url.PathEscape(key) + "-L.jpg?default=false"}, nil
}

// get calls the Open Library API, spacing out requests.
func (o *OpenLibrary) get(ctx context.Context, p string, params url.Values, out any) error {
	o.mu.Lock()
	wait := time.Until(o.last.Add(o.Interval))
	if wait > 0 {
		select {
		case <-ctx.Done():
			o.mu.Unlock()
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	o.last = time.Now()
	o.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, "GET", o.BaseURL+p+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", o.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := o.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// plainBookTitle takes notes like "(Unabridged)" or ": A Novel" off a title.
func plainBookTitle(t string) string {
	t = bookNoteRe.ReplaceAllString(t, "")
	if i := strings.Index(t, ": "); i > 0 {
		t = t[:i] // a subtitle
	}
	return strings.TrimSpace(t)
}

// bookNoteRe matches a bracketed note on an audiobook's title, like
// "(Unabridged)" or "[Dramatized Adaptation]".
var bookNoteRe = regexp.MustCompile(`(?i)\s*[(\[][^)\]]*\b(unabridged|abridged|audiobook|audio|dramati[sz]ed|narrated|edition|book\s*\d+)\b[^)\]]*[)\]]`)

// anySameName reports whether one of names is the author, or, for a
// folder like "Terry Pratchett & Neil Gaiman", one of them: names are
// compared by surname then.
func anySameName(names []string, author string) bool {
	last := func(s string) string {
		f := strings.Fields(s)
		if len(f) == 0 {
			return ""
		}
		return f[len(f)-1]
	}
	for _, n := range names {
		if sameName(n, author) {
			return true
		}
	}
	for _, n := range names {
		if l := last(n); l != "" && strings.Contains(strings.ToLower(author), strings.ToLower(l)) && len(l) > 2 {
			return true
		}
	}
	return false
}

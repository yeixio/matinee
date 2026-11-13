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
	"unicode"

	"github.com/yeixio/matinee/internal/store"
)

// Wikipedia describes games: the start of a game's English Wikipedia
// article as its summary, and its genres from Wikidata. Neither needs a
// key. Box art comes from the Games library itself (libretro's), so
// Wikipedia's pictures aren't used.
type Wikipedia struct {
	// API and Wikidata are the two sites' api.php; tests point them
	// elsewhere.
	API       string
	Wikidata  string
	UserAgent string
	Client    *http.Client
	// Interval spaces requests, to be a polite user of a free service.
	Interval time.Duration

	mu   sync.Mutex
	last time.Time
}

func NewWikipedia(version string) *Wikipedia {
	return &Wikipedia{
		API:       "https://en.wikipedia.org/w/api.php",
		Wikidata:  "https://www.wikidata.org/w/api.php",
		UserAgent: "Matinee/" + version + " (https://github.com/yeixio/matinee)",
		Client:    &http.Client{Timeout: 20 * time.Second},
		Interval:  200 * time.Millisecond,
	}
}

func (w *Wikipedia) Name() string    { return "wikipedia" }
func (w *Wikipedia) Kinds() []string { return []string{"game"} }

// wikiPage is one article, as the query API answers.
type wikiPage struct {
	Title     string `json:"title"`
	Missing   bool   `json:"missing"`
	Extract   string `json:"extract"`
	PageProps struct {
		Item  string `json:"wikibase_item"`
		Short string `json:"wikibase-shortdesc"`
	} `json:"pageprops"`
}

func (w *Wikipedia) Lookup(ctx context.Context, q Query) (*store.Metadata, error) {
	title := strings.TrimSpace(q.Title)
	if title == "" {
		return nil, nil
	}
	// The article named for the game, or for the game told apart from
	// other things of its name.
	tries := []string{title + " (video game)"}
	if q.Year > 0 {
		tries = append(tries, fmt.Sprintf("%s (%d video game)", title, q.Year))
	}
	tries = append(tries, title+" (arcade game)", title)
	if base, _, ok := strings.Cut(title, ":"); ok && len(base) > 3 {
		// "Street Fighter II: The World Warrior" is in "Street Fighter II".
		tries = append(tries, strings.TrimSpace(base)+" (video game)", strings.TrimSpace(base))
	}
	page, err := w.first(ctx, tries, q, false)
	if err != nil || page == nil {
		if err != nil {
			return nil, err
		}
		// Failing that, a search, keeping only an article with the same
		// title.
		found, err := w.search(ctx, title+" video game")
		if err != nil {
			return nil, err
		}
		if page, err = w.first(ctx, found, q, true); err != nil || page == nil {
			return nil, err
		}
	}
	m := &store.Metadata{Overview: summary(page.Extract)}
	if page.PageProps.Item != "" {
		// Genres are a nicety: a failure doesn't lose the summary.
		m.Genres, _ = w.genres(ctx, page.PageProps.Item)
	}
	return m, nil
}

// first is the first of titles (in order) that is an article about a
// video game that fits q. sameTitle also needs the article's title,
// without what's in brackets, to be the game's.
func (w *Wikipedia) first(ctx context.Context, titles []string, q Query, sameTitle bool) (*wikiPage, error) {
	if len(titles) == 0 {
		return nil, nil
	}
	v := url.Values{
		"action": {"query"}, "format": {"json"}, "formatversion": {"2"}, "redirects": {"1"},
		"prop": {"extracts|pageprops"}, "exintro": {"1"}, "explaintext": {"1"},
		"ppprop": {"wikibase_item|wikibase-shortdesc"}, "titles": {strings.Join(titles, "|")},
	}
	var r struct {
		Query struct {
			Normalized []struct{ From, To string } `json:"normalized"`
			Redirects  []struct{ From, To string } `json:"redirects"`
			Pages      []wikiPage                  `json:"pages"`
		} `json:"query"`
	}
	if err := w.get(ctx, w.API, v, &r); err != nil {
		return nil, err
	}
	follow := map[string]string{}
	for _, n := range r.Query.Normalized {
		follow[n.From] = n.To
	}
	for _, n := range r.Query.Redirects {
		follow[n.From] = n.To
	}
	pages := map[string]*wikiPage{}
	for i := range r.Query.Pages {
		pages[r.Query.Pages[i].Title] = &r.Query.Pages[i]
	}
	for _, t := range titles {
		for range 3 {
			if to, ok := follow[t]; ok {
				t = to
			}
		}
		p := pages[t]
		if p == nil || p.Missing || !aboutGame(p.PageProps.Short, q.Year) {
			continue
		}
		if sameTitle && looseWiki(stripBrackets(p.Title)) != looseWiki(q.Title) && looseWiki(stripBrackets(p.Title)) != looseWiki(beforeColon(q.Title)) {
			continue
		}
		return p, nil
	}
	return nil, nil
}

func (w *Wikipedia) search(ctx context.Context, text string) ([]string, error) {
	v := url.Values{"action": {"query"}, "format": {"json"}, "formatversion": {"2"}, "list": {"search"},
		"srsearch": {text}, "srlimit": {"5"}, "srnamespace": {"0"}, "srprop": {""}}
	var r struct {
		Query struct {
			Search []struct{ Title string } `json:"search"`
		} `json:"query"`
	}
	if err := w.get(ctx, w.API, v, &r); err != nil {
		return nil, err
	}
	var out []string
	for _, s := range r.Query.Search {
		out = append(out, s.Title)
	}
	return out, nil
}

var shortYear = regexp.MustCompile(`\b(19[5-9]\d|20\d\d)\b`)

// aboutGame says whether an article's short description ("1980 video
// game") is a game's, and of the right year when both are known: a remake
// of the same name is another game.
func aboutGame(short string, year int) bool {
	s := strings.ToLower(short)
	if !strings.Contains(s, "video game") && !strings.Contains(s, "arcade game") {
		return false
	}
	for _, not := range []string{"series", "franchise", "character", "company", "developer", "console", "list of", "media franchise"} {
		if strings.Contains(s, not) {
			return false
		}
	}
	if m := shortYear.FindString(s); m != "" && year > 0 {
		y, _ := strconv.Atoi(m)
		if y < year-2 || y > year+2 {
			return false
		}
	}
	return true
}

// genres are a Wikidata item's genres (P136), in English, written the way
// a genre row is named: "platform game" is Platform.
func (w *Wikipedia) genres(ctx context.Context, item string) ([]string, error) {
	var claims struct {
		Entities map[string]struct {
			Claims map[string][]struct {
				Mainsnak struct {
					Datavalue struct {
						Value struct {
							ID string `json:"id"`
						} `json:"value"`
					} `json:"datavalue"`
				} `json:"mainsnak"`
			} `json:"claims"`
		} `json:"entities"`
	}
	if err := w.get(ctx, w.Wikidata, url.Values{"action": {"wbgetentities"}, "format": {"json"}, "ids": {item}, "props": {"claims"}}, &claims); err != nil {
		return nil, err
	}
	var ids []string
	for _, c := range claims.Entities[item].Claims["P136"] {
		if id := c.Mainsnak.Datavalue.Value.ID; id != "" && len(ids) < 3 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var labels struct {
		Entities map[string]struct {
			Labels map[string]struct {
				Value string `json:"value"`
			} `json:"labels"`
		} `json:"entities"`
	}
	if err := w.get(ctx, w.Wikidata, url.Values{"action": {"wbgetentities"}, "format": {"json"}, "ids": {strings.Join(ids, "|")},
		"props": {"labels"}, "languages": {"en"}}, &labels); err != nil {
		return nil, err
	}
	var out []string
	for _, id := range ids {
		if g := genreName(labels.Entities[id].Labels["en"].Value); g != "" {
			out = append(out, g)
		}
	}
	return out, nil
}

// genreName is a Wikidata genre as a genre row is named: "platform game"
// and "maze video game" are Platform and Maze.
func genreName(label string) string {
	g := strings.TrimSpace(label)
	for _, suffix := range []string{" video game", " game"} {
		if s, ok := strings.CutSuffix(g, suffix); ok && s != "" {
			g = s
			break
		}
	}
	if g == "" {
		return ""
	}
	r := []rune(g)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// summary is the start of an article: its first paragraphs, up to about
// 1,000 characters, ending at a paragraph or a sentence.
func summary(extract string) string {
	var out []string
	n := 0
	for _, p := range strings.Split(strings.TrimSpace(extract), "\n") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if n > 0 && n+len(p) > 1000 {
			break
		}
		out = append(out, p)
		n += len(p)
	}
	s := strings.Join(out, "\n\n")
	if len(s) > 1200 {
		cut := s[:1200]
		if i := strings.LastIndex(cut, ". "); i > 200 {
			cut = cut[:i+1]
		}
		s = cut
	}
	return s
}

func stripBrackets(t string) string {
	if i := strings.Index(t, " ("); i > 0 {
		return t[:i]
	}
	return t
}

func beforeColon(t string) string {
	if b, _, ok := strings.Cut(t, ":"); ok {
		return b
	}
	return t
}

// looseWiki is a title for comparing: lower-case letters and digits only,
// "&" as "and".
func looseWiki(t string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.ReplaceAll(t, "&", "and")) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (w *Wikipedia) get(ctx context.Context, base string, v url.Values, out any) error {
	w.mu.Lock()
	if wait := w.Interval - time.Since(w.last); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			w.mu.Unlock()
			return ctx.Err()
		}
	}
	w.last = time.Now()
	w.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+v.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", w.UserAgent)
	resp, err := w.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", req.URL.Host, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

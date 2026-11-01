package podcast

import (
	"cmp"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Podcast Index (podcastindex.org) is an open directory of podcasts that
// anyone can use with a free key. Matinee asks it only to find shows: once
// someone follows one, its episodes come from the show's own feed.

// IndexBase is Podcast Index's API. The docs' screenshot server points it
// at a stand-in at build time.
var IndexBase = "https://api.podcastindex.org/api/1.0"

// IndexKey is an admin's Podcast Index key and secret, from
// api.podcastindex.org. They stay on the server.
type IndexKey struct {
	Key    string `json:"key"`
	Secret string `json:"secret"`
}

// Index searches Podcast Index.
type Index struct {
	Key  IndexKey
	Base string // IndexBase, or a test server
	HTTP *http.Client
}

// Show is a podcast found in the directory.
type Show struct {
	IndexID     int64    `json:"indexId"`
	Title       string   `json:"title"`
	Author      string   `json:"author,omitempty"`
	Description string   `json:"description,omitempty"`
	Image       string   `json:"image,omitempty"`
	FeedURL     string   `json:"feedUrl"`
	Link        string   `json:"link,omitempty"`
	Language    string   `json:"language,omitempty"`
	Categories  []string `json:"categories,omitempty"`
	Episodes    int      `json:"episodes,omitempty"`
}

var (
	// ErrBadKey means Podcast Index turned the key down.
	ErrBadKey = errors.New("Podcast Index didn't accept the key")
	// ErrNoKey means no key was set.
	ErrNoKey = errors.New("no Podcast Index key")
)

// Search finds shows by name, author or subject.
func (ix *Index) Search(ctx context.Context, q string, max int) ([]Show, error) {
	return ix.shows(ctx, "/search/byterm", url.Values{"q": {q}, "max": {strconv.Itoa(max)}, "clean": {""}})
}

// Trending lists shows getting popular now, in a language (like en or de)
// when one is given, and in a category (see Categories) when one is given.
func (ix *Index) Trending(ctx context.Context, lang, category string, max int) ([]Show, error) {
	v := url.Values{"max": {strconv.Itoa(max)}}
	if lang != "" {
		v.Set("lang", lang)
	}
	if c, ok := CategoryByID(category); ok {
		v.Set("cat", c.ids)
	}
	return ix.shows(ctx, "/podcasts/trending", v)
}

// Category is a kind of show to browse, like Comedy or News. ID is
// Matinee's own name for it, which the apps translate; Name is English.
type Category struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	ids  string // Podcast Index's category numbers
}

// Categories are the kinds of shows people can browse, the main ones in
// Podcast Index's list (github.com/Podcastindex-org/podcast-namespace,
// categories.json), some with their close neighbors.
var Categories = []Category{
	{"arts", "Arts", "1"},
	{"business", "Business", "9"},
	{"comedy", "Comedy", "16"},
	{"education", "Education", "20"},
	{"fiction", "Fiction", "26"},
	{"government", "Government", "58"},
	{"health", "Health & fitness", "29,30"},
	{"history", "History", "28"},
	{"kids", "Kids & family", "36,37"},
	{"leisure", "Leisure", "42"},
	{"music", "Music", "53"},
	{"news", "News", "55"},
	{"religion", "Religion & spirituality", "65,66"},
	{"science", "Science", "67"},
	{"society", "Society & culture", "77,78"},
	{"sports", "Sports", "86"},
	{"technology", "Technology", "102"},
	{"trueCrime", "True crime", "103"},
	{"tv", "TV & film", "104,105"},
}

// CategoryByID finds a category by its ID.
func CategoryByID(id string) (Category, bool) {
	for _, c := range Categories {
		if c.ID == id {
			return c, true
		}
	}
	return Category{}, false
}

// Check asks Podcast Index whether it accepts the key.
func (ix *Index) Check(ctx context.Context) error {
	_, err := ix.Search(ctx, "news", 1)
	return err
}

type indexFeed struct {
	ID           int64             `json:"id"`
	Title        string            `json:"title"`
	URL          string            `json:"url"`
	Link         string            `json:"link"`
	Description  string            `json:"description"`
	Author       string            `json:"author"`
	OwnerName    string            `json:"ownerName"`
	Image        string            `json:"image"`
	Artwork      string            `json:"artwork"`
	Language     string            `json:"language"`
	Categories   map[string]string `json:"categories"`
	EpisodeCount int               `json:"episodeCount"`
	Dead         int               `json:"dead"`
}

func (ix *Index) shows(ctx context.Context, path string, q url.Values) ([]Show, error) {
	var res struct {
		Feeds []indexFeed `json:"feeds"`
	}
	if err := ix.get(ctx, path, q, &res); err != nil {
		return nil, err
	}
	out := []Show{}
	for _, f := range res.Feeds {
		if f.Dead != 0 || f.URL == "" {
			continue
		}
		s := Show{
			IndexID: f.ID, Title: clean(f.Title), Author: clean(cmp.Or(f.Author, f.OwnerName)),
			Description: Text(f.Description), Image: cmp.Or(f.Artwork, f.Image), FeedURL: f.URL,
			Link: f.Link, Language: strings.ToLower(f.Language), Episodes: f.EpisodeCount,
		}
		keys := make([]string, 0, len(f.Categories))
		for k := range f.Categories {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { a, _ := strconv.Atoi(keys[i]); b, _ := strconv.Atoi(keys[j]); return a < b })
		for _, k := range keys {
			s.Categories = append(s.Categories, f.Categories[k])
		}
		out = append(out, s)
	}
	return out, nil
}

func (ix *Index) get(ctx context.Context, path string, q url.Values, v any) error {
	if ix.Key.Key == "" || ix.Key.Secret == "" {
		return ErrNoKey
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cmp.Or(ix.Base, IndexBase)+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	date := strconv.FormatInt(time.Now().Unix(), 10)
	sum := sha1.Sum([]byte(ix.Key.Key + ix.Key.Secret + date))
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Auth-Key", ix.Key.Key)
	req.Header.Set("X-Auth-Date", date)
	req.Header.Set("Authorization", hex.EncodeToString(sum[:]))
	client := ix.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrBadKey
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("Podcast Index answered %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(v)
}

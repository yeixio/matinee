package livetv

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// iptv-org (github.com/iptv-org/iptv) keeps lists of TV channels that are
// streamed publicly on the internet, by country, category and language.
// Matinee suggests them as a place to start: an admin picks what they want
// and Matinee reads the matching lists, like any other playlist. iptv-org
// only lists where channels stream; it doesn't host them.

// IPTVOrgBase is where iptv-org publishes its lists. The docs' screenshot
// server points it at a stand-in at build time.
var IPTVOrgBase = "https://iptv-org.github.io"

// Filter picks channels from iptv-org's lists. Within one kind, a channel
// may match any; across kinds, it must match each kind that has some. An
// empty filter is every channel.
type Filter struct {
	Countries  []string `json:"countries,omitempty"`  // two letters, such as US
	Categories []string `json:"categories,omitempty"` // such as news
	Languages  []string `json:"languages,omitempty"`  // three letters, such as eng
}

// Empty says the filter picks everything.
func (f Filter) Empty() bool {
	return len(f.Countries) == 0 && len(f.Categories) == 0 && len(f.Languages) == 0
}

// Playlists are the lists to read for a filter, one set per kind: a
// channel is kept when it is in some list of every set.
func (f Filter) Playlists(base string) [][]string {
	base = cmp.Or(base, IPTVOrgBase) + "/iptv/"
	if f.Empty() {
		return [][]string{{base + "index.m3u"}}
	}
	var sets [][]string
	add := func(dir string, codes []string, lower bool) {
		var set []string
		for _, c := range codes {
			c = strings.TrimSpace(c)
			if lower {
				c = strings.ToLower(c)
			}
			if c == "" || strings.ContainsAny(c, "/.?#") || c == "xxx" {
				continue
			}
			set = append(set, base+dir+"/"+c+".m3u")
		}
		if len(set) > 0 {
			sets = append(sets, set)
		}
	}
	add("countries", f.Countries, true)
	add("categories", f.Categories, true)
	add("languages", f.Languages, true)
	return sets
}

// Choice is something an admin can pick in a filter.
type Choice struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Flag string `json:"flag,omitempty"` // countries only
}

// Choices are the countries, categories and languages iptv-org has.
type Choices struct {
	Countries  []Choice `json:"countries"`
	Categories []Choice `json:"categories"`
	Languages  []Choice `json:"languages"`
}

// categories are iptv-org's, in case its list can't be fetched.
var categories = []Choice{
	{"animation", "Animation", ""}, {"auto", "Auto", ""}, {"business", "Business", ""}, {"classic", "Classic", ""},
	{"comedy", "Comedy", ""}, {"cooking", "Cooking", ""}, {"culture", "Culture", ""}, {"documentary", "Documentary", ""},
	{"education", "Education", ""}, {"entertainment", "Entertainment", ""}, {"family", "Family", ""}, {"general", "General", ""},
	{"kids", "Kids", ""}, {"legislative", "Legislative", ""}, {"lifestyle", "Lifestyle", ""}, {"movies", "Movies", ""},
	{"music", "Music", ""}, {"news", "News", ""}, {"outdoor", "Outdoor", ""}, {"public", "Public", ""}, {"relax", "Relax", ""},
	{"religious", "Religious", ""}, {"science", "Science", ""}, {"series", "Series", ""}, {"shop", "Shop", ""},
	{"sports", "Sports", ""}, {"travel", "Travel", ""}, {"weather", "Weather", ""},
}

// Catalog fetches iptv-org's choices, and keeps them for a day.
type Catalog struct {
	Client *http.Client
	Base   string // empty is IPTVOrgBase

	mu      sync.Mutex
	choices *Choices
	at      time.Time
}

// Choices returns what an admin can pick from.
func (c *Catalog) Choices(ctx context.Context) (Choices, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.choices != nil && time.Since(c.at) < 24*time.Hour {
		return *c.choices, nil
	}
	base := cmp.Or(c.Base, IPTVOrgBase) + "/api/"
	var out Choices
	var countries []struct{ Name, Code, Flag string }
	if err := getJSON(ctx, c.Client, base+"countries.json", &countries); err != nil {
		return out, err
	}
	for _, x := range countries {
		if x.Code != "" && x.Name != "" {
			out.Countries = append(out.Countries, Choice{Code: x.Code, Name: x.Name, Flag: x.Flag})
		}
	}
	var languages []struct{ Name, Code string }
	if err := getJSON(ctx, c.Client, base+"languages.json", &languages); err != nil {
		return out, err
	}
	for _, x := range languages {
		if x.Code != "" && x.Name != "" {
			out.Languages = append(out.Languages, Choice{Code: x.Code, Name: x.Name})
		}
	}
	var cats []struct{ ID, Name string }
	if err := getJSON(ctx, c.Client, base+"categories.json", &cats); err == nil {
		for _, x := range cats {
			if x.ID != "" && x.Name != "" && x.ID != "xxx" {
				out.Categories = append(out.Categories, Choice{Code: x.ID, Name: x.Name})
			}
		}
	}
	if len(out.Categories) == 0 {
		out.Categories = slices.Clone(categories)
	}
	byName := func(a, b Choice) int { return strings.Compare(a.Name, b.Name) }
	slices.SortFunc(out.Countries, byName)
	slices.SortFunc(out.Languages, byName)
	slices.SortFunc(out.Categories, byName)
	c.choices, c.at = &out, time.Now()
	return out, nil
}

func getJSON(ctx context.Context, client *http.Client, addr string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", addr, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(v)
}

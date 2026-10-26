package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// AwardSource looks up the awards a title won or was nominated for.
type AwardSource interface {
	Awards(ctx context.Context, wikidataID string) ([]store.Award, error)
}

// Wikidata looks up awards on wikidata.org, which needs no key. TMDB knows
// each title's Wikidata id.
type Wikidata struct {
	Endpoint  string // SPARQL endpoint; tests point it elsewhere
	UserAgent string
	Client    *http.Client
}

func NewWikidata(version string) *Wikidata {
	return &Wikidata{
		Endpoint:  "https://query.wikidata.org/sparql",
		UserAgent: "Matinee/" + version + " (https://github.com/yeixio/matinee)",
		Client:    &http.Client{Timeout: 30 * time.Second},
	}
}

var wikidataID = regexp.MustCompile(`^Q\d+$`)

// awardQuery finds awards given to the title itself, and to people for
// their work on it, such as an acting award.
const awardQuery = `SELECT ?award ?awardLabel ?won ?year ?whoLabel WHERE {
  {
    wd:%[1]s p:P166 ?st . ?st ps:P166 ?award . BIND(true AS ?won)
    OPTIONAL { ?st pq:P1346 ?who }
  } UNION {
    wd:%[1]s p:P1411 ?st . ?st ps:P1411 ?award . BIND(false AS ?won)
    OPTIONAL { ?st pq:P2453 ?who }
  } UNION {
    ?who p:P166 ?st . ?st ps:P166 ?award ; pq:P1686 wd:%[1]s . BIND(true AS ?won)
  } UNION {
    ?who p:P1411 ?st . ?st ps:P1411 ?award ; pq:P1686 wd:%[1]s . BIND(false AS ?won)
  }
  OPTIONAL { ?st pq:P585 ?time }
  BIND(YEAR(?time) AS ?year)
  SERVICE wikibase:label { bd:serviceParam wikibase:language "en". }
}`

func (w *Wikidata) Awards(ctx context.Context, id string) ([]store.Award, error) {
	if !wikidataID.MatchString(id) {
		return nil, nil
	}
	params := url.Values{"query": {fmt.Sprintf(awardQuery, id)}, "format": {"json"}}
	req, err := http.NewRequestWithContext(ctx, "GET", w.Endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	// Wikidata turns away requests without a descriptive User-Agent.
	req.Header.Set("User-Agent", w.UserAgent)
	req.Header.Set("Accept", "application/sparql-results+json")
	res, err := w.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wikidata: HTTP %d", res.StatusCode)
	}
	type value struct {
		Value string `json:"value"`
	}
	var out struct {
		Results struct {
			Bindings []map[string]value `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	type key struct {
		award string
		year  int
		won   bool
	}
	var order []key
	awards := map[key]*store.Award{}
	for _, b := range out.Results.Bindings {
		name := b["awardLabel"].Value
		// Unlabelled entities come back as their id.
		if name == "" || wikidataID.MatchString(name) {
			continue
		}
		year, _ := strconv.Atoi(b["year"].Value)
		k := key{b["award"].Value, year, b["won"].Value == "true"}
		a := awards[k]
		if a == nil {
			a = &store.Award{Name: name, Year: year, Won: k.won}
			awards[k] = a
			order = append(order, k)
		}
		if who := b["whoLabel"].Value; who != "" && !wikidataID.MatchString(who) && !slices.Contains(a.Names, who) {
			a.Names = append(a.Names, who)
		}
	}
	list := []store.Award{}
	for _, k := range order {
		// A win is often also listed as a nomination.
		if !k.won && awards[key{k.award, k.year, true}] != nil {
			continue
		}
		list = append(list, *awards[k])
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Won != list[j].Won {
			return list[i].Won
		}
		if list[i].Year != list[j].Year {
			return list[i].Year > list[j].Year
		}
		return list[i].Name < list[j].Name
	})
	return list, nil
}

// majorAwards are the awards worth naming in a title's one-line summary, in
// order of preference.
var majorAwards = []struct{ prefix, one, many string }{
	{"Academy Award for", "Academy Award", "Academy Awards"},
	{"Primetime Emmy Award for", "Primetime Emmy", "Primetime Emmys"},
	{"Golden Globe Award for", "Golden Globe", "Golden Globes"},
	{"BAFTA Award for", "BAFTA", "BAFTAs"},
	{"British Academy Film Award for", "BAFTA", "BAFTAs"},
}

// AwardSummary sums awards up in a line, e.g. "Won 4 Academy Awards · 31
// wins and 62 nominations".
func AwardSummary(awards []store.Award) string {
	if len(awards) == 0 {
		return ""
	}
	wins, noms := 0, 0
	for _, a := range awards {
		if a.Won {
			wins++
		} else {
			noms++
		}
	}
	plural := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return strconv.Itoa(n) + " " + many
	}
	var parts []string
	for _, won := range []bool{true, false} {
		for _, m := range majorAwards {
			n := 0
			for _, a := range awards {
				if a.Won == won && strings.HasPrefix(a.Name, m.prefix) {
					n++
				}
			}
			if n == 0 {
				continue
			}
			if won {
				parts = append(parts, "Won "+plural(n, m.one, m.many))
			} else {
				parts = append(parts, "Nominated for "+plural(n, m.one, m.many))
			}
			break
		}
		if len(parts) > 0 {
			break
		}
	}
	var total []string
	if wins > 0 {
		total = append(total, plural(wins, "win", "wins"))
	}
	if noms > 0 {
		total = append(total, plural(noms, "nomination", "nominations"))
	}
	return strings.Join(append(parts, strings.Join(total, " and ")), " · ")
}

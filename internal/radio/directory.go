package radio

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Radio Browser (radio-browser.info) is a free, open directory of radio
// stations that anyone can search without a key. Matinee asks it only to
// find stations: once someone adds one, it plays from the station itself.

// DirectoryBase is Radio Browser's API. The docs' screenshot server points
// it at a stand-in at build time.
var DirectoryBase = "https://all.api.radio-browser.info"

// Found is a station in the directory.
type Found struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Homepage string   `json:"homepage,omitempty"`
	Image    string   `json:"image,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Country  string   `json:"country,omitempty"` // two letters, such as GB
	Codec    string   `json:"codec,omitempty"`   // such as MP3 or AAC
	Bitrate  int      `json:"bitrate,omitempty"` // kbit/s
}

// Search finds stations by name, or by tag (genre) when byTag, the most
// listened first. Stations that only stream as HLS are left out.
func Search(ctx context.Context, client *http.Client, base, q string, byTag bool) ([]Found, error) {
	v := url.Values{"limit": {"40"}, "hidebroken": {"true"}, "order": {"clickcount"}, "reverse": {"true"}}
	if byTag {
		v.Set("tag", q)
	} else {
		v.Set("name", q)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cmp.Or(base, DirectoryBase)+"/json/stations/search?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Radio Browser answered %s", resp.Status)
	}
	var rows []struct {
		Name        string `json:"name"`
		URL         string `json:"url"`
		URLResolved string `json:"url_resolved"`
		Homepage    string `json:"homepage"`
		Favicon     string `json:"favicon"`
		Tags        string `json:"tags"`
		CountryCode string `json:"countrycode"`
		Codec       string `json:"codec"`
		Bitrate     int    `json:"bitrate"`
		HLS         int    `json:"hls"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rows); err != nil {
		return nil, err
	}
	out := []Found{}
	seen := map[string]bool{}
	for _, s := range rows {
		addr := cmp.Or(s.URLResolved, s.URL)
		name := clean(s.Name)
		if s.HLS == 1 || strings.Contains(strings.ToLower(addr), ".m3u8") || name == "" || seen[addr] {
			continue
		}
		if _, err := Normalize(addr); err != nil {
			continue
		}
		seen[addr] = true
		f := Found{Name: name, URL: addr, Homepage: s.Homepage, Country: s.CountryCode, Codec: s.Codec, Bitrate: s.Bitrate}
		if strings.HasPrefix(s.Favicon, "https://") || strings.HasPrefix(s.Favicon, "http://") {
			f.Image = s.Favicon
		}
		for _, t := range strings.Split(s.Tags, ",") {
			if t = strings.TrimSpace(t); t != "" && len(f.Tags) < 5 {
				f.Tags = append(f.Tags, t)
			}
		}
		out = append(out, f)
	}
	return out, nil
}

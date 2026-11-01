package subtitle

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OpenSubtitles (opensubtitles.com) is a large library of subtitle files
// that anyone can search with a free API key. An admin enters the key, and
// optionally the username and password of a free account, which raises
// how many files the server may fetch a day.

// OpenSubtitlesBase is the API. The docs' screenshot server points it at a
// stand-in at build time.
var OpenSubtitlesBase = "https://api.opensubtitles.com/api/v1"

// OpenSubtitlesKey is what an admin entered. It stays on the server.
type OpenSubtitlesKey struct {
	APIKey   string `json:"apiKey"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

var (
	// ErrBadKey means OpenSubtitles turned the API key down.
	ErrBadKey = errors.New("OpenSubtitles didn't accept the API key")
	// ErrBadLogin means OpenSubtitles turned the username or password down.
	ErrBadLogin = errors.New("OpenSubtitles didn't accept the username or password")
	// ErrQuota means today's downloads are used up.
	ErrQuota = errors.New("today's OpenSubtitles downloads are used up")
)

// OpenSubtitles searches opensubtitles.com and fetches its files.
type OpenSubtitles struct {
	Key       OpenSubtitlesKey
	Base      string // OpenSubtitlesBase, or a test server
	UserAgent string // "Matinee v1.2.3": the API asks every app to name itself
	HTTP      *http.Client

	mu      sync.Mutex
	token   string
	tokenAt time.Time
	// base is where a signed-in account's requests go; VIP accounts get
	// their own server.
	base string
}

// tokenLife is how long a sign-in is used for before signing in again.
// Signing in is limited to about one a second, so it isn't done per request.
const tokenLife = 12 * time.Hour

// Query is what's known about a video.
type Query struct {
	// TMDBID is a movie's TMDB id; ShowTMDBID with Season and Episode
	// find an episode.
	TMDBID     int64
	ShowTMDBID int64
	Season     int
	Episode    int
	// Title and Year find a video with no TMDB id: an episode's show title.
	Title string
	Year  int
	// Hash is the file's OpenSubtitles hash (see FileHash): subtitles made
	// for this very file are marked.
	Hash string
	// Languages are ISO 639-1 codes, or the API's own, such as pt-br.
	Languages []string
}

// Found is a subtitle file the search turned up.
type Found struct {
	// ID is the API's file id, passed back to add it.
	ID       int64  `json:"id"`
	Language string `json:"language,omitempty"` // ISO 639-1, such as en
	// Variant is the API's own language code where it says more, such as
	// pt-br or zh-tw.
	Variant string `json:"variant,omitempty"`
	Label   string `json:"label"` // in English, such as "Portuguese (Brazil)"
	Release string `json:"release,omitempty"`
	SDH     bool   `json:"sdh,omitempty"`
	Forced  bool   `json:"forced,omitempty"`
	// Machine is true for files a machine translated.
	Machine   bool `json:"machine,omitempty"`
	Downloads int  `json:"downloads"`
	// MatchesFile is true when the file was made for this very video file,
	// so its timing fits.
	MatchesFile bool `json:"matchesFile,omitempty"`
	// Trusted is true when a trusted member uploaded it.
	Trusted bool `json:"trusted,omitempty"`
}

// Search finds subtitles for a video: those made for the file itself
// first, then the most downloaded.
func (o *OpenSubtitles) Search(ctx context.Context, q Query) ([]Found, error) {
	langs := make([]string, 0, len(q.Languages))
	for _, l := range q.Languages {
		if c := APILanguage(l); c != "" && !slices.Contains(langs, c) {
			langs = append(langs, c)
		}
	}
	slices.Sort(langs)
	base := url.Values{}
	if len(langs) > 0 {
		base.Set("languages", strings.Join(langs, ","))
	}
	var searches []url.Values
	if q.Hash != "" {
		v := clone(base)
		v.Set("moviehash", q.Hash)
		searches = append(searches, v)
	}
	v := clone(base)
	switch {
	case q.ShowTMDBID != 0 && q.Season > 0 && q.Episode > 0:
		v.Set("parent_tmdb_id", strconv.FormatInt(q.ShowTMDBID, 10))
		v.Set("season_number", strconv.Itoa(q.Season))
		v.Set("episode_number", strconv.Itoa(q.Episode))
	case q.TMDBID != 0:
		v.Set("tmdb_id", strconv.FormatInt(q.TMDBID, 10))
	case q.Title != "":
		v.Set("query", strings.ToLower(q.Title))
		if q.Season > 0 && q.Episode > 0 {
			v.Set("season_number", strconv.Itoa(q.Season))
			v.Set("episode_number", strconv.Itoa(q.Episode))
		} else if q.Year > 0 {
			v.Set("year", strconv.Itoa(q.Year))
		}
	default:
		v = nil
	}
	if v != nil {
		searches = append(searches, v)
	}

	byID := map[int64]int{}
	var out []Found
	for i, v := range searches {
		var res searchResponse
		if err := o.call(ctx, http.MethodGet, "/subtitles?"+v.Encode(), nil, &res); err != nil {
			if i > 0 && len(out) > 0 {
				break // keep what the first search found
			}
			return nil, err
		}
		for _, d := range res.Data {
			f, ok := d.found()
			if !ok {
				continue
			}
			if j, seen := byID[f.ID]; seen {
				out[j].MatchesFile = out[j].MatchesFile || f.MatchesFile
				continue
			}
			if v.Get("moviehash") != "" && v.Get("tmdb_id") == "" && v.Get("parent_tmdb_id") == "" {
				// A search by hash alone only finds files made for this one.
				f.MatchesFile = f.MatchesFile || d.Attributes.MoviehashMatch == nil
			}
			byID[f.ID] = len(out)
			out = append(out, f)
		}
	}
	slices.SortStableFunc(out, func(a, b Found) int {
		if a.MatchesFile != b.MatchesFile {
			return boolFirst(a.MatchesFile)
		}
		if a.Machine != b.Machine {
			return boolFirst(!a.Machine)
		}
		return cmp.Compare(b.Downloads, a.Downloads)
	})
	return out, nil
}

func boolFirst(a bool) int {
	if a {
		return -1
	}
	return 1
}

func clone(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = slices.Clone(vs)
	}
	return out
}

type flexBool bool

// UnmarshalJSON takes true, false, 1, 0, "1" or "0": the API isn't consistent.
func (b *flexBool) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	*b = s == "true" || s == "1"
	return nil
}

type searchResponse struct {
	Data []searchItem `json:"data"`
}

type searchItem struct {
	ID         string `json:"id"`
	Attributes struct {
		Language         string    `json:"language"`
		DownloadCount    int       `json:"download_count"`
		HearingImpaired  flexBool  `json:"hearing_impaired"`
		ForeignPartsOnly flexBool  `json:"foreign_parts_only"`
		MachineTrans     flexBool  `json:"machine_translated"`
		AITranslated     flexBool  `json:"ai_translated"`
		FromTrusted      flexBool  `json:"from_trusted"`
		MoviehashMatch   *flexBool `json:"moviehash_match"`
		Release          string    `json:"release"`
		Files            []struct {
			FileID   int64  `json:"file_id"`
			FileName string `json:"file_name"`
		} `json:"files"`
	} `json:"attributes"`
}

// found turns a search result into a Found. Subtitles split over several
// files, one per disc, are left out: they fit no single video file.
func (d searchItem) found() (Found, bool) {
	a := d.Attributes
	if len(a.Files) != 1 || a.Files[0].FileID == 0 {
		return Found{}, false
	}
	f := Found{
		ID: a.Files[0].FileID, Release: strings.TrimSpace(a.Release),
		SDH: bool(a.HearingImpaired), Forced: bool(a.ForeignPartsOnly),
		Machine: bool(a.MachineTrans) || bool(a.AITranslated), Downloads: a.DownloadCount,
		Trusted: bool(a.FromTrusted),
	}
	if a.MoviehashMatch != nil {
		f.MatchesFile = bool(*a.MoviehashMatch)
	}
	if f.Release == "" {
		f.Release = strings.TrimSuffix(a.Files[0].FileName, ".srt")
	}
	f.Language, f.Variant, f.Label = fromAPILanguage(a.Language)
	if f.Label == "" {
		f.Label = a.Language
	}
	return f, true
}

// APILanguage turns a language code into the API's own: pt-BR is pt-br,
// Simplified Chinese is zh-cn, Norwegian Bokmål is no. Unknown ones give "".
func APILanguage(code string) string {
	k := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), "_", "-"))
	switch k {
	case "pt-br", "pob":
		return "pt-br"
	case "pt-pt":
		return "pt-pt"
	case "zh-hans", "zh-cn", "zh-sg", "chs":
		return "zh-cn"
	case "zh-hant", "zh-tw", "zh-hk", "cht":
		return "zh-tw"
	case "es-419", "es-la", "es-mx":
		return "ea"
	}
	c, _ := lookupLanguage(k)
	if c == "" {
		c, _ = lookupLanguage(strings.SplitN(k, "-", 2)[0])
	}
	switch c {
	case "":
		return ""
	case "pt":
		return "pt-pt"
	case "zh":
		return "zh-cn"
	case "nb":
		return "no"
	}
	return c
}

// APILanguageInfo is fromAPILanguage for a code from a client, which may
// be the API's own (pt-br) or ISO 639-1 (pt).
func APILanguageInfo(code string) (iso, variant, name string) {
	return fromAPILanguage(code)
}

// FoundLabel is a found subtitle's label and note, from its language's
// name: "Portuguese (Brazil, SDH)".
func FoundLabel(name string, forced, sdh bool) (string, string) {
	if name == "" {
		name = "Subtitles"
	}
	lang, region, _ := strings.Cut(strings.TrimSuffix(name, ")"), " (")
	return label(lang, region, forced, sdh), region
}

// fromAPILanguage turns the API's language code into an ISO 639-1 code,
// the API's code where it says more, and an English name.
func fromAPILanguage(api string) (code, variant, name string) {
	k := strings.ToLower(api)
	switch k {
	case "pt-pt":
		return "pt", "", "Portuguese"
	case "pt-br":
		return "pt", "pt-br", "Portuguese (Brazil)"
	case "ea":
		return "es", "ea", "Spanish (Latin America)"
	case "zh-cn":
		return "zh", "zh-cn", "Chinese (Simplified)"
	case "zh-tw":
		return "zh", "zh-tw", "Chinese (Traditional)"
	case "ze":
		return "zh", "ze", "Chinese and English"
	}
	code, name = lookupLanguage(k)
	return code, "", name
}

// Download fetches a file the search found, as SubRip. Remaining is how
// many more files the account may fetch today, where the API says.
func (o *OpenSubtitles) Download(ctx context.Context, id int64) (data []byte, remaining int, err error) {
	var res struct {
		Link      string `json:"link"`
		Remaining int    `json:"remaining"`
		Message   string `json:"message"`
	}
	body, _ := json.Marshal(map[string]any{"file_id": id, "sub_format": "srt"})
	if err := o.call(ctx, http.MethodPost, "/download", body, &res); err != nil {
		return nil, 0, err
	}
	if res.Link == "" {
		if res.Remaining <= 0 {
			return nil, 0, ErrQuota
		}
		return nil, 0, fmt.Errorf("opensubtitles: no download link: %s", res.Message)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, res.Link, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", o.UserAgent)
	resp, err := o.client().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("opensubtitles: download: %s", resp.Status)
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxSubtitleSize+1))
	if err != nil {
		return nil, 0, err
	}
	if len(data) > maxSubtitleSize {
		return nil, 0, errors.New("opensubtitles: the file is too big")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, 0, errors.New("opensubtitles: the file is empty")
	}
	return data, res.Remaining, nil
}

// maxSubtitleSize is far more than any film's subtitles need.
const maxSubtitleSize = 5 << 20

// Check makes sure the API key works, and the username and password when
// they're given.
func (o *OpenSubtitles) Check(ctx context.Context) error {
	if o.Key.Username != "" {
		o.mu.Lock()
		o.token = ""
		o.mu.Unlock()
		_, _, err := o.signIn(ctx)
		return err
	}
	return o.call(ctx, http.MethodGet, "/infos/formats", nil, nil)
}

func (o *OpenSubtitles) client() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (o *OpenSubtitles) apiBase() string {
	if o.Base != "" {
		return strings.TrimSuffix(o.Base, "/")
	}
	return OpenSubtitlesBase
}

// signIn returns a token for the account, signing in when there's none
// yet, and where its requests go.
func (o *OpenSubtitles) signIn(ctx context.Context) (token, base string, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.token != "" && time.Since(o.tokenAt) < tokenLife {
		return o.token, o.base, nil
	}
	body, _ := json.Marshal(map[string]string{"username": o.Key.Username, "password": o.Key.Password})
	var res struct {
		Token   string `json:"token"`
		BaseURL string `json:"base_url"`
	}
	status, err := o.do(ctx, o.apiBase(), "", http.MethodPost, "/login", body, &res)
	switch {
	case status == http.StatusUnauthorized:
		return "", "", ErrBadLogin
	case err != nil:
		return "", "", err
	case res.Token == "":
		return "", "", ErrBadLogin
	}
	o.token, o.tokenAt, o.base = res.Token, time.Now(), o.apiBase()
	if b := strings.TrimSpace(res.BaseURL); b != "" && o.Base == "" {
		// A host such as vip-api.opensubtitles.com.
		if !strings.Contains(b, "://") {
			b = "https://" + b
		}
		if u, err := url.Parse(b); err == nil && u.Host != "" {
			o.base = "https://" + u.Host + "/api/v1"
		}
	}
	return o.token, o.base, nil
}

// call makes a request, signed in when there's an account, and signs in
// again once if the token has expired.
func (o *OpenSubtitles) call(ctx context.Context, method, path string, body []byte, out any) error {
	for attempt := 0; ; attempt++ {
		token, base := "", o.apiBase()
		if o.Key.Username != "" {
			var err error
			if token, base, err = o.signIn(ctx); err != nil {
				return err
			}
		}
		status, err := o.do(ctx, base, token, method, path, body, out)
		if status == http.StatusUnauthorized && token != "" && attempt == 0 {
			o.mu.Lock()
			o.token = ""
			o.mu.Unlock()
			continue
		}
		return err
	}
}

func (o *OpenSubtitles) do(ctx context.Context, base, token, method, path string, body []byte, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Api-Key", o.Key.APIKey)
	req.Header.Set("User-Agent", o.UserAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := o.client().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	switch {
	case resp.StatusCode == http.StatusForbidden:
		return resp.StatusCode, ErrBadKey
	case resp.StatusCode == http.StatusUnauthorized && token == "" && path != "/login":
		return resp.StatusCode, ErrBadKey
	case resp.StatusCode == http.StatusNotAcceptable || resp.StatusCode == http.StatusProxyAuthRequired:
		// 406 and 407 are how the API says the day's downloads are used up.
		return resp.StatusCode, ErrQuota
	case resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated:
		return resp.StatusCode, fmt.Errorf("opensubtitles %s %s: %s: %s", method, strings.SplitN(path, "?", 2)[0], resp.Status, bytes.TrimSpace(raw[:min(len(raw), 200)]))
	}
	if out == nil {
		return resp.StatusCode, nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return resp.StatusCode, fmt.Errorf("opensubtitles: %w", err)
	}
	return resp.StatusCode, nil
}

// FileHash is a video's OpenSubtitles hash: its size plus the sum of its
// first and last 64 KB as 64-bit numbers, as 16 hex digits. It reads only
// those two pieces, so it's quick even on network storage.
func FileHash(path string) (string, error) {
	const chunk = 64 << 10
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()
	if size < chunk {
		return "", errors.New("too small to hash")
	}
	hash := uint64(size)
	buf := make([]byte, chunk)
	for _, off := range []int64{0, size - chunk} {
		if _, err := f.ReadAt(buf, off); err != nil {
			return "", err
		}
		for i := 0; i < chunk; i += 8 {
			hash += binary.LittleEndian.Uint64(buf[i:])
		}
	}
	return fmt.Sprintf("%016x", hash), nil
}

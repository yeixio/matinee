package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/subtitle"
	"github.com/yeixio/matinee/internal/version"
)

// Subtitle search: once an admin enters an OpenSubtitles API key under
// Settings → Add-ons, anyone watching (but not on a kids profile) can look
// for subtitles a video doesn't have, from the player's subtitles menu.
// What they pick is fetched once, kept in the database (found_subtitles),
// and listed with the video's own tracks for everyone from then on. Admins,
// and whoever added one, can remove it again.

const settingOpenSubtitles = "opensubtitles"

// searchCacheTTL keeps a title's search results for a while, so opening
// the menu again doesn't ask OpenSubtitles again.
const searchCacheTTL = 15 * time.Minute

// foundPrefix starts the track id of a subtitle found online.
const foundPrefix = "f"

type openSubsState struct {
	mu     sync.Mutex
	raw    string
	client *subtitle.OpenSubtitles
	cache  ttlCache
}

// openSubtitles returns the client for the admin's key, or nil when there's
// none. The client is kept while the key stays the same, so its sign-in is
// reused.
func (s *Server) openSubtitles(ctx context.Context) (*subtitle.OpenSubtitles, error) {
	v, err := s.Store.Setting(ctx, settingOpenSubtitles)
	if err != nil || v == "" {
		return nil, err
	}
	st := &s.openSubs
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.client != nil && st.raw == v {
		return st.client, nil
	}
	var k subtitle.OpenSubtitlesKey
	if err := json.Unmarshal([]byte(v), &k); err != nil || k.APIKey == "" {
		return nil, err
	}
	st.raw, st.client = v, s.newOpenSubtitles(k)
	st.cache.clear()
	return st.client, nil
}

func (s *Server) newOpenSubtitles(k subtitle.OpenSubtitlesKey) *subtitle.OpenSubtitles {
	return &subtitle.OpenSubtitles{Key: k, Base: s.OpenSubtitlesBase, UserAgent: "Matinee v" + strings.TrimPrefix(version.Version, "v")}
}

// ---- the OpenSubtitles key (admins) ----

// openSubtitlesView is GET /integrations/opensubtitles. The key and the
// password never leave the server; Hint is the key's last four characters.
type openSubtitlesView struct {
	Connected bool   `json:"connected"`
	Hint      string `json:"hint,omitempty"`
	Username  string `json:"username,omitempty"`
}

func (s *Server) getOpenSubtitles(w http.ResponseWriter, r *http.Request) {
	c, err := s.openSubtitles(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	v := openSubtitlesView{}
	if c != nil {
		v.Connected, v.Username = true, c.Key.Username
		if k := c.Key.APIKey; len(k) >= 8 {
			v.Hint = k[len(k)-4:]
		}
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) putOpenSubtitles(w http.ResponseWriter, r *http.Request) {
	var k subtitle.OpenSubtitlesKey
	if err := readJSON(r, &k); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	k.APIKey, k.Username = strings.TrimSpace(k.APIKey), strings.TrimSpace(k.Username)
	old, err := s.openSubtitles(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	if old != nil {
		// Changing only the account keeps the key, and the other way round,
		// so an admin doesn't have to type them in again.
		if k.APIKey == "" {
			k.APIKey = old.Key.APIKey
		}
		if k.Password == "" && k.Username == old.Key.Username {
			k.Password = old.Key.Password
		}
	}
	if k.APIKey == "" {
		writeError(w, http.StatusBadRequest, "enter the API key")
		return
	}
	if (k.Username == "") != (k.Password == "") {
		writeError(w, http.StatusBadRequest, "enter both the username and the password, or neither")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	switch err := s.newOpenSubtitles(k).Check(ctx); {
	case errors.Is(err, subtitle.ErrBadKey):
		writeError(w, http.StatusBadRequest, "OpenSubtitles didn't accept that API key")
		return
	case errors.Is(err, subtitle.ErrBadLogin):
		writeError(w, http.StatusBadRequest, "OpenSubtitles didn't accept that username and password")
		return
	case err != nil:
		s.Log.Warn("opensubtitles check", "err", err)
		writeError(w, http.StatusBadGateway, "OpenSubtitles couldn't be reached to check the key")
		return
	}
	b, _ := json.Marshal(k)
	if err := s.Store.SetSetting(r.Context(), settingOpenSubtitles, string(b)); err != nil {
		s.serverError(w, err)
		return
	}
	r.Body = http.NoBody
	s.getOpenSubtitles(w, r)
}

func (s *Server) deleteOpenSubtitles(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.SetSetting(r.Context(), settingOpenSubtitles, ""); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- searching ----

// canFindSubtitles says whether the person may look for subtitles for a
// video: there's a key, it's a video, and they're not on a kids profile.
func (s *Server) canFindSubtitles(ctx context.Context, u store.User, it store.Item) bool {
	if u.KidsLevel > 0 || !it.Playable || it.VideoCodec == "" {
		return false
	}
	c, err := s.openSubtitles(ctx)
	return err == nil && c != nil
}

// videoForSubtitles loads the video in the path and the client, answering
// for both when either is missing.
func (s *Server) videoForSubtitles(w http.ResponseWriter, r *http.Request) (store.Item, *subtitle.OpenSubtitles, bool) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return it, nil, false
	}
	if !it.Playable || it.VideoCodec == "" {
		writeError(w, http.StatusBadRequest, "not a video")
		return it, nil, false
	}
	c, err := s.openSubtitles(r.Context())
	if err != nil {
		s.serverError(w, err)
		return it, nil, false
	}
	if c == nil {
		writeError(w, http.StatusConflict, "subtitle search isn't set up on this server")
		return it, nil, false
	}
	return it, c, true
}

// searchLanguages are the languages to look in: those asked for
// (?languages=en,pt-BR), or else the person's own.
func (s *Server) searchLanguages(r *http.Request) []string {
	var out []string
	for _, l := range strings.Split(r.URL.Query().Get("languages"), ",") {
		if c := subtitle.APILanguage(l); c != "" && !slices.Contains(out, c) && len(out) < 5 {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		lang := s.requestLanguage(r, currentUser(r))
		if c := subtitle.APILanguage(lang); c != "" {
			out = append(out, c)
		}
		if !slices.Contains(out, "en") {
			out = append(out, "en")
		}
	}
	return out
}

// subtitleSearchResult is GET /items/{id}/subtitles/search.
type subtitleSearchResult struct {
	Languages []string         `json:"languages"`
	Results   []subtitle.Found `json:"results"`
	// Have lists the found files the video already has, so apps can mark them.
	Have []int64 `json:"have"`
}

// searchSubtitles finds subtitles for a video online.
func (s *Server) searchSubtitles(w http.ResponseWriter, r *http.Request) {
	it, c, ok := s.videoForSubtitles(w, r)
	if !ok {
		return
	}
	langs := s.searchLanguages(r)
	q := subtitle.Query{Languages: langs, TMDBID: deref64(it.TMDBID), Title: it.Title, Year: derefInt(it.Year)}
	if it.Kind == "episode" {
		q.TMDBID, q.Title, q.Year = 0, it.ShowTitle, 0
		q.Episode = derefInt(it.IndexNumber)
		for p := it.ParentID; p != nil; {
			parent, err := s.Store.Item(r.Context(), currentUser(r).ID, *p)
			if err != nil {
				break
			}
			switch parent.Kind {
			case "season":
				q.Season = derefInt(parent.IndexNumber)
			case "show":
				q.ShowTMDBID, q.Title = deref64(parent.TMDBID), parent.Title
			}
			p = parent.ParentID
		}
	}
	key := strings.Join(append([]string{it.Path}, langs...), "|")
	results, err := cached(&s.openSubs.cache, key, searchCacheTTL, func() ([]subtitle.Found, error) {
		if h, err := subtitle.FileHash(it.Path); err == nil {
			q.Hash = h
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		return c.Search(ctx, q)
	})
	if !s.subtitleSearchFailed(w, err) {
		return
	}
	if len(results) > 50 {
		results = results[:50]
	}
	out := subtitleSearchResult{Languages: langs, Results: results, Have: []int64{}}
	if out.Results == nil {
		out.Results = []subtitle.Found{}
	}
	found, err := s.Store.FoundSubtitles(r.Context(), it.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	for _, f := range found {
		if id, ok := openSubtitlesID(f.Source); ok {
			out.Have = append(out.Have, id)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// subtitleSearchFailed answers for an error from OpenSubtitles, and says
// whether there was none.
func (s *Server) subtitleSearchFailed(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, subtitle.ErrBadKey), errors.Is(err, subtitle.ErrBadLogin):
		writeError(w, http.StatusBadGateway, "OpenSubtitles turned down the server's key. An admin can check it in Settings.")
	case errors.Is(err, subtitle.ErrQuota):
		writeError(w, http.StatusTooManyRequests, "today's subtitle downloads are used up. Try again tomorrow.")
	default:
		s.Log.Warn("opensubtitles", "err", err)
		writeError(w, http.StatusBadGateway, "OpenSubtitles couldn't be reached right now")
	}
	return false
}

func openSubtitlesID(source string) (int64, bool) {
	rest, ok := strings.CutPrefix(source, "opensubtitles:")
	if !ok {
		return 0, false
	}
	var id int64
	err := json.Unmarshal([]byte(rest), &id)
	return id, err == nil
}

// addFoundSubtitle fetches a file the search found and adds it to the video.
// The body is the result as the search gave it.
func (s *Server) addFoundSubtitle(w http.ResponseWriter, r *http.Request) {
	it, c, ok := s.videoForSubtitles(w, r)
	if !ok {
		return
	}
	var req subtitle.Found
	if err := readJSON(r, &req); err != nil || req.ID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	source := "opensubtitles:" + jsonInt(req.ID)
	f := store.FoundSubtitle{
		ItemID: it.ID, Source: source, Format: "srt", AddedBy: currentUser(r).ID,
		SDH: req.SDH, Forced: req.Forced, Release: truncate(strings.TrimSpace(req.Release), 200),
	}
	lang := req.Variant
	if lang == "" {
		lang = req.Language
	}
	if code, _, name := subtitle.APILanguageInfo(lang); name != "" {
		f.Language, f.Label = code, name
	}
	have, err := s.Store.FoundSubtitles(r.Context(), it.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if i := slices.IndexFunc(have, func(h store.FoundSubtitle) bool { return h.Source == source }); i < 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		data, remaining, err := c.Download(ctx, req.ID)
		if !s.subtitleSearchFailed(w, err) {
			return
		}
		s.Log.Info("subtitles found", "item", it.ID, "file", req.ID, "downloadsLeft", remaining)
		f.Data = data
	}
	f, err = s.Store.AddFoundSubtitle(r.Context(), f)
	if err != nil {
		s.serverError(w, err)
		return
	}
	t := s.foundTrack(currentUser(r), f)
	writeJSON(w, http.StatusCreated, t)
}

// removeFoundSubtitle takes a found subtitle off a video, for an admin or
// whoever added it.
func (s *Server) removeFoundSubtitle(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	id, ok := strings.CutPrefix(r.PathValue("track"), foundPrefix)
	if !ok {
		writeError(w, http.StatusBadRequest, "only subtitles found online can be removed")
		return
	}
	f, err := s.Store.FoundSubtitle(r.Context(), it.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such subtitle track")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	if u := currentUser(r); !u.IsAdmin && (f.AddedBy == 0 || f.AddedBy != u.ID) {
		writeError(w, http.StatusForbidden, "only an admin or whoever added them can remove these subtitles")
		return
	}
	if err := s.Store.RemoveFoundSubtitle(r.Context(), it.ID, id); err != nil {
		s.serverError(w, err)
		return
	}
	os.Remove(s.foundPath(id))
	w.WriteHeader(http.StatusNoContent)
}

// foundTracks lists the subtitles found online for a video as tracks.
func (s *Server) foundTracks(ctx context.Context, u store.User, it store.Item) []subtitle.Track {
	found, err := s.Store.FoundSubtitles(ctx, it.ID)
	if err != nil {
		s.Log.Warn("found subtitles", "item", it.ID, "err", err)
		return nil
	}
	out := make([]subtitle.Track, 0, len(found))
	for _, f := range found {
		out = append(out, s.foundTrack(u, f))
	}
	return out
}

func (s *Server) foundTrack(u store.User, f store.FoundSubtitle) subtitle.Track {
	t := subtitle.Track{
		ID: foundPrefix + f.ID, Language: f.Language, Codec: "subrip", External: true, Found: true,
		Forced: f.Forced, SDH: f.SDH, Path: s.foundPath(f.ID),
		Removable: u.IsAdmin || (f.AddedBy != 0 && f.AddedBy == u.ID),
	}
	if f.Format == "vtt" {
		t.Codec = "webvtt"
	}
	t.Label, t.Note = subtitle.FoundLabel(f.Label, f.Forced, f.SDH)
	return t
}

// foundPath is where a found subtitle's file is put for the converter,
// which reads files. The database keeps the file; this is a copy.
func (s *Server) foundPath(id string) string {
	return filepath.Join(s.Subtitles.Dir, "found", id+".srt")
}

// ensureFoundFile writes a found subtitle's file for the converter, from the
// database, when it isn't there yet (a new pod, or a cleared cache).
func (s *Server) ensureFoundFile(ctx context.Context, it store.Item, t subtitle.Track) error {
	if _, err := os.Stat(t.Path); err == nil {
		return nil
	}
	f, err := s.Store.FoundSubtitle(ctx, it.ID, strings.TrimPrefix(t.ID, foundPrefix))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(t.Path), 0o755); err != nil {
		return err
	}
	tmp := t.Path + ".tmp"
	if err := os.WriteFile(tmp, f.Data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, t.Path)
}

func deref64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

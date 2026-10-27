package api

import (
	"net/http"
	"sync"

	"github.com/yeixio/matinee/i18n"
	"github.com/yeixio/matinee/internal/store"
)

// Each profile can pick the language Matinee shows it in, which follows it to
// every device; until it does, each device uses its own (the browser's or
// the phone's), sent as Accept-Language. Titles and descriptions from the
// online match come in that language where TMDB has them (see
// store/translations.go). The text of the web player and the apps comes from
// the catalog in i18n/.

// languages remembers profiles' picked languages, so requests don't each
// read them, and the languages devices asked for that were noted already.
type languages struct {
	mu     sync.Mutex
	picked map[int64]string
	seen   map[string]bool
}

func (l *languages) get(id int64) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.picked[id]
	return v, ok
}

func (l *languages) set(id int64, lang string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.picked == nil {
		l.picked = map[int64]string{}
	}
	l.picked[id] = lang
}

// firstSight reports whether a language is new to this run of the server.
func (l *languages) firstSight(lang string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen[lang] {
		return false
	}
	if l.seen == nil {
		l.seen = map[string]bool{}
	}
	l.seen[lang] = true
	return true
}

// requestLanguage is the language a request gets titles in: the profile's
// pick, else the device's, else "" for the original text.
func (s *Server) requestLanguage(r *http.Request, u store.User) string {
	lang, ok := s.langs.get(u.ID)
	if !ok {
		var err error
		if lang, err = s.Store.UserLanguage(r.Context(), u.ID); err != nil {
			s.Log.Warn("read profile language", "err", err)
			return ""
		}
		s.langs.set(u.ID, lang)
	}
	if lang != "" {
		return lang
	}
	lang = i18n.FromAcceptLanguage(r.Header.Get("Accept-Language"))
	if lang != "" && lang != i18n.Source && s.langs.firstSight(lang) {
		// Seasons and episodes are translated in the languages people use.
		if isNew, err := s.Store.NoteLanguageSeen(r.Context(), lang); err != nil {
			s.Log.Warn("note language", "err", err)
		} else if isNew {
			s.languagesChanged()
		}
	}
	return lang
}

// languagesChanged looks up translations for a language people started using.
func (s *Server) languagesChanged() {
	if s.Translate != nil {
		go s.Translate(s.BackgroundCtx)
	}
}

type myLanguage struct {
	// Language is the profile's own pick, such as de or pt-BR; "" follows
	// each device's language.
	Language string `json:"language"`
}

func (s *Server) getMyLanguage(w http.ResponseWriter, r *http.Request) {
	lang, err := s.Store.UserLanguage(r.Context(), currentUser(r).ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if !i18n.Has(lang) {
		lang = ""
	}
	writeJSON(w, http.StatusOK, myLanguage{Language: lang})
}

// putMyLanguage saves a profile's language. On the home network without
// signing in that's the shared Home account, so everyone at home shares it.
func (s *Server) putMyLanguage(w http.ResponseWriter, r *http.Request) {
	var in myLanguage
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if in.Language != "" && !i18n.Has(in.Language) {
		writeError(w, http.StatusBadRequest, "no such language")
		return
	}
	id := currentUser(r).ID
	if err := s.Store.SetUserLanguage(r.Context(), id, in.Language); err != nil {
		s.serverError(w, err)
		return
	}
	s.langs.set(id, in.Language)
	if in.Language != "" && in.Language != i18n.Source {
		s.languagesChanged()
	}
	writeJSON(w, http.StatusOK, in)
}

// listLanguages lists the languages a profile can pick, for clients that
// don't carry the list themselves.
func (s *Server) listLanguages(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, i18n.Languages())
}

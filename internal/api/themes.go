package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"

	"github.com/yeixio/matinee/internal/theme"
)

// Themes: the built-in ones and the files an admin put in ThemeDir, the
// server's default (an admin picks it), and each person's own choice, which
// follows them to every device. See internal/theme and docs/themes.md.

// settingTheme is the theme people get until they pick their own.
const settingTheme = "theme"

// userThemeKey keeps a person's own theme in the settings table.
func userThemeKey(userID int64) string { return "theme:user:" + strconv.FormatInt(userID, 10) }

type themeList struct {
	// Default is the server's theme: what someone who hasn't picked one sees.
	Default string        `json:"default"`
	Themes  []theme.Theme `json:"themes"`
}

// themes reads the theme folder each time, so a theme dropped in shows up
// without a restart.
func (s *Server) themes() ([]theme.Theme, []error) {
	return theme.Load(s.ThemeDir)
}

func hasTheme(themes []theme.Theme, id string) bool {
	return slices.ContainsFunc(themes, func(t theme.Theme) bool { return t.ID == id })
}

// defaultTheme is the admin's pick, or the built-in default when there's
// none or its file was removed.
func (s *Server) defaultTheme(ctx context.Context, themes []theme.Theme) (string, error) {
	id, err := s.Store.Setting(ctx, settingTheme)
	if err != nil || !hasTheme(themes, id) {
		return theme.Default, err
	}
	return id, nil
}

// listThemes is public, so the sign-in page can wear the server's theme.
func (s *Server) listThemes(w http.ResponseWriter, r *http.Request) {
	themes, _ := s.themes()
	def, err := s.defaultTheme(r.Context(), themes)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, themeList{Default: def, Themes: themes})
}

type myTheme struct {
	// Theme is the person's own pick; "" follows the server's default.
	Theme string `json:"theme"`
}

func (s *Server) getMyTheme(w http.ResponseWriter, r *http.Request) {
	id, err := s.Store.Setting(r.Context(), userThemeKey(currentUser(r).ID))
	if err != nil {
		s.serverError(w, err)
		return
	}
	if themes, _ := s.themes(); !hasTheme(themes, id) {
		id = ""
	}
	writeJSON(w, http.StatusOK, myTheme{Theme: id})
}

// putMyTheme saves a person's theme. On the home network without signing in
// that's the shared Home account, so everyone at home shares it.
func (s *Server) putMyTheme(w http.ResponseWriter, r *http.Request) {
	var in myTheme
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if in.Theme != "" {
		if themes, _ := s.themes(); !hasTheme(themes, in.Theme) {
			writeError(w, http.StatusBadRequest, "no such theme")
			return
		}
	}
	if err := s.Store.SetSetting(r.Context(), userThemeKey(currentUser(r).ID), in.Theme); err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, in)
}

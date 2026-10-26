// Package theme holds Matinee's colour themes: the ones built in, and ones
// an admin adds by putting a theme file in the data directory's themes folder.
//
// A theme is a small JSON file of colours and font names, never code, so it
// is safe to share. The server sends every client the full set of colours;
// the colours a file leaves out are worked out from the ones it gives (see
// Resolve). docs/themes.md describes the format for people making themes.
package theme

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Default is the theme people see until they or an admin pick another.
const Default = "matinee"

//go:embed builtin/*.json
var builtinFS embed.FS

// Theme is a theme file, and with Resolve, what the clients are sent.
type Theme struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	// Scheme is "dark" or "light": what the system's own controls (scroll
	// bars, keyboards, alerts) should match.
	Scheme string `json:"scheme"`
	Fonts  Fonts  `json:"fonts"`
	Colors Colors `json:"colors"`
	// BuiltIn is set by the server for the themes that ship with Matinee.
	BuiltIn bool `json:"builtIn"`
}

// Fonts name typefaces from the set every client ships (DisplayFonts and
// BodyFonts), since a TV can't load fonts from a theme.
type Fonts struct {
	Display string `json:"display"` // titles and headings
	Body    string `json:"body"`    // everything else
}

// DisplayFonts and BodyFonts are the typefaces bundled with the web player
// and the apps. Adding one means adding it to every client.
var (
	DisplayFonts = []string{"fraunces", "inter", "orbitron"}
	BodyFonts    = []string{"inter"}
)

// Colors are CSS colours: #rgb, #rrggbb, #rrggbbaa or rgba(r,g,b,a).
type Colors struct {
	// Required.
	Ink     string `json:"ink"`     // the page behind everything
	Surface string `json:"surface"` // panels and bars
	Raised  string `json:"raised"`  // cards, inputs and buttons on a panel
	Line    string `json:"line"`    // borders and dividers
	Text    string `json:"text"`    // body text and titles
	Muted   string `json:"muted"`   // secondary text
	Faint   string `json:"faint"`   // hints and the least important text
	Accent  string `json:"accent"`  // the main button, links, what has focus

	// Optional; Resolve works them out from the ones above.
	AccentStrong string `json:"accentStrong"` // the accent when pressed or hovered
	OnAccent     string `json:"onAccent"`     // text on an accent button
	Danger       string `json:"danger"`       // errors and deleting
	Glow         string `json:"glow"`         // the halo around what has focus on a TV
	Glass        string `json:"glass"`        // frosted buttons over artwork
	GlassFocused string `json:"glassFocused"` // a frosted button with focus
	Scrim        string `json:"scrim"`        // behind the video player
	Track        string `json:"track"`        // a progress bar's unplayed part
	Shade        string `json:"shade"`        // dark wash under text on artwork
	Veil         string `json:"veil"`         // tints a backdrop towards the page
}

// Builtin returns the themes that ship with Matinee, Default first.
func Builtin() []Theme {
	entries, _ := fs.ReadDir(builtinFS, "builtin")
	var out []Theme
	for _, e := range entries {
		b, _ := builtinFS.ReadFile("builtin/" + e.Name())
		t, err := Parse(b)
		if err != nil {
			panic(fmt.Sprintf("built-in theme %s: %v", e.Name(), err))
		}
		t.BuiltIn = true
		out = append(out, t)
	}
	sortThemes(out)
	return out
}

// builtinOrder is how the built-in themes are listed; others go by name.
var builtinOrder = []string{Default, "dark", "light", "clean", "sci-fi", "drive-in", "high-contrast"}

func sortThemes(ts []Theme) {
	rank := func(t Theme) int {
		if i := slices.Index(builtinOrder, t.ID); i >= 0 {
			return i
		}
		return len(builtinOrder)
	}
	sort.SliceStable(ts, func(i, j int) bool {
		if ri, rj := rank(ts[i]), rank(ts[j]); ri != rj {
			return ri < rj
		}
		return strings.ToLower(ts[i].Name) < strings.ToLower(ts[j].Name)
	})
}

// Load returns the built-in themes and the ones in dir, which may not exist.
// A file that isn't a valid theme is skipped and reported in problems, so one
// bad file doesn't hide the rest. A file can't replace a built-in theme.
func Load(dir string) (themes []Theme, problems []error) {
	themes = Builtin()
	if dir == "" {
		return themes, nil
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return themes, []error{err}
	}
	seen := map[string]bool{}
	for _, t := range themes {
		seen[t.ID] = true
	}
	var custom []Theme
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		t, err := Parse(b)
		if err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", filepath.Base(f), err))
			continue
		}
		if seen[t.ID] {
			problems = append(problems, fmt.Errorf("%s: another theme already has the id %q", filepath.Base(f), t.ID))
			continue
		}
		seen[t.ID] = true
		custom = append(custom, t)
	}
	sortThemes(custom)
	return append(themes, custom...), problems
}

// Parse reads a theme file, checks it, and fills in the optional colours.
func Parse(b []byte) (Theme, error) {
	var t Theme
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return t, fmt.Errorf("not a theme file: %w", err)
	}
	t.BuiltIn = false
	if err := t.Validate(); err != nil {
		return t, err
	}
	return t.Resolve(), nil
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// Validate says what's wrong with a theme, all at once.
func (t Theme) Validate() error {
	var errs []error
	if !idPattern.MatchString(t.ID) {
		errs = append(errs, errors.New("id must be 1-40 lowercase letters, digits and dashes"))
	}
	if strings.TrimSpace(t.Name) == "" || len(t.Name) > 40 {
		errs = append(errs, errors.New("name must be 1-40 characters"))
	}
	if t.Scheme != "dark" && t.Scheme != "light" {
		errs = append(errs, errors.New(`scheme must be "dark" or "light"`))
	}
	if t.Fonts.Display != "" && !slices.Contains(DisplayFonts, t.Fonts.Display) {
		errs = append(errs, fmt.Errorf("fonts.display must be one of %s", strings.Join(DisplayFonts, ", ")))
	}
	if t.Fonts.Body != "" && !slices.Contains(BodyFonts, t.Fonts.Body) {
		errs = append(errs, fmt.Errorf("fonts.body must be one of %s", strings.Join(BodyFonts, ", ")))
	}
	for _, c := range t.Colors.fields() {
		if c.value == "" {
			if c.required {
				errs = append(errs, fmt.Errorf("colors.%s is required", c.name))
			}
			continue
		}
		if _, err := ParseColor(c.value); err != nil {
			errs = append(errs, fmt.Errorf("colors.%s: %w", c.name, err))
		}
	}
	return errors.Join(errs...)
}

type colorField struct {
	name     string
	value    string
	required bool
	ptr      *string
}

func (c *Colors) fields() []colorField {
	return []colorField{
		{"ink", c.Ink, true, &c.Ink},
		{"surface", c.Surface, true, &c.Surface},
		{"raised", c.Raised, true, &c.Raised},
		{"line", c.Line, true, &c.Line},
		{"text", c.Text, true, &c.Text},
		{"muted", c.Muted, true, &c.Muted},
		{"faint", c.Faint, true, &c.Faint},
		{"accent", c.Accent, true, &c.Accent},
		{"accentStrong", c.AccentStrong, false, &c.AccentStrong},
		{"onAccent", c.OnAccent, false, &c.OnAccent},
		{"danger", c.Danger, false, &c.Danger},
		{"glow", c.Glow, false, &c.Glow},
		{"glass", c.Glass, false, &c.Glass},
		{"glassFocused", c.GlassFocused, false, &c.GlassFocused},
		{"scrim", c.Scrim, false, &c.Scrim},
		{"track", c.Track, false, &c.Track},
		{"shade", c.Shade, false, &c.Shade},
		{"veil", c.Veil, false, &c.Veil},
	}
}

// Resolve fills in the optional colours and fonts of a valid theme, and
// writes every colour the same way, as #rrggbb or rgba(r,g,b,a), so clients
// only have to read two forms.
func (t Theme) Resolve() Theme {
	c := &t.Colors
	light := t.Scheme == "light"
	text, _ := ParseColor(c.Text)
	ink, _ := ParseColor(c.Ink)
	accent, _ := ParseColor(c.Accent)
	def := func(p *string, v string) {
		if *p == "" {
			*p = v
		}
	}
	def(&c.AccentStrong, c.Accent)
	if c.OnAccent == "" {
		// Whichever of the page and the text reads better on the accent.
		if Contrast(accent, ink) >= Contrast(accent, text) {
			c.OnAccent = c.Ink
		} else {
			c.OnAccent = c.Text
		}
	}
	if light {
		def(&c.Danger, "#b42318")
		def(&c.Glass, text.Alpha(0.08).String())
		def(&c.GlassFocused, text.Alpha(0.16).String())
		def(&c.Track, text.Alpha(0.2).String())
	} else {
		def(&c.Danger, "#f08a80")
		def(&c.Glass, text.Alpha(0.14).String())
		def(&c.GlassFocused, text.Alpha(0.3).String())
		def(&c.Track, text.Alpha(0.25).String())
	}
	def(&c.Glow, accent.Alpha(0.45).String())
	def(&c.Scrim, "#000000")
	def(&c.Shade, "rgba(0,0,0,0.55)")
	def(&c.Veil, ink.Alpha(0.35).String())
	for _, f := range c.fields() {
		v, _ := ParseColor(*f.ptr)
		*f.ptr = v.String()
	}
	def(&t.Fonts.Display, "fraunces")
	def(&t.Fonts.Body, "inter")
	return t
}

// Color is an sRGB colour with alpha from 0 to 1.
type Color struct {
	R, G, B uint8
	A       float64
}

var rgbaPattern = regexp.MustCompile(`^rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*(?:,\s*([01]|0?\.\d+|1\.0+)\s*)?\)$`)

// ParseColor reads #rgb, #rrggbb, #rrggbbaa, rgb(r,g,b) or rgba(r,g,b,a).
func ParseColor(s string) (Color, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	bad := fmt.Errorf("%q isn't a colour (use #rrggbb or rgba(r,g,b,a))", s)
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) != 6 && len(h) != 8 {
			return Color{}, bad
		}
		n, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return Color{}, bad
		}
		if len(h) == 6 {
			return Color{uint8(n >> 16), uint8(n >> 8), uint8(n), 1}, nil
		}
		return Color{uint8(n >> 24), uint8(n >> 16), uint8(n >> 8), math.Round(float64(uint8(n))/255*100) / 100}, nil
	}
	m := rgbaPattern.FindStringSubmatch(s)
	if m == nil {
		return Color{}, bad
	}
	var ch [3]uint8
	for i := range ch {
		v, _ := strconv.Atoi(m[i+1])
		if v > 255 {
			return Color{}, bad
		}
		ch[i] = uint8(v)
	}
	a := 1.0
	if m[4] != "" {
		a, _ = strconv.ParseFloat(m[4], 64)
	}
	return Color{ch[0], ch[1], ch[2], a}, nil
}

// Alpha is the colour with another opacity.
func (c Color) Alpha(a float64) Color { c.A = a; return c }

func (c Color) String() string {
	if c.A >= 1 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%s)", c.R, c.G, c.B, strconv.FormatFloat(c.A, 'f', -1, 64))
}

// luminance is WCAG's relative luminance of an opaque colour.
func (c Color) luminance() float64 {
	lin := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// Contrast is the WCAG contrast ratio of two opaque colours, from 1 to 21.
func Contrast(a, b Color) float64 {
	la, lb := a.luminance(), b.luminance()
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Check lists what's hard to read in a theme: pairs of colours below WCAG
// AA contrast (4.5:1 for text, 3:1 for hints). Themes with warnings still
// load; the built-in ones have none.
func (t Theme) Check() []string {
	c := func(s string) Color { v, _ := ParseColor(s); return v }
	pairs := []struct {
		fg, bg string
		min    float64
		what   string
	}{
		{t.Colors.Text, t.Colors.Ink, 4.5, "text on ink"},
		{t.Colors.Text, t.Colors.Raised, 4.5, "text on raised"},
		{t.Colors.Muted, t.Colors.Ink, 4.5, "muted on ink"},
		{t.Colors.Muted, t.Colors.Surface, 4.5, "muted on surface"},
		{t.Colors.Faint, t.Colors.Ink, 3, "faint on ink"},
		{t.Colors.OnAccent, t.Colors.Accent, 4.5, "onAccent on accent"},
		{t.Colors.Accent, t.Colors.Ink, 3, "accent on ink"},
	}
	var out []string
	for _, p := range pairs {
		if r := Contrast(c(p.fg), c(p.bg)); r < p.min {
			out = append(out, fmt.Sprintf("%s is %.1f:1, below %.1f:1", p.what, r, p.min))
		}
	}
	return out
}

package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinThemesAreReadable(t *testing.T) {
	themes := Builtin()
	if len(themes) < 7 || themes[0].ID != Default {
		t.Fatalf("want %s first of at least 7 themes, got %d starting with %s", Default, len(themes), themes[0].ID)
	}
	for _, th := range themes {
		if !th.BuiltIn {
			t.Errorf("%s isn't marked built in", th.ID)
		}
		for _, w := range th.Check() {
			t.Errorf("%s: %s", th.ID, w)
		}
		for _, f := range th.Colors.fields() {
			if f.value == "" {
				t.Errorf("%s: %s wasn't filled in", th.ID, f.name)
			}
		}
	}
}

func TestResolveFillsInTheRest(t *testing.T) {
	th, err := Parse([]byte(`{
		"id": "mint", "name": "Mint", "scheme": "dark",
		"colors": {"ink": "#0b1210", "surface": "#101a17", "raised": "#18241f", "line": "#25352f",
			"text": "#eafff6", "muted": "#9cc4b4", "faint": "#6c8e81", "accent": "#3ddc97"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	c := th.Colors
	if c.AccentStrong != "#3ddc97" || c.OnAccent != "#0b1210" || c.Glow != "rgba(61,220,151,0.45)" ||
		c.Glass != "rgba(234,255,246,0.14)" || c.Veil != "rgba(11,18,16,0.35)" || c.Scrim != "#000000" {
		t.Errorf("derived colours: %+v", c)
	}
	if th.Fonts != (Fonts{"fraunces", "inter"}) {
		t.Errorf("fonts: %+v", th.Fonts)
	}
}

func TestParseColor(t *testing.T) {
	for in, want := range map[string]string{
		"#FFF":                   "#ffffff",
		"#ff6a55":                "#ff6a55",
		"#ff6a5580":              "rgba(255,106,85,0.5)",
		"rgb(1, 2, 3)":           "#010203",
		"rgba(255,106,85,0.45)":  "rgba(255,106,85,0.45)",
		"rgba(255, 106, 85, .3)": "rgba(255,106,85,0.3)",
	} {
		c, err := ParseColor(in)
		if err != nil || c.String() != want {
			t.Errorf("%s: got %s, %v; want %s", in, c, err, want)
		}
	}
	for _, in := range []string{"red", "#12", "rgba(300,0,0,1)", "url(x)", "#ff6a55; background: red"} {
		if _, err := ParseColor(in); err == nil {
			t.Errorf("%s was accepted", in)
		}
	}
}

func TestLoadSkipsBadFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
	}
	good := `{"id": "%s", "name": "Mine", "scheme": "light", "colors": {"ink": "#fff", "surface": "#eee", "raised": "#ddd",
		"line": "#ccc", "text": "#111", "muted": "#444", "faint": "#777", "accent": "#0a7"}}`
	write("mine.json", strings.Replace(good, "%s", "mine", 1))
	write("clash.json", strings.Replace(good, "%s", "matinee", 1))
	write("broken.json", `{"id": "broken"`)
	write("script.json", `{"id": "x", "name": "X", "scheme": "dark", "css": "body{}"}`)
	write("notes.txt", "not a theme")

	themes, problems := Load(dir)
	if len(problems) != 3 {
		t.Errorf("want 3 problems, got %v", problems)
	}
	last := themes[len(themes)-1]
	if last.ID != "mine" || last.BuiltIn || last.Colors.OnAccent != "#111111" {
		t.Errorf("custom theme: %+v", last)
	}
	if themes, problems := Load(filepath.Join(dir, "missing")); len(problems) != 0 || len(themes) != len(Builtin()) {
		t.Errorf("a missing folder: %d themes, %v", len(themes), problems)
	}
}

func TestValidateSaysEverythingAtOnce(t *testing.T) {
	err := Theme{ID: "Bad ID", Scheme: "dim", Fonts: Fonts{Display: "comic-sans"}, Colors: Colors{Ink: "black"}}.Validate()
	for _, want := range []string{"id must", "name must", "scheme must", "fonts.display", "colors.ink", "colors.accent is required"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

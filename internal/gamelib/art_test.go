package gamelib

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yeixio/matinee/internal/gamedat"
)

func TestPickArt(t *testing.T) {
	have := &artNames{exact: map[string]bool{}, byTitle: map[string][]string{}}
	for _, n := range []string{
		"Legend of Zelda, The - A Link to the Past (USA)",
		"Legend of Zelda, The - A Link to the Past (Europe)",
		"Zelda no Densetsu - Kamigami no Triforce (Japan)",
		"Super Mario World (Europe)",
		"Super Mario World (Japan)",
		"Super Mario World (USA) (Beta)",
		"Tom & Jerry (USA)",
		"Final Fantasy VII (USA) (Disc 1)",
	} {
		have.exact[n] = true
		have.byTitle[looseTitle(n)] = append(have.byTitle[looseTitle(n)], n)
	}
	for name, want := range map[string]string{
		"Legend of Zelda, The - A Link to the Past (Europe)": "Legend of Zelda, The - A Link to the Past (Europe)",
		"The Legend of Zelda - A Link to the Past":           "Legend of Zelda, The - A Link to the Past (USA)",
		"Legend of Zelda, The - A Link to the Past (Canada)": "Legend of Zelda, The - A Link to the Past (USA)",
		"Super Mario World (USA)":                            "Super Mario World (Europe)",
		"super mario world":                                  "Super Mario World (Europe)",
		"Tom and Jerry":                                      "Tom & Jerry (USA)",
		"Final Fantasy VII (USA) (Disc 1)":                   "Final Fantasy VII (USA) (Disc 1)",
		"Final Fantasy VII (USA)":                            "Final Fantasy VII (USA) (Disc 1)",
		"Some Other Game (USA)":                              "",
	} {
		if got := pickArt(have, name); got != want {
			t.Errorf("pickArt(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestOnlineArt(t *testing.T) {
	var lists atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Nintendo - Game Boy/Named_Boxarts/":
			lists.Add(1)
			w.Write([]byte(`<html><body><h1>Index of /Nintendo - Game Boy/Named_Boxarts</h1>
<a href="../">../</a>
<a href="Tetris%20%28World%29%20%28Rev%201%29.png">Tetris (World) (Rev 1).png</a>
<a href="Kirby%27s%20Dream%20Land%20%28USA%2C%20Europe%29.png">Kirby's Dream Land (USA, Europe).png</a>
</body></html>`))
		case "/Nintendo - Game Boy/Named_Titles/":
			lists.Add(1)
			w.Write([]byte(`<a href="Wario%20Land%20-%20Super%20Mario%20Land%203%20%28World%29.png">x</a>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	k := &Kind{Dir: t.TempDir(), Art: srv.URL}
	gb, _ := gamedat.SystemByFolder("gb")
	ctx := context.Background()
	base := srv.URL + "/Nintendo%20-%20Game%20Boy/"
	for name, want := range map[string]string{
		"Tetris (World) (Rev 1)":          base + "Named_Boxarts/Tetris%20%28World%29%20%28Rev%201%29.png",
		"Kirby's Dream Land (Japan)":      base + "Named_Boxarts/Kirby%27s%20Dream%20Land%20%28USA%2C%20Europe%29.png",
		"Wario Land - Super Mario Land 3": base + "Named_Titles/Wario%20Land%20-%20Super%20Mario%20Land%203%20%28World%29.png",
		"Nothing Like It (USA)":           "",
	} {
		if got := k.onlineArt(ctx, gb, name); got != want {
			t.Errorf("onlineArt(%q) = %q, want %q", name, got, want)
		}
	}
	// The lists are kept: a new Kind reads them from its folder.
	n := lists.Load()
	k2 := &Kind{Dir: k.Dir, Art: srv.URL}
	if got := k2.onlineArt(ctx, gb, "Tetris (World) (Rev 1)"); !strings.HasSuffix(got, "Tetris%20%28World%29%20%28Rev%201%29.png") {
		t.Fatalf("from the kept list: %q", got)
	}
	if lists.Load() != n {
		t.Fatal("lists downloaded again")
	}
}

func TestLocalArt(t *testing.T) {
	dir := t.TempDir()
	game := filepath.Join(dir, "Tetris (World).gb")
	write := func(p string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	write(game)
	if p := localArt(game, "Tetris (World)"); p != "" {
		t.Fatalf("found %q with no art", p)
	}
	write(filepath.Join(dir, "media", "box2dfront", "Tetris (World).PNG"))
	if p := localArt(game, "Tetris (World)"); p != filepath.Join(dir, "media", "box2dfront", "Tetris (World).PNG") {
		t.Fatalf("scraper folder: %q", p)
	}
	write(filepath.Join(dir, "images", "Tetris (World)-image.jpg"))
	if p := localArt(game, "Tetris (World)"); p != filepath.Join(dir, "images", "Tetris (World)-image.jpg") {
		t.Fatalf("images folder: %q", p)
	}
	write(filepath.Join(dir, "Tetris (World).jpg"))
	if p := localArt(game, "Tetris (World)"); p != filepath.Join(dir, "Tetris (World).jpg") {
		t.Fatalf("beside the game: %q", p)
	}
}

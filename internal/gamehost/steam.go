// Package gamehost plays PC games on a computer with Steam and streams them
// to Matinee: the game host (cmd/matinee-gamehost) runs beside Steam, in a
// container or on a gaming PC, lists the games installed there, launches
// one, captures its picture and sound, encodes them on that computer's GPU
// and sends them to Matinee in the direct stream's messages
// (internal/gamestream/direct.go), taking the player's buttons back as
// virtual gamepads. Matinee passes the stream on to the player as it does
// an emulated game's.
package gamehost

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Game is a game installed in Steam.
type Game struct {
	AppID int    `json:"appId"`
	Name  string `json:"name"`
	// Size is how much room it takes on disk, in bytes.
	Size int64 `json:"size,omitempty"`
}

// SteamRoot finds Steam's own folder under home: where its
// steamapps/libraryfolders.vdf is.
func SteamRoot(home string) (string, error) {
	for _, p := range []string{".steam/steam", ".local/share/Steam", ".steam/root", ".var/app/com.valvesoftware.Steam/.local/share/Steam"} {
		dir := filepath.Join(home, p)
		if _, err := os.Stat(filepath.Join(dir, "steamapps")); err == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("no Steam folder in %s", home)
}

// Installed lists the games installed in every Steam library folder, by
// name. Steam's own tools (Proton, its runtimes) are left out.
func Installed(steamRoot string) ([]Game, error) {
	libs := []string{filepath.Join(steamRoot, "steamapps")}
	if f, err := os.Open(filepath.Join(steamRoot, "steamapps", "libraryfolders.vdf")); err == nil {
		kv, err := ParseVDF(f)
		f.Close()
		if err == nil {
			for _, lib := range kv.Child("libraryfolders").Children() {
				if p := lib.Value.Get("path"); p != "" {
					libs = append(libs, filepath.Join(p, "steamapps"))
				}
			}
		}
	}
	seen := map[int]bool{}
	var games []Game
	var firstErr error
	for _, lib := range libs {
		manifests, err := filepath.Glob(filepath.Join(lib, "appmanifest_*.acf"))
		if err != nil {
			continue
		}
		for _, m := range manifests {
			g, ok, err := readManifest(m)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if ok && !seen[g.AppID] {
				seen[g.AppID] = true
				games = append(games, g)
			}
		}
	}
	slices.SortFunc(games, func(a, b Game) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	if len(games) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return games, nil
}

// stateInstalled is the bit of an app manifest's StateFlags that says the
// app is fully installed.
const stateInstalled = 4

// tools are Steam's own apps that aren't games: runtimes and
// redistributables Steam installs beside them.
var tools = map[int]bool{
	228980:  true, // Steamworks Common Redistributables
	1070560: true, // Steam Linux Runtime 1.0 (scout)
	1391110: true, // Steam Linux Runtime 2.0 (soldier)
	1628350: true, // Steam Linux Runtime 3.0 (sniper)
	1493710: true, // Proton Experimental
	2180100: true, // Proton Hotfix
	1826330: true, // Proton EasyAntiCheat Runtime
	1161040: true, // Proton BattlEye Runtime
}

func readManifest(path string) (Game, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return Game{}, false, err
	}
	defer f.Close()
	kv, err := ParseVDF(f)
	if err != nil {
		return Game{}, false, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	app := kv.Child("AppState")
	id, err := strconv.Atoi(app.Get("appid"))
	if err != nil || id <= 0 {
		return Game{}, false, fmt.Errorf("%s: no app id", filepath.Base(path))
	}
	name := app.Get("name")
	flags, _ := strconv.Atoi(app.Get("StateFlags"))
	if flags&stateInstalled == 0 || tools[id] || name == "" || strings.HasPrefix(name, "Proton ") || strings.HasPrefix(name, "Steam Linux Runtime") {
		return Game{}, false, nil
	}
	size, _ := strconv.ParseInt(app.Get("SizeOnDisk"), 10, 64)
	return Game{AppID: id, Name: name, Size: size}, true, nil
}

// KV is one level of Valve's KeyValues text format (.vdf, .acf): keys,
// each with a string or a nested KV. Keys are matched without regard to
// case, as Steam does.
type KV struct {
	Pairs []Pair
}

// Pair is one key and its value: Value for a nested block, Str otherwise.
type Pair struct {
	Key   string
	Str   string
	Value *KV
}

// Get is a key's string, or "".
func (kv *KV) Get(key string) string {
	if kv == nil {
		return ""
	}
	for _, p := range kv.Pairs {
		if p.Value == nil && strings.EqualFold(p.Key, key) {
			return p.Str
		}
	}
	return ""
}

// Child is a key's block, or nil.
func (kv *KV) Child(key string) *KV {
	if kv == nil {
		return nil
	}
	for _, p := range kv.Pairs {
		if p.Value != nil && strings.EqualFold(p.Key, key) {
			return p.Value
		}
	}
	return nil
}

// Children are the nested blocks, in order.
func (kv *KV) Children() []Pair {
	if kv == nil {
		return nil
	}
	var out []Pair
	for _, p := range kv.Pairs {
		if p.Value != nil {
			out = append(out, p)
		}
	}
	return out
}

// ParseVDF reads Valve's KeyValues text.
func ParseVDF(r io.Reader) (*KV, error) {
	t := &tokenizer{r: bufio.NewReader(r)}
	kv, err := parseBlock(t, 0)
	if err != nil {
		return nil, err
	}
	return kv, nil
}

func parseBlock(t *tokenizer, depth int) (*KV, error) {
	if depth > 32 {
		return nil, errors.New("vdf: nested too deep")
	}
	kv := &KV{}
	for {
		tok, kind, err := t.next()
		if err == io.EOF {
			if depth > 0 {
				return nil, errors.New("vdf: unexpected end")
			}
			return kv, nil
		}
		if err != nil {
			return nil, err
		}
		if kind == '}' {
			if depth == 0 {
				return nil, errors.New("vdf: unexpected }")
			}
			return kv, nil
		}
		if kind != 's' {
			return nil, fmt.Errorf("vdf: expected a key, got %q", string(kind))
		}
		val, vkind, err := t.next()
		if err != nil {
			return nil, fmt.Errorf("vdf: no value for %q", tok)
		}
		switch vkind {
		case 's':
			kv.Pairs = append(kv.Pairs, Pair{Key: tok, Str: val})
		case '{':
			child, err := parseBlock(t, depth+1)
			if err != nil {
				return nil, err
			}
			kv.Pairs = append(kv.Pairs, Pair{Key: tok, Value: child})
		default:
			return nil, fmt.Errorf("vdf: unexpected %q after %q", string(vkind), tok)
		}
	}
}

type tokenizer struct{ r *bufio.Reader }

// next returns a string (kind 's') or a brace (kind '{' or '}').
func (t *tokenizer) next() (string, byte, error) {
	for {
		c, err := t.r.ReadByte()
		if err != nil {
			return "", 0, err
		}
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			continue
		case c == '/':
			if n, _ := t.r.Peek(1); len(n) == 1 && n[0] == '/' {
				t.r.ReadString('\n')
				continue
			}
			return t.bare(c)
		case c == '{' || c == '}':
			return "", c, nil
		case c == '"':
			var b strings.Builder
			for {
				c, err := t.r.ReadByte()
				if err != nil {
					return "", 0, errors.New("vdf: unterminated string")
				}
				if c == '"' {
					return b.String(), 's', nil
				}
				if c == '\\' {
					n, err := t.r.ReadByte()
					if err != nil {
						return "", 0, errors.New("vdf: unterminated string")
					}
					switch n {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					default:
						b.WriteByte(n)
					}
					continue
				}
				b.WriteByte(c)
			}
		default:
			return t.bare(c)
		}
	}
}

// bare reads a word without quotes, which some files have.
func (t *tokenizer) bare(first byte) (string, byte, error) {
	b := []byte{first}
	for {
		c, err := t.r.ReadByte()
		if err != nil {
			return string(b), 's', nil
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '{' || c == '}' || c == '"' {
			t.r.UnreadByte()
			return string(b), 's', nil
		}
		b = append(b, c)
	}
}

package api

import (
	"net/http"
	"slices"
	"strings"
	"unicode"

	"github.com/yeixio/matinee/internal/store"
)

// Voice: "Hey Siri, play Abbey Road on Matinee", or the same said to Google
// Assistant or in Android Auto. The phone apps hear what was asked for and
// ask GET /search/play for the one thing to play; see docs/voice.md.

// voiceKinds are what a voice request can play, in the order a plain "play
// X" prefers them when more than one is called exactly X: an album over the
// song it's named after, say.
var voiceKinds = []string{"artist", "album", "book", "track"}

// voiceAsk is what was asked for: the words as heard, and, when the phone
// worked them out, what kind of thing it is and who it's by.
type voiceAsk struct {
	Words  string // the title, or everything that was said
	Kind   string // artist, album, track or book; empty for any
	Artist string // who made it, or for a book who wrote it
	Album  string // the album a song is on
}

// parseVoice reads a request such as "the album Thriller", "Yesterday by the
// Beatles", "songs by Queen" or "The Hobbit audiobook". Fields the phone
// already worked out (kind, artist, album) win over what's read from the
// words.
func parseVoice(q, kind, artist, album string) voiceAsk {
	a := voiceAsk{Words: strings.TrimSpace(q), Kind: kind, Artist: strings.TrimSpace(artist), Album: strings.TrimSpace(album)}
	lower := strings.ToLower(a.Words)
	cut := func(n int) { a.Words = strings.TrimSpace(a.Words[n:]); lower = strings.ToLower(a.Words) }
	cutEnd := func(n int) { a.Words = strings.TrimSpace(a.Words[:len(a.Words)-n]); lower = strings.ToLower(a.Words) }
	setKind := func(k string) {
		if a.Kind == "" {
			a.Kind = k
		}
	}
	for _, p := range []struct{ prefix, kind string }{
		{"songs by ", "artist"}, {"music by ", "artist"}, {"the artist ", "artist"}, {"the band ", "artist"},
		{"the album ", "album"}, {"album ", "album"},
		{"the song ", "track"}, {"song ", "track"}, {"the track ", "track"},
		{"the audiobook ", "book"}, {"the audio book ", "book"}, {"the book ", "book"}, {"audiobook ", "book"},
	} {
		if strings.HasPrefix(lower, p.prefix) && len(a.Words) > len(p.prefix) {
			cut(len(p.prefix))
			setKind(p.kind)
			break
		}
	}
	for _, s := range []struct{ suffix, kind string }{
		{" audiobook", "book"}, {" audio book", "book"}, {" album", "album"}, {" song", "track"},
	} {
		if strings.HasSuffix(lower, s.suffix) && len(a.Words) > len(s.suffix) {
			cutEnd(len(s.suffix))
			setKind(s.kind)
			break
		}
	}
	return a
}

// splitBy reads "Yesterday by the Beatles" as a title and who it's by. The
// caller tries the whole thing too, for titles like "Stand by Me".
func splitBy(words string) (title, by string, ok bool) {
	i := strings.LastIndex(strings.ToLower(words), " by ")
	if i <= 0 || i+4 >= len(words) {
		return "", "", false
	}
	return strings.TrimSpace(words[:i]), strings.TrimSpace(words[i+4:]), true
}

// voiceNorm is a name as it's compared: lower case, letters and digits, one
// space between words, "and" for "&", without a leading "the".
func voiceNorm(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "&", " and ")
	words := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) > 1 && words[0] == "the" {
		words = words[1:]
	}
	return strings.Join(words, " ")
}

// closeness says how well a title matches what was said: 0 exactly, 1 it
// starts with it, 2 it has it, 3 only some of the words (search matched
// each word on its own).
func closeness(title, said string) int {
	t, s := voiceNorm(title), voiceNorm(said)
	switch {
	case t == s:
		return 0
	case strings.HasPrefix(t, s):
		return 1
	case strings.Contains(t, s):
		return 2
	}
	return 3
}

func byWho(it store.Item) string {
	if it.Kind == "book" {
		return it.Author
	}
	return it.Artist
}

// sameName is true when one name has the other in it, so "Beatles" finds
// "The Beatles" and "Simon" finds "Simon & Garfunkel".
func sameName(a, b string) bool {
	a, b = voiceNorm(a), voiceNorm(b)
	return a != "" && b != "" && (strings.Contains(a, b) || strings.Contains(b, a))
}

// bestVoiceMatch picks what to play from search results: only what can be
// heard, of the kind asked for, by whoever was named, then the closest title,
// then by kind (voiceKinds), then the shortest title.
func bestVoiceMatch(items []store.Item, a voiceAsk, title string) (store.Item, bool) {
	var found []store.Item
	for _, it := range items {
		if !slices.Contains(voiceKinds, it.Kind) || (a.Kind != "" && it.Kind != a.Kind) {
			continue
		}
		if a.Artist != "" && it.Kind != "artist" && !sameName(byWho(it), a.Artist) {
			continue
		}
		if a.Album != "" && it.Kind == "track" && !sameName(it.Album, a.Album) {
			continue
		}
		found = append(found, it)
	}
	if len(found) == 0 {
		return store.Item{}, false
	}
	slices.SortStableFunc(found, func(x, y store.Item) int {
		if d := closeness(x.Title, title) - closeness(y.Title, title); d != 0 {
			return d
		}
		if d := slices.Index(voiceKinds, x.Kind) - slices.Index(voiceKinds, y.Kind); d != 0 {
			return d
		}
		return len(x.Title) - len(y.Title)
	})
	return found[0], true
}

// voiceMatch is GET /search/play's answer.
type voiceMatch struct {
	// Kind is artist, album, track or book.
	Kind string     `json:"kind"`
	Item store.Item `json:"item"`
}

// searchPlay finds the one thing to play for what someone asked for out
// loud. q is what was said (or the title, when the phone split it up); kind,
// artist and album are what the phone worked out, if anything. 404 when
// nothing fits.
func (s *Server) searchPlay(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	// A kind it can't play (say, movie) finds nothing.
	a := parseVoice(query.Get("q"), query.Get("kind"), query.Get("artist"), query.Get("album"))
	// Asked for by name only ("play the Beatles"): the artist is the title.
	if a.Words == "" {
		a.Words = a.Album
		if a.Words == "" {
			a.Words = a.Artist
			a.Artist = ""
		}
	}
	if a.Words == "" {
		writeError(w, http.StatusBadRequest, "say what to play")
		return
	}
	ctx, user := r.Context(), currentUser(r)
	find := func(a voiceAsk) (store.Item, bool, error) {
		items, err := s.Store.Search(ctx, user.ID, a.Words, 50)
		if err != nil {
			return store.Item{}, false, err
		}
		it, ok := bestVoiceMatch(items, a, a.Words)
		return it, ok, nil
	}
	tries := []voiceAsk{}
	if title, by, ok := splitBy(a.Words); ok && a.Artist == "" {
		split := a
		split.Words, split.Artist = title, by
		tries = append(tries, split)
	}
	tries = append(tries, a)
	// Someone named but nothing of theirs by that title: the title alone.
	if a.Artist != "" {
		loose := a
		loose.Artist = ""
		tries = append(tries, loose)
	}
	for _, try := range tries {
		it, ok, err := find(try)
		if err != nil {
			s.serverError(w, err)
			return
		}
		if ok {
			writeJSON(w, http.StatusOK, voiceMatch{Kind: it.Kind, Item: it})
			return
		}
	}
	writeError(w, http.StatusNotFound, "nothing found to play")
}

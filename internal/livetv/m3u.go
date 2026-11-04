// Package livetv plays live TV channels from playlists people add: an M3U
// playlist lists channels and where each one streams, and an XMLTV guide
// says what's on them. Matinee checks each channel's stream now and then
// and hides the ones that don't answer, passes the streams through the
// server so every player can reach them (see hls.go), and suggests
// iptv-org's lists of publicly available channels as a place to start
// (see iptvorg.go).
//
// Matinee only plays what a playlist points at. It doesn't look for
// streams on websites, and doesn't get around ads or copy protection.
package livetv

import (
	"bufio"
	"io"
	"net/url"
	"strings"
)

// MaxChannels is the most channels Matinee reads from one playlist.
const MaxChannels = 20000

// Entry is one channel in a playlist.
type Entry struct {
	Name    string
	GuideID string // tvg-id: the channel's id in the guide
	Logo    string
	// Groups are the playlist's groups for the channel (group-title),
	// such as News; iptv-org separates several with ;.
	Groups    []string
	Number    string // tvg-chno, the channel number
	Language  string // tvg-language, as the playlist wrote it
	Country   string // tvg-country
	URL       string // the stream
	UserAgent string // #EXTVLCOPT:http-user-agent
	Referrer  string // #EXTVLCOPT:http-referrer
}

// Playlist is what an M3U file lists.
type Playlist struct {
	// GuideURL is the guide the playlist names in its header (x-tvg-url
	// or url-tvg), if any.
	GuideURL string
	Entries  []Entry
}

// ParsePlaylist reads an M3U playlist. base resolves relative stream
// addresses. Entries that don't stream over http or https are skipped.
func ParsePlaylist(r io.Reader, base string) (Playlist, error) {
	var p Playlist
	b, _ := url.Parse(base)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var cur *Entry
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			line = strings.TrimPrefix(line, "\ufeff")
			first = false
		}
		switch {
		case line == "":
		case strings.HasPrefix(line, "#EXTM3U"):
			attrs, _ := parseAttrs(strings.TrimPrefix(line, "#EXTM3U"))
			for _, k := range []string{"x-tvg-url", "url-tvg", "tvg-url"} {
				if v := attrs[k]; v != "" && p.GuideURL == "" {
					// Some name several guides, separated by commas.
					v, _, _ = strings.Cut(v, ",")
					p.GuideURL = strings.TrimSpace(v)
				}
			}
		case strings.HasPrefix(line, "#EXTINF:"):
			attrs, title := parseAttrs(strings.TrimPrefix(line, "#EXTINF:"))
			e := Entry{
				Name:     clean(firstOf(title, attrs["tvg-name"])),
				GuideID:  clean(attrs["tvg-id"]),
				Logo:     webAddress(attrs["tvg-logo"]),
				Number:   clean(attrs["tvg-chno"]),
				Language: clean(attrs["tvg-language"]),
				Country:  clean(attrs["tvg-country"]),
			}
			e.Groups = splitGroups(attrs["group-title"])
			cur = &e
		case strings.HasPrefix(line, "#EXTGRP:"):
			if cur != nil && len(cur.Groups) == 0 {
				cur.Groups = splitGroups(strings.TrimPrefix(line, "#EXTGRP:"))
			}
		case strings.HasPrefix(line, "#EXTVLCOPT:"):
			if cur == nil {
				continue
			}
			k, v, _ := strings.Cut(strings.TrimPrefix(line, "#EXTVLCOPT:"), "=")
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "http-user-agent":
				cur.UserAgent = clean(v)
			case "http-referrer", "http-referer":
				cur.Referrer = webAddress(v)
			}
		case strings.HasPrefix(line, "#"):
		default:
			if cur == nil {
				continue
			}
			// Some playlists add Kodi-style options after a |.
			addr, opts, _ := strings.Cut(line, "|")
			u, err := url.Parse(strings.TrimSpace(addr))
			if err == nil && b != nil {
				u = b.ResolveReference(u)
			}
			if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
				cur.URL = u.String()
				for _, kv := range strings.Split(opts, "&") {
					k, v, _ := strings.Cut(kv, "=")
					v, _ = url.QueryUnescape(v)
					switch strings.ToLower(k) {
					case "user-agent":
						cur.UserAgent = firstOf(cur.UserAgent, clean(v))
					case "referer", "referrer":
						cur.Referrer = firstOf(cur.Referrer, webAddress(v))
					}
				}
				if cur.Name == "" {
					cur.Name = u.Host
				}
				p.Entries = append(p.Entries, *cur)
				if len(p.Entries) >= MaxChannels {
					return p, nil
				}
			}
			cur = nil
		}
	}
	return p, sc.Err()
}

// parseAttrs reads key="value" pairs, and the title after the comma that
// follows them: `-1 tvg-id="a" group-title="News, Talk",Channel`.
func parseAttrs(s string) (map[string]string, string) {
	attrs := map[string]string{}
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ',':
			return attrs, strings.TrimSpace(s[i+1:])
		case c == ' ' || c == '\t':
			i++
		default:
			// A key, then =, then a quoted or bare value.
			j := i
			for j < len(s) && s[j] != '=' && s[j] != ' ' && s[j] != ',' {
				j++
			}
			key := strings.ToLower(s[i:j])
			if j >= len(s) || s[j] != '=' {
				i = j
				continue
			}
			j++
			var val string
			if j < len(s) && (s[j] == '"' || s[j] == '\'') {
				q := s[j]
				end := strings.IndexByte(s[j+1:], q)
				if end < 0 {
					val, j = s[j+1:], len(s)
				} else {
					val, j = s[j+1:j+1+end], j+2+end
				}
			} else {
				k := j
				for k < len(s) && s[k] != ' ' && s[k] != ',' {
					k++
				}
				val, j = s[j:k], k
			}
			attrs[key] = val
			i = j
		}
	}
	return attrs, ""
}

func splitGroups(s string) []string {
	var out []string
	for _, g := range strings.Split(s, ";") {
		if g = clean(g); g != "" && len(out) < 5 {
			out = append(out, g)
		}
	}
	return out
}

// webAddress is s if it's an http or https address, or "".
func webAddress(s string) string {
	s = strings.TrimSpace(s)
	if u, err := url.Parse(s); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		return s
	}
	return ""
}

func firstOf(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// clean drops what isn't printable, and trims.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == 0xfffd {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 300 {
		s = string(r[:300])
	}
	return s
}

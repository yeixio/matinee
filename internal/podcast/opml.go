package podcast

import (
	"bytes"
	"encoding/xml"
	"errors"
	"strings"
	"time"
)

// OPML is how podcast apps hand each other the shows someone follows: a
// list of feeds.

// OPMLFeed is one show in an OPML file.
type OPMLFeed struct {
	Title string
	URL   string
}

type opmlOutline struct {
	Text     string        `xml:"text,attr"`
	Title    string        `xml:"title,attr,omitempty"`
	Type     string        `xml:"type,attr,omitempty"`
	XMLURL   string        `xml:"xmlUrl,attr,omitempty"`
	Outlines []opmlOutline `xml:"outline"`
}

type opmlDoc struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    struct {
		Title       string `xml:"title"`
		DateCreated string `xml:"dateCreated,omitempty"`
	} `xml:"head"`
	Body struct {
		Outlines []opmlOutline `xml:"outline"`
	} `xml:"body"`
}

// ParseOPML lists the feeds in an OPML file, in folders or not.
func ParseOPML(data []byte) ([]OPMLFeed, error) {
	var doc opmlDoc
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.CharsetReader = charsetReader
	if err := dec.Decode(&doc); err != nil {
		return nil, ErrNotOPML
	}
	var out []OPMLFeed
	seen := map[string]bool{}
	var walk func([]opmlOutline)
	walk = func(os []opmlOutline) {
		for _, o := range os {
			if u := strings.TrimSpace(o.XMLURL); u != "" && !seen[u] {
				seen[u] = true
				out = append(out, OPMLFeed{Title: clean(first(o.Title, o.Text)), URL: u})
			}
			walk(o.Outlines)
		}
	}
	walk(doc.Body.Outlines)
	return out, nil
}

// ErrNotOPML means a file isn't an OPML list of feeds.
var ErrNotOPML = errors.New("that isn't an OPML file")

// WriteOPML makes an OPML file of feeds.
func WriteOPML(title string, feeds []OPMLFeed, now time.Time) []byte {
	var doc opmlDoc
	doc.Version = "2.0"
	doc.Head.Title = title
	doc.Head.DateCreated = now.UTC().Format(time.RFC1123Z)
	for _, f := range feeds {
		doc.Body.Outlines = append(doc.Body.Outlines, opmlOutline{Text: f.Title, Title: f.Title, Type: "rss", XMLURL: f.URL})
	}
	b, _ := xml.MarshalIndent(doc, "", "  ")
	return append([]byte(xml.Header), append(b, '\n')...)
}

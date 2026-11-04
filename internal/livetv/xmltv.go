package livetv

import (
	"bufio"
	"compress/gzip"
	"encoding/xml"
	"io"
	"strings"
	"time"
)

// Program is one show in the guide.
type Program struct {
	Channel     string // the guide's id for the channel
	Start, Stop time.Time
	Title       string
	Subtitle    string // such as an episode's name
	Description string
	Category    string
	Image       string
}

// GuideChannel is a channel the guide lists.
type GuideChannel struct {
	ID    string
	Names []string
	Logo  string
}

// ParseGuide reads an XMLTV guide, gzipped or not. It calls channel for
// each channel the guide lists, and program for each show that's on
// between from and to on a channel wanted says yes to. Guides list
// channels before their shows.
func ParseGuide(r io.Reader, from, to time.Time, wanted func(id string) bool, channel func(GuideChannel), program func(Program)) error {
	br := bufio.NewReader(r)
	if magic, err := br.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return err
		}
		defer gz.Close()
		br = bufio.NewReader(gz)
	}
	d := xml.NewDecoder(br)
	d.Strict = false
	d.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "channel":
			var c struct {
				ID    string `xml:"id,attr"`
				Names []struct {
					Text string `xml:",chardata"`
				} `xml:"display-name"`
				Icon struct {
					Src string `xml:"src,attr"`
				} `xml:"icon"`
			}
			if err := d.DecodeElement(&c, &se); err != nil {
				return err
			}
			gc := GuideChannel{ID: strings.TrimSpace(c.ID), Logo: webAddress(c.Icon.Src)}
			for _, n := range c.Names {
				if n := clean(n.Text); n != "" {
					gc.Names = append(gc.Names, n)
				}
			}
			if gc.ID != "" && channel != nil {
				channel(gc)
			}
		case "programme":
			var id, startS, stopS string
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "channel":
					id = strings.TrimSpace(a.Value)
				case "start":
					startS = a.Value
				case "stop":
					stopS = a.Value
				}
			}
			start, ok1 := parseTime(startS)
			stop, ok2 := parseTime(stopS)
			if !ok2 && ok1 {
				stop, ok2 = start.Add(30*time.Minute), true
			}
			if !ok1 || !ok2 || !stop.After(start) || !stop.After(from) || !start.Before(to) || !wanted(id) {
				if err := d.Skip(); err != nil {
					return err
				}
				continue
			}
			var p struct {
				Title    []string `xml:"title"`
				Sub      []string `xml:"sub-title"`
				Desc     []string `xml:"desc"`
				Category []string `xml:"category"`
				Icon     struct {
					Src string `xml:"src,attr"`
				} `xml:"icon"`
			}
			if err := d.DecodeElement(&p, &se); err != nil {
				return err
			}
			pr := Program{Channel: id, Start: start, Stop: stop, Title: clean(first(p.Title)), Subtitle: clean(first(p.Sub)),
				Category: clean(first(p.Category)), Image: webAddress(p.Icon.Src)}
			pr.Description = strings.TrimSpace(first(p.Desc))
			if r := []rune(pr.Description); len(r) > 2000 {
				pr.Description = string(r[:2000])
			}
			if pr.Title != "" {
				program(pr)
			}
		}
	}
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// parseTime reads XMLTV's times, such as "20261104183000 +0000". Without
// a zone, the time is UTC; seconds and minutes may be left out.
func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	stamp, zone, _ := strings.Cut(s, " ")
	if len(stamp) > 14 {
		zone, stamp = strings.TrimSpace(stamp[14:]), stamp[:14]
	}
	for len(stamp) < 14 && len(stamp) >= 10 {
		stamp += "0"
	}
	t, err := time.Parse("20060102150405", stamp)
	if err != nil {
		return time.Time{}, false
	}
	if zone = strings.TrimSpace(zone); zone != "" {
		if z, err := time.Parse("-0700", zone); err == nil {
			_, off := z.Zone()
			t = t.Add(-time.Duration(off) * time.Second)
		}
	}
	return t.UTC(), true
}

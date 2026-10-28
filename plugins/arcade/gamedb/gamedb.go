// Package gamedb is the arcade plugin's list of games: what MAME's XML list
// (mame -listxml) says about each arcade machine, cut down to what the
// plugin shows and what the player needs to start it.
package gamedb

import (
	"compress/gzip"
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"slices"
	"strings"
)

// DB is every arcade game in one MAME version.
type DB struct {
	// MAME is the version the list came from, such as 0.289.
	MAME  string  `json:"mame"`
	Games []*Game `json:"games"`

	byName map[string]*Game
}

// Game is one arcade machine.
type Game struct {
	Name         string `json:"n"`
	Description  string `json:"d"`
	Year         string `json:"y,omitempty"` // as MAME has it: 1981, 198?
	Manufacturer string `json:"m,omitempty"`
	CloneOf      string `json:"c,omitempty"`
	// Source is the driver's source file, such as pacman/pacman.cpp. The
	// player loads the MAME build for it.
	Source string `json:"s"`
	// Needs are the other sets MAME reads this game's ROMs from: its parent
	// and BIOS, and devices with ROMs of their own.
	Needs    []string `json:"r,omitempty"`
	Players  int      `json:"p,omitempty"`
	Buttons  int      `json:"b,omitempty"`
	Controls []string `json:"ctl,omitempty"`
	Rotate   int      `json:"rot,omitempty"`
	Width    int      `json:"w,omitempty"`
	Height   int      `json:"h,omitempty"`
	// Status is MAME's emulation status: good, imperfect or preliminary.
	Status    string `json:"st,omitempty"`
	SaveState bool   `json:"ss,omitempty"`
	// Disk is set when the game needs a hard disk or CD image (a CHD),
	// which the player can't load.
	Disk bool `json:"disk,omitempty"`
}

// Game finds a game by its MAME name.
func (db *DB) Game(name string) *Game {
	if db.byName == nil {
		db.byName = make(map[string]*Game, len(db.Games))
		for _, g := range db.Games {
			db.byName[g.Name] = g
		}
	}
	return db.byName[name]
}

// Load reads a list Write made.
func Load(path string) (*DB, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	var db DB
	if err := json.NewDecoder(zr).Decode(&db); err != nil {
		return nil, err
	}
	return &db, nil
}

// Write saves the list, gzipped.
func (db *DB) Write(w io.Writer) error {
	zw, _ := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err := json.NewEncoder(zw).Encode(db); err != nil {
		return err
	}
	return zw.Close()
}

// The parts of MAME's XML list the plugin reads.
type xmlMachine struct {
	Name         string `xml:"name,attr"`
	Source       string `xml:"sourcefile,attr"`
	IsBIOS       string `xml:"isbios,attr"`
	IsDevice     string `xml:"isdevice,attr"`
	IsMechanical string `xml:"ismechanical,attr"`
	Runnable     string `xml:"runnable,attr"`
	CloneOf      string `xml:"cloneof,attr"`
	RomOf        string `xml:"romof,attr"`
	Description  string `xml:"description"`
	Year         string `xml:"year"`
	Manufacturer string `xml:"manufacturer"`
	ROMs         []struct {
		Status string `xml:"status,attr"`
	} `xml:"rom"`
	Disks   []struct{} `xml:"disk"`
	Devices []struct {
		Name string `xml:"name,attr"`
	} `xml:"device_ref"`
	Displays []struct {
		Rotate int `xml:"rotate,attr"`
		Width  int `xml:"width,attr"`
		Height int `xml:"height,attr"`
	} `xml:"display"`
	Input *struct {
		Players  int `xml:"players,attr"`
		Coins    int `xml:"coins,attr"`
		Controls []struct {
			Type    string `xml:"type,attr"`
			Buttons int    `xml:"buttons,attr"`
			Ways    string `xml:"ways,attr"`
		} `xml:"control"`
	} `xml:"input"`
	Driver *struct {
		Status    string `xml:"status,attr"`
		SaveState string `xml:"savestate,attr"`
	} `xml:"driver"`
}

// FromXML builds the list from MAME's XML list. It keeps machines with a
// coin slot, which is what makes a machine an arcade game rather than a
// computer or console, and leaves out BIOS sets, devices and mechanical
// games.
func FromXML(r io.Reader) (*DB, error) {
	dec := xml.NewDecoder(r)
	db := &DB{}
	var machines []*xmlMachine
	byName := map[string]*xmlMachine{}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "mame":
			for _, a := range se.Attr {
				if a.Name.Local == "build" {
					db.MAME, _, _ = strings.Cut(a.Value, " ")
				}
			}
		case "machine":
			var m xmlMachine
			if err := dec.DecodeElement(&m, &se); err != nil {
				return nil, err
			}
			machines = append(machines, &m)
			byName[m.Name] = &m
		}
	}

	hasROMs := func(m *xmlMachine) bool {
		for _, r := range m.ROMs {
			if r.Status != "nodump" {
				return true
			}
		}
		return false
	}
	for _, m := range machines {
		if m.IsBIOS == "yes" || m.IsDevice == "yes" || m.IsMechanical == "yes" || m.Runnable == "no" || m.Input == nil || m.Input.Coins == 0 {
			continue
		}
		g := &Game{
			Name:         m.Name,
			Description:  m.Description,
			Year:         m.Year,
			Manufacturer: m.Manufacturer,
			CloneOf:      m.CloneOf,
			Source:       m.Source,
			Players:      m.Input.Players,
			Disk:         len(m.Disks) > 0,
		}
		// The parent, its BIOS, and so on up.
		seen := map[string]bool{m.Name: true}
		for p := m.RomOf; p != "" && !seen[p]; {
			seen[p] = true
			g.Needs = append(g.Needs, p)
			pm := byName[p]
			if pm == nil {
				break
			}
			p = pm.RomOf
		}
		for _, d := range m.Devices {
			if dm := byName[d.Name]; dm != nil && !seen[d.Name] && hasROMs(dm) {
				seen[d.Name] = true
				g.Needs = append(g.Needs, d.Name)
			}
		}
		for _, c := range m.Input.Controls {
			g.Controls = append(g.Controls, controlName(c.Type, c.Ways))
			g.Buttons = max(g.Buttons, c.Buttons)
		}
		g.Controls = dedupe(g.Controls)
		if len(m.Displays) > 0 {
			d := m.Displays[0]
			g.Rotate, g.Width, g.Height = d.Rotate, d.Width, d.Height
		}
		if m.Driver != nil {
			g.Status = m.Driver.Status
			g.SaveState = m.Driver.SaveState == "supported"
		}
		db.Games = append(db.Games, g)
	}
	return db, nil
}

func controlName(typ, ways string) string {
	name := map[string]string{
		"joy":          "Joystick",
		"doublejoy":    "Twin joysticks",
		"triplejoy":    "Three joysticks",
		"stick":        "Analog stick",
		"lightgun":     "Light gun",
		"trackball":    "Trackball",
		"dial":         "Dial",
		"paddle":       "Paddle",
		"pedal":        "Pedal",
		"positional":   "Wheel",
		"gambling":     "Gambling buttons",
		"hanafuda":     "Hanafuda panel",
		"mahjong":      "Mahjong panel",
		"keypad":       "Keypad",
		"keyboard":     "Keyboard",
		"mouse":        "Mouse",
		"only_buttons": "Buttons",
	}[typ]
	if name == "" {
		name = typ
	}
	switch ways {
	case "2", "4", "8":
		name += " (" + ways + "-way)"
	}
	return name
}

func dedupe(s []string) []string {
	var out []string
	for _, v := range s {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// Package rating turns a country's age ratings, such as PG-13 or TV-MA, into
// the few levels kids profiles choose from.
package rating

import (
	"strconv"
	"strings"
)

// Levels, from the youngest audience up. 0 means the rating is unknown.
const (
	Unknown    = 0
	LittleKids = 1 // all ages: G, TV-Y, TV-G, U
	Kids       = 2 // PG, TV-Y7, TV-PG
	Teens      = 3 // PG-13, TV-14, 12A
	Adults     = 4 // R, NC-17, TV-MA, 15, 18
)

// DefaultCountry is used when none is set.
const DefaultCountry = "US"

// ratings by country, as TMDB spells them. Countries not listed here, and
// ratings not listed for a country, fall back to the age in the rating.
var ratings = map[string]map[string]int{
	"US": {
		"G": LittleKids, "TV-Y": LittleKids, "TV-G": LittleKids,
		"PG": Kids, "TV-Y7": Kids, "TV-Y7-FV": Kids, "TV-PG": Kids,
		"PG-13": Teens, "TV-14": Teens,
		"R": Adults, "NC-17": Adults, "TV-MA": Adults,
	},
	"GB": {
		"U": LittleKids, "UC": LittleKids, "PG": Kids, "12A": Teens, "12": Teens,
		"15": Adults, "18": Adults, "R18": Adults,
	},
	"IE": {
		"G": LittleKids, "PG": Kids, "12A": Teens, "12": Teens, "15A": Adults, "16": Adults, "18": Adults,
	},
	"CA": {
		"G": LittleKids, "C": LittleKids, "C8": Kids, "PG": Kids, "14A": Teens, "14+": Teens,
		"18A": Adults, "18+": Adults, "R": Adults, "A": Adults,
	},
	"AU": {
		"E": LittleKids, "G": LittleKids, "P": LittleKids, "C": LittleKids, "PG": Kids, "M": Teens,
		"MA15+": Adults, "AV15+": Adults, "R18+": Adults, "X18+": Adults,
	},
	"NZ": {
		"G": LittleKids, "PG": Kids, "M": Teens, "R13": Teens,
		"R15": Adults, "R16": Adults, "R18": Adults, "R": Adults,
	},
	"FR": {"U": LittleKids, "TP": LittleKids},
	"ES": {"A": LittleKids, "APTA": LittleKids, "TP": LittleKids},
	"NL": {"AL": LittleKids},
	"BR": {"L": LittleKids, "ER": Kids},
	"DE": {"0": LittleKids},
}

// Level says which level a rating from the given country belongs to.
func Level(country, cert string) int {
	cert = strings.ToUpper(strings.TrimSpace(cert))
	if cert == "" {
		return Unknown
	}
	if l, ok := ratings[strings.ToUpper(country)][cert]; ok {
		return l
	}
	// Most countries rate by age, e.g. "12", "FSK 16" or "-10".
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, cert)
	age, err := strconv.Atoi(digits)
	if err != nil {
		return Unknown
	}
	switch {
	case age < 6:
		return LittleKids
	case age < 12:
		return Kids
	case age < 15:
		return Teens
	}
	return Adults
}

// Name is how the apps describe a kids level.
func Name(level int) string {
	switch level {
	case LittleKids:
		return "Little kids"
	case Kids:
		return "Kids"
	case Teens:
		return "Teens"
	}
	return ""
}

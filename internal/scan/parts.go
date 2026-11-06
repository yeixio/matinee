package scan

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// A movie can come in parts, one file each, as rips that fit on CDs did:
// "Heat (1995) CD1.avi" and "Heat (1995) CD2.avi". The parts of a movie
// show as one movie that plays them one after the other (see
// store.GroupParts).

// partRe finds "CD1", "cd 2", "Part1", "pt.2", "Disc 1", "Disk2" or
// "DVD1" standing on its own in a name.
var partRe = regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])((cd|dvd|disc|disk|part|pt)[\s._-]?(\d{1,2}))(?:$|[\s._\-\])])`)

// Part reads a movie file's part number from its name, and returns the
// name without it, so the parts of one movie share the rest. part is 0
// when the name names no part. "Part" counts only after the year, as in
// "Heat.1995.Part1.avi", or in a file whose folder has the year, so
// "Deathly Hallows Part 1 (2010)" stays a title of its own.
func Part(path string) (rest string, part int) {
	name := stem(path)
	locs := partRe.FindAllStringSubmatchIndex(name, -1)
	if len(locs) == 0 {
		return name, 0
	}
	loc := locs[len(locs)-1] // the last one; a title may hold "Part" too
	start, end := loc[2], loc[3]
	word := strings.ToLower(name[loc[4]:loc[5]])
	n, _ := strconv.Atoi(name[loc[6]:loc[7]])
	if n == 0 || start == 0 {
		return name, 0 // "CD1" alone is the whole name, not a part of one
	}
	if word == "part" || word == "pt" {
		yearBefore := false
		for _, l := range yearRe.FindAllStringIndex(name[:start], -1) {
			if l[0] > 0 && isSep(name[l[0]-1]) && (l[1] == start || isSep(name[l[1]])) {
				yearBefore = true
			}
		}
		folder := filepath.Base(filepath.Dir(path))
		yearAfter := yearRe.MatchString(name[end:])
		if !yearBefore && (yearAfter || parseMovieName(folder).Year == 0 || partRe.MatchString(folder)) {
			return name, 0
		}
	}
	rest = strings.TrimRight(name[:start], " ._-([") + " " + strings.TrimLeft(name[end:], " ._-)]")
	return strings.TrimSpace(rest), n
}

// PartKey says which movie a file is a part of: files in the same folder
// whose names match but for the part. key is "" when the name names no part.
func PartKey(path string) (key string, part int) {
	rest, n := Part(path)
	if n == 0 {
		return "", 0
	}
	return filepath.Dir(path) + "\x00" + strings.ToLower(rest) + strings.ToLower(filepath.Ext(path)), n
}

package importer

import (
	"sort"
	"strings"
)

// PathChange turns the start of the other server's paths into Matinee's:
// the same share may be /data/movies to one and /mnt/media/movies to the
// other.
type PathChange struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Files is how many files the change finds, when it's a suggestion.
	Files int `json:"files,omitempty"`
}

func isSep(c byte) bool { return c == '/' || c == '\\' }

// windowsy says whether a path looks like a Windows one, where case doesn't
// matter.
func windowsy(p string) bool {
	return strings.Contains(p, `\`) || (len(p) >= 2 && p[1] == ':')
}

// under reports whether p is from or inside it, and how long the matching
// start of p is.
func under(p, from string) (int, bool) {
	from = strings.TrimRight(from, `/\`)
	if from == "" {
		return 0, false
	}
	if len(p) < len(from) {
		return 0, false
	}
	head := p[:len(from)]
	if head != from && !(windowsy(from) && strings.EqualFold(head, from)) {
		return 0, false
	}
	if len(p) > len(from) && !isSep(p[len(from)]) {
		return 0, false
	}
	return len(from), true
}

// MapPath applies the change with the longest matching start, and writes
// the rest of the path with the separators Matinee's side uses.
func MapPath(p string, changes []PathChange) string {
	best, n := -1, 0
	for i, c := range changes {
		if l, ok := under(p, c.From); ok && l > n {
			best, n = i, l
		}
	}
	if best < 0 {
		return p
	}
	to := strings.TrimRight(changes[best].To, `/\`)
	rest := p[n:]
	sep := "/"
	if strings.Contains(to, `\`) && !strings.Contains(to, "/") {
		sep = `\`
	}
	if sep == "/" {
		rest = strings.ReplaceAll(rest, `\`, "/")
	} else {
		rest = strings.ReplaceAll(rest, "/", `\`)
	}
	if to == "" && sep == "/" {
		return rest
	}
	return to + rest
}

func base(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if isSep(p[i]) {
			return p[i+1:]
		}
	}
	return p
}

// startBefore returns p without its last k parts, or false when it has no
// more than k.
func startBefore(p string, k int) (string, bool) {
	end := len(p)
	for range k {
		i := end - 1
		for i >= 0 && !isSep(p[i]) {
			i--
		}
		if i <= 0 {
			return "", false
		}
		end = i
	}
	return p[:end], true
}

// sameEnd counts how many trailing parts two paths share.
func sameEnd(a, b string) int {
	pa, pb := splitPath(a), splitPath(b)
	n := 0
	for n < len(pa) && n < len(pb) && pa[len(pa)-1-n] == pb[len(pb)-1-n] {
		n++
	}
	return n
}

func splitPath(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
}

// SuggestChanges looks at the files that don't match and finds the path
// changes that would match them: a Matinee file with the same name and the
// same folders above it, up to where the two paths differ. It returns the
// changes that each find at least two files (or one, when that's all there
// is), most files first.
func SuggestChanges(files []string, known func(string) bool, byName map[string][]string, have []PathChange) []PathChange {
	changes := append([]PathChange(nil), have...)
	var out []PathChange
	for range 10 {
		votes := map[PathChange]int{}
		missing := 0
		for _, f := range files {
			if known(MapPath(f, changes)) {
				continue
			}
			missing++
			if missing > 20000 {
				break
			}
			for _, m := range byName[base(f)] {
				k := sameEnd(f, m)
				// Keep at least the top folder of each side, so the change
				// isn't "everything" to "everything".
				from, ok1 := startBefore(f, k)
				to, ok2 := startBefore(m, k)
				if !ok1 || !ok2 {
					continue
				}
				votes[PathChange{From: from, To: to}]++
			}
		}
		if len(votes) == 0 {
			break
		}
		var best PathChange
		top := 0
		for c, n := range votes {
			if n > top || n == top && c.From+c.To < best.From+best.To {
				best, top = c, n
			}
		}
		if top < 2 && !(top == 1 && missing == 1) {
			break
		}
		best.Files = top
		changes = append(changes, PathChange{From: best.From, To: best.To})
		out = append(out, best)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Files > out[j].Files })
	return out
}

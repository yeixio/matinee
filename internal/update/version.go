package update

import (
	"regexp"
	"strconv"
	"strings"
)

// Versions look like v1.2.3 or 1.2.3-beta.1 (releases) or main-<commit>
// (images built from main, which the beta channel follows).

var (
	releaseRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$`)
	mainRe    = regexp.MustCompile(`^main-([0-9a-f]{7,40})$`)
)

func isRelease(v string) bool {
	return releaseRe.MatchString(v) && !strings.HasPrefix(strings.TrimPrefix(v, "v"), "0.0.0")
}

func isMainBuild(v string) bool { return mainRe.MatchString(v) }

// mainCommit is the commit a main build was made from.
func mainCommit(v string) string {
	if m := mainRe.FindStringSubmatch(v); m != nil {
		return m[1]
	}
	return ""
}

// Display is a version as people see it: v1.2.3, or main-abc1234 with the
// commit cut to seven characters, as the image tag sha-abc1234 has it.
func Display(v string) string {
	if c := mainCommit(v); c != "" {
		return "main-" + short(c)
	}
	if isRelease(v) && !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// Newer is whether release a comes after release b, by semantic versioning:
// 1.2.0 after 1.2.0-beta.2 after 1.2.0-beta.1. Anything that isn't a
// release is never newer, nor older.
func Newer(a, b string) bool { return compare(a, b) > 0 }

func compare(a, b string) int {
	ma, mb := releaseRe.FindStringSubmatch(a), releaseRe.FindStringSubmatch(b)
	if ma == nil || mb == nil {
		return 0
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(ma[i])
		y, _ := strconv.Atoi(mb[i])
		if x != y {
			return cmpInt(x, y)
		}
	}
	pa, pb := ma[4], mb[4]
	switch {
	case pa == pb:
		return 0
	case pa == "":
		return 1
	case pb == "":
		return -1
	}
	xs, ys := strings.Split(pa, "."), strings.Split(pb, ".")
	for i := 0; i < len(xs) && i < len(ys); i++ {
		x, errX := strconv.Atoi(xs[i])
		y, errY := strconv.Atoi(ys[i])
		switch {
		case errX == nil && errY == nil:
			if x != y {
				return cmpInt(x, y)
			}
		case errX == nil:
			return -1 // numbers sort before words
		case errY == nil:
			return 1
		default:
			if c := strings.Compare(xs[i], ys[i]); c != 0 {
				return c
			}
		}
	}
	return cmpInt(len(xs), len(ys))
}

func cmpInt(x, y int) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

// tag is the git tag for a release version.
func tag(v string) string { return "v" + strings.TrimPrefix(v, "v") }

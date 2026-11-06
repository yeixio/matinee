// Package update looks for newer versions of Matinee and, where the server
// can, installs them: Settings → Server health → Updates.
//
// There are two channels. Releases are the tagged versions (v1.2.3), the
// default. Main is every commit on main once its checks pass and its image
// is published (the edge branch), a beta that can break.
//
// How an update is installed depends on how Matinee was installed (see
// Detect). Under Argo CD it moves the Application to the new revision and
// Argo CD rolls it out; a plain binary replaces itself and restarts. In a
// container, from a package manager, or inside the Mac or Windows app,
// whatever installed Matinee updates it, so the server only says what's new
// and how.
package update

import (
	"regexp"
	"strconv"
	"strings"
)

// The channels.
const (
	ChannelRelease = "release"
	ChannelMain    = "main"
)

// Build is the version that's running, from version.Version.
type Build struct {
	Version string `json:"version"`
	// Release is a tagged version without its v, such as 0.2.3 or 0.3.0-beta.1.
	Release string `json:"release,omitempty"`
	// Commit is a build of main: CI builds them as main-<commit>.
	Commit string `json:"commit,omitempty"`
}

var (
	commitRE   = regexp.MustCompile(`^main-([0-9a-f]{7,40})$`)
	describeRE = regexp.MustCompile(`-[0-9]+-g[0-9a-f]+(-dirty)?$|-dirty$`)
)

// ParseBuild reads a version. Anything else, such as dev or a local build
// (git describe's v0.2.2-3-gabc1234), is a build nothing can be compared
// with, so it's never offered updates.
func ParseBuild(v string) Build {
	b := Build{Version: v}
	if m := commitRE.FindStringSubmatch(v); m != nil {
		b.Commit = m[1]
		return b
	}
	if describeRE.MatchString(v) {
		return b
	}
	if _, ok := parseSemver(v); ok {
		b.Release = strings.TrimPrefix(v, "v")
	}
	return b
}

// Known is whether the build is a release or a main commit.
func (b Build) Known() bool { return b.Release != "" || b.Commit != "" }

// Channel is the channel the build came from.
func (b Build) Channel() string {
	if b.Commit != "" {
		return ChannelMain
	}
	if b.Release != "" {
		return ChannelRelease
	}
	return ""
}

// Revision is the git revision the build was made from: its tag, or its commit.
func (b Build) Revision() string {
	if b.Commit != "" {
		return b.Commit
	}
	if b.Release != "" {
		return "v" + b.Release
	}
	return ""
}

// ImageTag is the build's container image: CI tags each main commit's
// sha-<7 characters>, and the release workflow each release's version.
func (b Build) ImageTag() string {
	if b.Commit != "" {
		return "sha-" + short(b.Commit)
	}
	return b.Release
}

func short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

type semver struct {
	core [3]int
	pre  string
}

func parseSemver(v string) (semver, bool) {
	var s semver
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, s.pre = v[:i], v[i+1:]
		if s.pre == "" {
			return s, false
		}
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return s, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return s, false
		}
		s.core[i] = n
	}
	return s, true
}

// Newer is whether version a comes after version b (both like 1.2.3, with
// or without a v). A pre-release comes before its release.
func Newer(a, b string) bool {
	pa, okA := parseSemver(a)
	pb, okB := parseSemver(b)
	if !okA || !okB {
		return false
	}
	for i := range pa.core {
		if pa.core[i] != pb.core[i] {
			return pa.core[i] > pb.core[i]
		}
	}
	switch {
	case pa.pre == pb.pre:
		return false
	case pa.pre == "":
		return true
	case pb.pre == "":
		return false
	}
	return comparePre(pa.pre, pb.pre) > 0
}

// comparePre orders pre-release names as semver does: dot-separated parts,
// numbers by value and before words.
func comparePre(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, errA := strconv.Atoi(pa[i])
		nb, errB := strconv.Atoi(pb[i])
		switch {
		case errA == nil && errB == nil:
			if na != nb {
				return na - nb
			}
		case errA == nil:
			return -1
		case errB == nil:
			return 1
		default:
			if c := strings.Compare(pa[i], pb[i]); c != 0 {
				return c
			}
		}
	}
	return len(pa) - len(pb)
}

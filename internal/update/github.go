package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitHub reads Matinee's releases and the edge branch from GitHub's API,
// without signing in. GitHub allows 60 such requests an hour from one
// address; a check makes one or two.
type GitHub struct {
	// Base is the repository's API address.
	Base string
	HTTP *http.Client
}

// DefaultRepo is Matinee's repository.
const DefaultRepo = "https://api.github.com/repos/yeixio/matinee"

// EdgeBranch is the branch CI moves to each commit on main once its checks
// pass and its image is published, so it never points at a build without one.
const EdgeBranch = "edge"

type release struct {
	Tag       string    `json:"tag_name"`
	URL       string    `json:"html_url"`
	Published time.Time `json:"published_at"`
	Draft     bool      `json:"draft"`
	Pre       bool      `json:"prerelease"`
	Assets    []asset   `json:"assets"`
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

func (g *GitHub) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.Base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := g.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("GitHub asked Matinee to wait before looking again (%s)", resp.Status)
	default:
		return fmt.Errorf("GitHub answered %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// latestRelease is the newest release that isn't a pre-release.
func (g *GitHub) latestRelease(ctx context.Context) (release, error) {
	var r release
	err := g.get(ctx, "/releases/latest", &r)
	return r, err
}

// edge is the commit the edge branch points at, and when it was made.
func (g *GitHub) edge(ctx context.Context) (string, time.Time, error) {
	var b struct {
		Commit struct {
			SHA    string `json:"sha"`
			Commit struct {
				Committer struct {
					Date time.Time `json:"date"`
				} `json:"committer"`
			} `json:"commit"`
		} `json:"commit"`
	}
	if err := g.get(ctx, "/branches/"+EdgeBranch, &b); err != nil {
		return "", time.Time{}, err
	}
	return b.Commit.SHA, b.Commit.Commit.Committer.Date, nil
}

// ahead is how many commits head has that base doesn't, when head has all
// of base's: 0 when it's base itself, behind it, or off on another line.
func (g *GitHub) ahead(ctx context.Context, base, head string) (int, error) {
	var c struct {
		Status  string `json:"status"`
		AheadBy int    `json:"ahead_by"`
	}
	if err := g.get(ctx, "/compare/"+url.PathEscape(base)+"..."+url.PathEscape(head), &c); err != nil {
		return 0, err
	}
	if c.Status != "ahead" {
		return 0, nil
	}
	return c.AheadBy, nil
}

// webURL is the repository's page, from its API address.
func (g *GitHub) webURL() string {
	return strings.Replace(g.Base, "://api.github.com/repos/", "://github.com/", 1)
}

// Candidate is a newer version to update to.
type Candidate struct {
	Channel string `json:"channel"`
	// Version is how Settings shows it: v1.2.3, or main-<commit> for a build of main.
	Version string `json:"version"`
	// Revision is the git revision: the release's tag, or the commit.
	Revision string `json:"revision"`
	// ImageTag is its container image's tag.
	ImageTag string `json:"imageTag"`
	// URL is the release's notes, or the commits since this build.
	URL       string    `json:"url"`
	Published time.Time `json:"published,omitzero"`
	// Commits is how many commits main has since this build.
	Commits int `json:"commits,omitempty"`

	assets []asset
}

// Find looks for a version newer than b on the channel. It returns nil
// when there's none, and never offers a version older than b, so a build
// of main waits on the release channel for a release that has everything
// it has.
func (g *GitHub) Find(ctx context.Context, b Build, channel string) (*Candidate, error) {
	if !b.Known() {
		return nil, nil
	}
	switch channel {
	case ChannelMain:
		sha, when, err := g.edge(ctx)
		if err != nil {
			return nil, err
		}
		if sha == "" || strings.HasPrefix(sha, b.Commit) && b.Commit != "" {
			return nil, nil
		}
		n, err := g.ahead(ctx, b.Revision(), sha)
		if err != nil || n == 0 {
			return nil, err
		}
		return &Candidate{
			Channel: ChannelMain, Version: "main-" + short(sha), Revision: sha, ImageTag: "sha-" + short(sha),
			URL: g.webURL() + "/compare/" + b.Revision() + "..." + sha, Published: when, Commits: n,
		}, nil
	default:
		r, err := g.latestRelease(ctx)
		if err != nil {
			return nil, err
		}
		if r.Draft || r.Pre || r.Tag == "" {
			return nil, nil
		}
		if b.Release != "" {
			if !Newer(r.Tag, b.Release) {
				return nil, nil
			}
		} else if n, err := g.ahead(ctx, b.Commit, r.Tag); err != nil || n == 0 {
			return nil, err
		}
		return &Candidate{
			Channel: ChannelRelease, Version: "v" + strings.TrimPrefix(r.Tag, "v"), Revision: r.Tag,
			ImageTag: strings.TrimPrefix(r.Tag, "v"), URL: r.URL, Published: r.Published, assets: r.Assets,
		}, nil
	}
}

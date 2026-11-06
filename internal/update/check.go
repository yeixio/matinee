package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Channels: stable follows releases; beta follows the edge branch, which CI
// moves to each commit on main that passed every check and has its image
// published.
const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
)

// Release is a version this server could update to.
type Release struct {
	Version   string `json:"version"`             // v1.2.3 or main-abc1234
	URL       string `json:"url"`                 // the release page, or the changes on GitHub
	Notes     string `json:"notes,omitempty"`     // the release's notes (Markdown)
	Published int64  `json:"published,omitempty"` // Unix milliseconds
	// Commits are the changes since this version, newest first (beta only).
	Commits []Commit `json:"commits,omitempty"`
	// More counts changes beyond Commits.
	More int `json:"more,omitempty"`

	commit string // the full commit of a beta build
}

// Commit is one change on main.
type Commit struct {
	SHA   string `json:"sha"` // seven characters
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Checker asks GitHub for Matinee's releases and builds.
type Checker struct {
	API    string // https://api.github.com
	Web    string // https://github.com, for downloads
	Repo   string // yeixio/matinee
	Client *http.Client
	Agent  string // User-Agent
}

// NewChecker asks github.com about yeixio/matinee.
func NewChecker(version string) *Checker {
	return &Checker{
		API: "https://api.github.com", Web: "https://github.com", Repo: "yeixio/matinee",
		Client: &http.Client{Timeout: 30 * time.Second},
		Agent:  "Matinee/" + strings.TrimPrefix(version, "v") + " (+https://matinee.yeix.io)",
	}
}

// ErrUnknownVersion is a build that isn't a release or a build of main,
// such as one made by hand, which can't tell what it's behind.
var ErrUnknownVersion = errors.New("this build's version isn't a release, so it can't tell which versions are newer")

// Check finds the newest version on channel. latest is nil when current is
// up to date; ahead is whether current is newer than anything on the
// channel (a beta build on the stable channel).
func (c *Checker) Check(ctx context.Context, current, channel string) (latest *Release, ahead bool, err error) {
	if !isRelease(current) && !isMainBuild(current) {
		return nil, false, ErrUnknownVersion
	}
	if channel == ChannelBeta {
		return c.checkBeta(ctx, current)
	}
	return c.checkStable(ctx, current)
}

func (c *Checker) checkStable(ctx context.Context, current string) (*Release, bool, error) {
	var r struct {
		Tag       string    `json:"tag_name"`
		Name      string    `json:"name"`
		Page      string    `json:"html_url"`
		Body      string    `json:"body"`
		Published time.Time `json:"published_at"`
	}
	// releases/latest is the newest release that isn't a draft or a pre-release.
	if err := c.get(ctx, "/repos/"+c.Repo+"/releases/latest", &r); err != nil {
		return nil, false, err
	}
	rel := &Release{Version: Display(r.Tag), URL: r.Page, Notes: r.Body, Published: r.Published.UnixMilli()}
	if isRelease(current) {
		switch {
		case Newer(r.Tag, current):
			return rel, false, nil
		case Newer(current, r.Tag):
			return nil, true, nil
		}
		return nil, false, nil
	}
	// A build of main: the release is newer when it has commits this build doesn't.
	cmp, err := c.compare(ctx, mainCommit(current), r.Tag)
	if err != nil {
		return nil, false, err
	}
	switch cmp.Status {
	case "ahead", "diverged":
		return rel, false, nil
	case "behind":
		return nil, true, nil
	}
	return nil, false, nil
}

func (c *Checker) checkBeta(ctx context.Context, current string) (*Release, bool, error) {
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
	if err := c.get(ctx, "/repos/"+c.Repo+"/branches/edge", &b); err != nil {
		return nil, false, err
	}
	edge := b.Commit.SHA
	base := tag(current)
	if sha := mainCommit(current); sha != "" {
		if strings.HasPrefix(edge, sha) || strings.HasPrefix(sha, edge) {
			return nil, false, nil
		}
		base = sha
	}
	cmp, err := c.compare(ctx, base, edge)
	if err != nil {
		return nil, false, err
	}
	switch cmp.Status {
	case "identical":
		return nil, false, nil
	case "behind":
		return nil, true, nil
	}
	rel := &Release{Version: "main-" + short(edge), URL: cmp.Page, Published: b.Commit.Commit.Committer.Date.UnixMilli(), commit: edge}
	// GitHub lists the changes oldest first; show the newest 30.
	for i := len(cmp.Commits) - 1; i >= 0 && len(rel.Commits) < 30; i-- {
		cm := cmp.Commits[i]
		title, _, _ := strings.Cut(cm.Commit.Message, "\n")
		rel.Commits = append(rel.Commits, Commit{SHA: short(cm.SHA), Title: title, URL: cm.Page})
	}
	rel.More = max(cmp.AheadBy-len(rel.Commits), 0)
	return rel, false, nil
}

type comparison struct {
	Status  string `json:"status"` // ahead, behind, diverged or identical: head against base
	AheadBy int    `json:"ahead_by"`
	Page    string `json:"html_url"`
	Commits []struct {
		SHA    string `json:"sha"`
		Page   string `json:"html_url"`
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	} `json:"commits"`
}

func (c *Checker) compare(ctx context.Context, base, head string) (comparison, error) {
	var cmp comparison
	err := c.get(ctx, "/repos/"+c.Repo+"/compare/"+url.PathEscape(base)+"..."+url.PathEscape(head)+"?per_page=100", &cmp)
	return cmp, err
}

func (c *Checker) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.API+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", c.Agent)
	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return errors.New("GitHub is limiting how often this address may ask; Matinee tries again later")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("GitHub answered %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

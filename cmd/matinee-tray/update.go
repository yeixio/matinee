package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// releasesURL is the newest stable release. Pre-releases aren't offered.
const releasesURL = "https://api.github.com/repos/yeixio/matinee/releases/latest"

// Release is a newer Matinee with an installer for this computer.
type Release struct {
	Version      string // 0.1.8
	Page         string // the release's page, with its notes
	InstallerURL string
	Installer    string // the installer's file name
	SumsURL      string // SHA256SUMS.txt
}

// installerName is the installer the release workflow attaches for arch
// (x64 or arm64); scripts/package-windows.sh names it. It has no version, so
// releases/latest/download/<name> always links the newest.
func installerName(arch string) string {
	return fmt.Sprintf("Matinee-Setup-%s.exe", arch)
}

// LatestRelease looks up the newest release. ok is false when it isn't
// newer than current or has no installer for arch.
func LatestRelease(ctx context.Context, client *http.Client, url, current, arch string) (rel Release, ok bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return rel, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return rel, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return rel, false, fmt.Errorf("looking for updates: %s", resp.Status)
	}
	var r struct {
		Tag        string `json:"tag_name"`
		Page       string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return rel, false, err
	}
	rel = Release{Version: strings.TrimPrefix(r.Tag, "v"), Page: r.Page}
	if r.Draft || r.Prerelease || !Newer(rel.Version, current) {
		return rel, false, nil
	}
	rel.Installer = installerName(arch)
	for _, a := range r.Assets {
		switch a.Name {
		case rel.Installer:
			rel.InstallerURL = a.URL
		case "SHA256SUMS.txt":
			rel.SumsURL = a.URL
		}
	}
	return rel, rel.InstallerURL != "" && rel.SumsURL != "", nil
}

// Newer is whether version a comes after b. Both are like 1.2.3, with or
// without a v. A build that isn't a release (dev, or a commit) is never
// offered updates, since it can't tell what it's behind.
func Newer(a, b string) bool {
	pa, okA := parseVersion(a)
	pb, okB := parseVersion(b)
	if !okA || !okB {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	if strings.ContainsAny(v, "-+") {
		return out, false
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Download saves the release's installer in dir and checks it against the
// release's SHA256SUMS.txt, so a broken or swapped download isn't run.
func Download(ctx context.Context, client *http.Client, rel Release, dir string) (string, error) {
	want, err := expectedSum(ctx, client, rel.SumsURL, rel.Installer)
	if err != nil {
		return "", err
	}
	body, err := get(ctx, client, rel.InstallerURL)
	if err != nil {
		return "", err
	}
	defer body.Close()
	path := filepath.Join(dir, rel.Installer)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		os.Remove(path)
		return "", fmt.Errorf("the downloaded installer doesn't match the release's checksum")
	}
	return path, nil
}

func expectedSum(ctx context.Context, client *http.Client, url, name string) (string, error) {
	body, err := get(ctx, client, url)
	if err != nil {
		return "", err
	}
	defer body.Close()
	sc := bufio.NewScanner(body)
	for sc.Scan() {
		// sha256sum's lines: <hex>, two spaces (or space and *), file name.
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("the release's checksums don't list %s", name)
}

func get(ctx context.Context, client *http.Client, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

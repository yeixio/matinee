package update

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// archiveName is the release archive scripts/release-binaries.sh makes for
// this computer, such as matinee_1.2.3_linux_arm64.tar.gz.
func archiveName(version, goos, goarch, goarm string) string {
	arch := goarch
	if goarch == "arm" && goarm != "" {
		arch += "v" + goarm
	}
	return fmt.Sprintf("matinee_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), goos, arch)
}

// replaceBinary downloads the release's archive for this computer, checks it
// against the release's SHA256SUMS.txt, and puts its matinee in place of
// exe. The running server carries on until it restarts.
func replaceBinary(ctx context.Context, client *http.Client, c *Candidate, exe, goos, goarch, goarm string) error {
	name := archiveName(c.Version, goos, goarch, goarm)
	var archiveURL, sumsURL string
	for _, a := range c.assets {
		switch a.Name {
		case name:
			archiveURL = a.URL
		case "SHA256SUMS.txt":
			sumsURL = a.URL
		}
	}
	if archiveURL == "" || sumsURL == "" {
		return fmt.Errorf("%s has no %s to download", c.Version, name)
	}
	want, err := expectedSum(ctx, client, sumsURL, name)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".matinee-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	body, err := fetch(ctx, client, archiveURL)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmp, h), body)
	body.Close()
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return errors.New("the download doesn't match the release's checksum")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}

	// Unpack the new matinee beside the old one, then swap them in one rename.
	next := exe + ".new"
	if err := extract(tmp, next); err != nil {
		os.Remove(next)
		return err
	}
	if err := os.Rename(next, exe); err != nil {
		os.Remove(next)
		return err
	}
	return nil
}

func extract(archive io.Reader, to string) error {
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return errors.New("the download has no matinee in it")
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || filepath.Base(h.Name) != "matinee" {
			continue
		}
		f, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, io.LimitReader(tr, 1<<30))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	}
}

func expectedSum(ctx context.Context, client *http.Client, url, name string) (string, error) {
	body, err := fetch(ctx, client, url)
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

func fetch(ctx context.Context, client *http.Client, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = http.DefaultClient
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

package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// archiveName is the release archive for this computer, as
// scripts/release-binaries.sh names it.
func archiveName(version, goos, goarch, goarm string) string {
	arch := goarch
	if goarch == "arm" {
		arch += "v" + goarm
	}
	return fmt.Sprintf("matinee_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), goos, arch)
}

func goarm() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "GOARM" {
				return strings.TrimSuffix(strings.TrimSuffix(s.Value, ",softfloat"), ",hardfloat")
			}
		}
	}
	return "7"
}

// maxBinary is far more than the server's executable will ever be.
const maxBinary = 512 << 20

// installBinary downloads version's archive for this computer, checks it
// against the release's SHA256SUMS.txt, and puts its matinee in place of
// exe. The running server keeps going until it's restarted.
func (c *Checker) installBinary(ctx context.Context, version, exe string) error {
	name := archiveName(version, runtime.GOOS, runtime.GOARCH, goarm())
	base := c.Web + "/" + c.Repo + "/releases/download/" + tag(version) + "/"
	sums, err := c.download(ctx, base+"SHA256SUMS.txt", 1<<20)
	if err != nil {
		return fmt.Errorf("download the checksums: %w", err)
	}
	want := ""
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			want = f[0]
		}
	}
	if want == "" {
		return fmt.Errorf("the release has no %s for this computer", name)
	}
	archive, err := c.download(ctx, base+name, maxBinary)
	if err != nil {
		return fmt.Errorf("download %s: %w", name, err)
	}
	if got := sha256.Sum256(archive); hex.EncodeToString(got[:]) != want {
		return errors.New("the download doesn't match the release's checksum, so it wasn't installed")
	}
	bin, err := extract(archive, "matinee")
	if err != nil {
		return err
	}
	// Written beside the executable and renamed over it, so a failure never
	// leaves half a server; the running one keeps its open copy.
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".matinee-update-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	mode := os.FileMode(0o755)
	if st, err := os.Stat(exe); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), exe)
}

// extract is the file called name in a .tar.gz.
func extract(archive []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("the archive has no %s", name)
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == name {
			return io.ReadAll(io.LimitReader(tr, maxBinary))
		}
	}
}

func (c *Checker) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.Agent)
	// Downloads can take a while; the context bounds them, not the client.
	client := *c.Client
	client.Timeout = 0
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = errors.New("the download is too big")
	}
	return b, err
}

// upgradeHomebrew runs brew upgrade matinee as the user the service runs as,
// who installed it.
func upgradeHomebrew(ctx context.Context, prefix string) error {
	cmd := exec.CommandContext(ctx, filepath.Join(prefix, "bin", "brew"), "upgrade", "matinee")
	// A service has a bare PATH; brew needs git and curl.
	cmd.Env = append(os.Environ(), "PATH="+filepath.Join(prefix, "bin")+":/usr/bin:/bin:/usr/sbin:/sbin", "HOMEBREW_NO_ENV_HINTS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("brew upgrade matinee: %w: %s", err, lastLines(string(out), 5))
	}
	return nil
}

// requestPackage asks the packages' helper, which runs as root, to upgrade
// the matinee package, and waits for it. Once the new package is in, its
// install script restarts the server, so a wait that ends is a failure.
func requestPackage(ctx context.Context, request, logPath string) error {
	if err := os.MkdirAll(filepath.Dir(request), 0o750); err != nil {
		return err
	}
	asked := time.Now()
	if err := os.WriteFile(request, []byte(asked.UTC().Format(time.RFC3339)+"\n"), 0o640); err != nil {
		return err
	}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			os.Remove(request)
			return errors.New("the package manager didn't finish in time; see " + logPath)
		case <-t.C:
		}
		st, err := os.Stat(logPath)
		if err != nil || st.ModTime().Before(asked) {
			continue
		}
		b, err := os.ReadFile(logPath)
		if err != nil {
			continue
		}
		switch result := packageResult(string(b)); result {
		case "", "ok":
			// Still going, or done and about to be restarted.
		case "unchanged":
			return errors.New("the package manager has no newer matinee package yet; try again later")
		default:
			return fmt.Errorf("the package manager couldn't upgrade matinee: %s (see %s)", lastLines(string(b), 3), logPath)
		}
	}
}

// packageResult is the helper's last "result:" line (ok, unchanged or
// failed), or empty while it's still going.
func packageResult(log string) string {
	result := ""
	for _, line := range strings.Split(log, "\n") {
		if r, ok := strings.CutPrefix(strings.TrimSpace(line), "result:"); ok {
			result = strings.TrimSpace(r)
		}
	}
	return result
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " / ")
}

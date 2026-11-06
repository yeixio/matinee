package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// upgradeHomebrew runs brew upgrade matinee as the user the service runs
// as, who installed it.
func upgradeHomebrew(ctx context.Context, prefix string) error {
	cmd := exec.CommandContext(ctx, filepath.Join(prefix, "bin", "brew"), "upgrade", "matinee")
	// A service has a bare PATH, and brew needs git and curl.
	cmd.Env = append(os.Environ(), "PATH="+filepath.Join(prefix, "bin")+":/usr/bin:/bin:/usr/sbin:/sbin", "HOMEBREW_NO_ENV_HINTS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("brew upgrade matinee: %w: %s", err, lastLines(string(out), 5))
	}
	return nil
}

// requestPackage asks the packages' root helper to upgrade the matinee
// package, and waits for it. Once the new package is in, its install
// script restarts the server, so a wait that ends is a failure.
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
		switch packageResult(string(b)) {
		case "", "ok":
			// Still going, or done and about to restart the server.
		case "unchanged":
			return errors.New("the package repository doesn't have the new version yet; try again later")
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

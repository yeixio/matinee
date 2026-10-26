//go:build unix

package hwaccel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeviceProblem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "renderD128")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if p := deviceProblem(path); p != "" {
		t.Fatalf("openable device: %q", p)
	}
	if os.Geteuid() == 0 {
		t.Skip("root opens anything")
	}
	os.Chmod(path, 0)
	if p := deviceProblem(path); !strings.Contains(p, "gpu.vaapi.groups") || !strings.Contains(p, "group ") {
		t.Fatalf("unopenable device: %q", p)
	}
}

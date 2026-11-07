//go:build unix

package hwaccel

import (
	"os"
	"path/filepath"
	"slices"
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

// Quick Sync is only tried on Intel GPUs: an AMD-only host has nothing to try.
func TestIntelNodes(t *testing.T) {
	dir := t.TempDir()
	old := sysDRM
	sysDRM = dir
	t.Cleanup(func() { sysDRM = old })
	for node, vendor := range map[string]string{"renderD128": "0x1002\n", "renderD129": "0x8086\n"} {
		if err := os.MkdirAll(filepath.Join(dir, node, "device"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, node, "device", "vendor"), []byte(vendor), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := intelNodes([]string{"/dev/dri/renderD128", "/dev/dri/renderD129", "/dev/dri/renderD130"})
	want := []string{"/dev/dri/renderD129", "/dev/dri/renderD130"} // 130's maker is unknown, so it's kept
	if !slices.Equal(got, want) {
		t.Errorf("intelNodes = %v, want %v", got, want)
	}
	if got := intelNodes([]string{"/dev/dri/renderD128"}); len(got) != 0 {
		t.Errorf("AMD only: %v, want none", got)
	}
}

package diagnose

import (
	"testing"
	"time"
)

func TestExplain(t *testing.T) {
	for _, c := range []struct {
		name string
		f    Facts
		code string
		tip  string
		vals map[string]string
	}{
		{"slow connection", Facts{DownloadBps: 4_000_000, NeededBps: 25_000_000}, CauseSlowConnection, "adaptive", map[string]string{"have": "4.0", "need": "25"}},
		{"slow connection, adaptive on", Facts{DownloadBps: 4_000_000, NeededBps: 25_000_000, Adaptive: true}, CauseSlowConnection, "", nil},
		{"fast enough connection", Facts{DownloadBps: 40_000_000, NeededBps: 25_000_000}, CauseUnknown, "", nil},
		{"slow conversion on the cpu", Facts{Converting: true, Why: "codec", Speed: 0.7, DownloadBps: 1_000_000, NeededBps: 8_000_000}, CauseSlowConvert, "gpu", map[string]string{"speed": "0.7"}},
		{"slow conversion on a gpu", Facts{Converting: true, Speed: 0.9, GPU: true}, CauseSlowConvert, "lowerQuality", nil},
		{"conversion keeping up", Facts{Converting: true, Speed: 3}, CauseUnknown, "", nil},
		{"slow share beats everything", Facts{ReadMs: 2500, Converting: true, Speed: 0.5, DownloadBps: 1}, CauseSlowDisk, "disk", map[string]string{"seconds": "2.5"}},
		{"share slower than the file", Facts{ReadMs: 300, ReadBps: 10_000_000, NeededBps: 40_000_000}, CauseSlowDisk, "disk", nil},
		{"nothing known", Facts{}, CauseUnknown, "", nil},
	} {
		got := Explain(c.f)
		if got.Code != c.code || got.Tip != c.tip {
			t.Errorf("%s: got %s/%s, want %s/%s", c.name, got.Code, got.Tip, c.code, c.tip)
		}
		for k, v := range c.vals {
			if got.Values[k] != v {
				t.Errorf("%s: %s = %q, want %q", c.name, k, got.Values[k], v)
			}
		}
	}
	if c := Explain(Facts{Converting: true, Why: "hdr"}); c.Converted != "hdr" {
		t.Errorf("converted: %+v", c)
	}
}

func TestLog(t *testing.T) {
	now := time.Unix(1000, 0)
	l := &Log{now: func() time.Time { return now }}
	l.Add(1, 10, Cause{Code: CauseSlowDisk})
	if c, ok := l.Latest(1, 10, time.Minute); !ok || c.Code != CauseSlowDisk || c.At != now.UnixMilli() {
		t.Fatalf("latest: %+v %v", c, ok)
	}
	if _, ok := l.Latest(2, 10, 0); ok {
		t.Fatal("another person's")
	}
	now = now.Add(2 * time.Minute)
	if _, ok := l.Latest(1, 10, time.Minute); ok {
		t.Fatal("too old for a minute")
	}
	if _, ok := l.Latest(1, 10, 0); !ok {
		t.Fatal("kept an hour")
	}
	now = now.Add(2 * time.Hour)
	l.Add(3, 30, Cause{})
	if len(l.causes) != 1 {
		t.Fatalf("old ones kept: %v", l.causes)
	}
}

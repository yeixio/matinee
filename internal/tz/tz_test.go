package tz

import (
	"context"
	"testing"
	"time"
)

type settings map[string]string

func (s settings) Setting(_ context.Context, key string) (string, error) { return s[key], nil }

func TestLoad(t *testing.T) {
	local := time.Local
	t.Cleanup(func() { time.Local, inUse = local, "" })
	ctx := context.Background()
	if err := Load(ctx, settings{}); err != nil || time.Local != local || InUse() != System() {
		t.Fatalf("unset: %v %v %q", err, time.Local, InUse())
	}
	if err := Load(ctx, settings{Setting: "Mars/Olympus_Mons"}); err != nil || time.Local != local {
		t.Fatalf("unknown zone: %v %v", err, time.Local)
	}
	if err := Load(ctx, settings{Setting: "America/Juneau"}); err != nil || InUse() != "America/Juneau" {
		t.Fatalf("Juneau: %v %q", err, InUse())
	}
	if name, _ := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local).Zone(); name != "AKST" {
		t.Errorf("zone %s", name)
	}
}

func TestValid(t *testing.T) {
	for name, want := range map[string]bool{"": true, "America/Juneau": true, "UTC": true, "Local": false, "Mars/Olympus_Mons": false} {
		if Valid(name) != want {
			t.Errorf("%q: want %v", name, want)
		}
	}
}

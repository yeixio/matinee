package health

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func logTo(t *testing.T, dir string, at time.Time) (*Logs, *Runs, *slog.Logger) {
	t.Helper()
	runs, err := StartRuns(dir, "v1.2.3", at)
	if err != nil {
		t.Fatal(err)
	}
	logs := NewLogs(10)
	logs.Keep(runs)
	return logs, runs, slog.New(logs.Handler(slog.DiscardHandler))
}

func TestRunsKeepEarlierRuns(t *testing.T) {
	dir := t.TempDir()
	t0 := time.UnixMilli(1_700_000_000_000)

	// A run that stopped cleanly.
	_, first, log := logTo(t, dir, t0)
	log.Info("matinee started")
	log.Warn("slow disk", "path", "/media")
	first.Close(true)

	// A run that was killed: it never closes.
	_, second, log := logTo(t, dir, t0.Add(time.Hour))
	log.Info("matinee started")
	log.Error("database went away", "err", "connection refused")
	second.mu.Lock()
	second.f.Close()
	second.f = nil
	second.mu.Unlock()

	// This run.
	_, runs, log := logTo(t, dir, t0.Add(2*time.Hour))
	log.Info("matinee started")
	defer runs.Close(true)

	list := runs.List()
	if len(list) != 3 {
		t.Fatalf("runs = %+v", list)
	}
	if !list[0].Current || list[0].ID != runs.ID() || list[0].Lines != 1 {
		t.Errorf("current = %+v", list[0])
	}
	if list[1].Clean || list[1].Errors != 1 || list[1].Lines != 2 || list[1].Version != "v1.2.3" {
		t.Errorf("killed run = %+v", list[1])
	}
	if !list[2].Clean || list[2].Warnings != 1 || list[2].Ended == 0 {
		t.Errorf("clean run = %+v", list[2])
	}

	lines, more, err := runs.Lines(second.ID(), Query{})
	if err != nil || more || len(lines) != 2 || lines[0].Message != "database went away" {
		t.Fatalf("lines = %+v %v %v", lines, more, err)
	}
	lines, _, _ = runs.Lines(second.ID(), Query{Level: "error"})
	if len(lines) != 1 || lines[0].Attrs[0].Value != "connection refused" {
		t.Errorf("errors = %+v", lines)
	}
	lines, more, _ = runs.Lines(second.ID(), Query{Limit: 1})
	if len(lines) != 1 || !more {
		t.Errorf("paged = %+v %v", lines, more)
	}
	lines, _, _ = runs.Lines(second.ID(), Query{Before: lines[0].Seq})
	if len(lines) != 1 || lines[0].Message != "matinee started" {
		t.Errorf("older = %+v", lines)
	}
	if _, _, err := runs.Lines("../etc", Query{}); err == nil {
		t.Error("read outside the folder")
	}
}

func TestRunsPrune(t *testing.T) {
	dir := t.TempDir()
	t0 := time.UnixMilli(1_700_000_000_000)
	var runs *Runs
	for i := range KeepRuns + 3 {
		var err error
		runs, err = StartRuns(dir, "", t0.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if i < KeepRuns+2 {
			runs.Close(true)
		}
	}
	defer runs.Close(true)
	list := runs.List()
	if len(list) != KeepRuns || list[0].ID != runs.ID() {
		t.Fatalf("kept %d runs, newest %s", len(list), list[0].ID)
	}
	if runs.Has(strconv.FormatInt(t0.UnixMilli(), 10)) {
		t.Error("the oldest run is still kept")
	}
}

func TestRunsRotate(t *testing.T) {
	dir := t.TempDir()
	logs, runs, log := logTo(t, dir, time.Now())
	defer runs.Close(true)
	big := strings.Repeat("x", 1500) // under maxValue, so it's kept whole
	for range MaxRunBytes / 1500 * 3 {
		log.Info("line", "v", big)
	}
	_ = logs
	for _, name := range []string{runs.ID() + ".log", runs.ID() + ".old.log"} {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil || st.Size() > MaxRunBytes {
			t.Errorf("%s: %v %v", name, st, err)
		}
	}
}

// A real crash: a child process panics, and the panic is in its run's
// .crash file afterwards.
func TestRunsKeepCrash(t *testing.T) {
	if dir := os.Getenv("MATINEE_TEST_CRASH_DIR"); dir != "" {
		_, _, log := logTo(t, dir, time.Now())
		log.Info("about to crash")
		go func() { panic("something broke") }()
		time.Sleep(time.Second)
		return
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunsKeepCrash$")
	cmd.Env = append(os.Environ(), "MATINEE_TEST_CRASH_DIR="+dir)
	if err := cmd.Run(); err == nil {
		t.Fatal("the child didn't crash")
	}
	runs, err := StartRuns(dir, "", time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer runs.Close(true)
	list := runs.List()
	if len(list) != 2 || !list[1].Crashed || list[1].Clean || list[1].Lines != 1 {
		t.Fatalf("runs = %+v", list)
	}
	if c := runs.Crash(list[1].ID); !strings.Contains(c, "panic: something broke") {
		t.Errorf("crash = %q", c)
	}
}

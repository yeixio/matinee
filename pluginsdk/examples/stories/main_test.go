package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	plugin "github.com/yeixio/matinee/pluginsdk"
	"github.com/yeixio/matinee/pluginsdk/plugintest"
)

func indexFile(t *testing.T, root, rel string) *plugin.Item {
	t.Helper()
	res, err := index(plugin.IndexRequest{Root: root, Path: filepath.Join(root, rel)})
	if err != nil {
		t.Fatal(err)
	}
	return res.Item
}

func TestIndex(t *testing.T) {
	root := "testdata/library"

	wind := indexFile(t, root, "the-north-wind-and-the-sun.md")
	if wind == nil || wind.Kind != "story" || wind.Title != "The North Wind and the Sun" || len(wind.Parents) != 0 {
		t.Fatalf("got %+v", wind)
	}
	if want := "The North Wind and the Sun were disputing which was the stronger, when a traveler came along wrapped in a warm cloak."; wind.Overview != want {
		t.Errorf("overview %q", wind.Overview)
	}
	if !slices.Contains(wind.Facts, plugin.Fact{Label: "Reading time", Value: "1 min"}) || wind.Data["markdown"] != true {
		t.Errorf("facts %v, data %v", wind.Facts, wind.Data)
	}

	hare := indexFile(t, root, "Aesop's Fables/the-tortoise-and-the-hare.txt")
	if hare.Title != "The Tortoise and the Hare" || len(hare.Parents) != 1 || hare.Parents[0].Title != "Aesop's Fables" || hare.Parents[0].Kind != "book" {
		t.Fatalf("got %+v", hare)
	}

	if it := indexFile(t, root, "empty.txt"); it != nil {
		t.Errorf("an empty file was filed: %+v", it)
	}
}

func TestCover(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Fables")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("A story\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "cover.png"), []byte("png"), 0o644)
	it := indexFile(t, root, "Fables/a.txt")
	if it.Parents[0].Poster != filepath.Join(dir, "cover.png") {
		t.Errorf("poster %q", it.Parents[0].Poster)
	}
}

func TestEvent(t *testing.T) {
	log := plugintest.Logs(t)
	err := event(plugin.Event{
		Type:   "playback.stopped",
		Item:   &plugin.EventItem{Title: "The Fox and the Grapes"},
		User:   &plugin.EventUser{Name: "Sam"},
		Played: &plugin.EventPlay{Finished: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(log.Lines(), []string{`Sam finished "The Fox and the Grapes"`}) {
		t.Errorf("logged %v", log.Lines())
	}
}

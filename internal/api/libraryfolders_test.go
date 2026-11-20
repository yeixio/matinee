package api

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

// One library can have several folders: a show split across two disks is
// one show, folders are added and taken out of a library, and two
// libraries made by mistake for the same kind of media become one.
func TestLibraryFolders(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	if code := c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login); code != 200 {
		t.Fatalf("setup: %d", code)
	}
	c.token = login.Token
	episode := func(disk, show string, n string) {
		makeMedia(t, filepath.Join(tmp, disk, show, "Season 1", show+" - S01E0"+n+".mkv"),
			"-f", "lavfi", "-i", "testsrc=duration=1:size=64x64:rate=5", "-c:v", "mpeg4")
	}
	episode("disk1", "Split Show", "1")
	episode("disk2", "Split Show", "2")
	episode("disk3", "Split Show", "3")
	episode("disk3", "Other Show", "1")
	disk := func(n string) string { return filepath.Join(tmp, "disk"+n) }

	episodes := func(libID int64) map[string]int {
		t.Helper()
		var shows []store.Item
		c.do("GET", "/api/v1/libraries/"+itoa(libID)+"/items", nil, &shows)
		out := map[string]int{}
		for _, sh := range shows {
			var seasons []store.Item
			c.do("GET", "/api/v1/items/"+itoa(sh.ID)+"/children", nil, &seasons)
			if len(seasons) != 1 {
				t.Fatalf("%s seasons: %+v", sh.Title, seasons)
			}
			var eps []store.Item
			c.do("GET", "/api/v1/items/"+itoa(seasons[0].ID)+"/children", nil, &eps)
			if _, dup := out[sh.Title]; dup {
				t.Fatalf("%s is two shows", sh.Title)
			}
			out[sh.Title] = len(eps)
		}
		return out
	}

	// Two folders in one library: one show.
	var tv libraryView
	if code := c.do("POST", "/api/v1/libraries", map[string]any{"name": "TV", "kind": "tv", "paths": []string{disk("1"), disk("2")}}, &tv); code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	waitForScan(t, c)
	if got := episodes(tv.ID); len(got) != 1 || got["Split Show"] != 2 {
		t.Fatalf("two folders: %v", got)
	}

	// A folder already in a library can't be another library's too.
	if code := c.do("POST", "/api/v1/libraries", map[string]any{"name": "TV 2", "kind": "tv", "paths": []string{disk("2")}}, nil); code != http.StatusConflict {
		t.Fatalf("same folder twice: %d", code)
	}
	// Nor can a folder inside another of the same library.
	if code := c.do("PUT", "/api/v1/libraries/"+itoa(tv.ID), map[string]any{"paths": []string{disk("1"), filepath.Join(disk("1"), "Split Show")}}, nil); code != http.StatusBadRequest {
		t.Fatalf("folder inside a folder: %d", code)
	}

	// Taking a folder out takes its episodes with it; the rename sticks.
	var lib libraryView
	if code := c.do("PUT", "/api/v1/libraries/"+itoa(tv.ID), map[string]any{"name": "Shows", "paths": []string{disk("1")}}, &lib); code != 200 || lib.Name != "Shows" || len(lib.Paths) != 1 {
		t.Fatalf("remove folder: %d %+v", code, lib)
	}
	waitForScan(t, c)
	if got := episodes(tv.ID); got["Split Show"] != 1 {
		t.Fatalf("after removing a folder: %v", got)
	}
	// Adding it back brings them back.
	if code := c.do("PUT", "/api/v1/libraries/"+itoa(tv.ID), map[string]any{"paths": []string{disk("1"), disk("2")}}, &lib); code != 200 || len(lib.Paths) != 2 {
		t.Fatalf("add folder: %d %+v", code, lib)
	}
	waitForScan(t, c)
	if got := episodes(tv.ID); got["Split Show"] != 2 {
		t.Fatalf("after adding a folder: %v", got)
	}

	// A second library made for the third disk, as adding a folder used to
	// need, with someone's watch list entry for its copy of the show.
	var extra libraryView
	if code := c.do("POST", "/api/v1/libraries", map[string]any{"name": "TV (disk 3)", "kind": "tv", "paths": []string{disk("3")}}, &extra); code != http.StatusCreated {
		t.Fatalf("create second: %d", code)
	}
	waitForScan(t, c)
	if got := episodes(extra.ID); got["Split Show"] != 1 || got["Other Show"] != 1 {
		t.Fatalf("second library: %v", got)
	}
	var extraShows []store.Item
	c.do("GET", "/api/v1/libraries/"+itoa(extra.ID)+"/items", nil, &extraShows)
	for _, sh := range extraShows {
		if sh.Title == "Split Show" {
			if code := c.do("PUT", "/api/v1/watchlist/"+itoa(sh.ID), nil, nil); code >= 300 {
				t.Fatalf("watch list: %d", code)
			}
		}
	}

	// Only the same type of library can be combined.
	var movies libraryView
	os.MkdirAll(filepath.Join(tmp, "movies"), 0o755)
	if code := c.do("POST", "/api/v1/libraries", map[string]any{"name": "Movies", "kind": "movies", "paths": []string{filepath.Join(tmp, "movies")}}, &movies); code != http.StatusCreated {
		t.Fatalf("create movies: %d", code)
	}
	waitForScan(t, c)
	if code := c.do("POST", "/api/v1/libraries/"+itoa(tv.ID)+"/merge", map[string]any{"from": movies.ID}, nil); code != http.StatusBadRequest {
		t.Fatalf("merge movies into TV: %d", code)
	}
	if code := c.do("POST", "/api/v1/libraries/"+itoa(tv.ID)+"/merge", map[string]any{"from": extra.ID}, &lib); code != 200 || len(lib.Paths) != 3 {
		t.Fatalf("merge: %d %+v", code, lib)
	}
	waitForScan(t, c)
	if got := episodes(tv.ID); len(got) != 2 || got["Split Show"] != 3 || got["Other Show"] != 1 {
		t.Fatalf("merged: %v", got)
	}
	var libs []libraryView
	c.do("GET", "/api/v1/libraries", nil, &libs)
	for _, l := range libs {
		if l.ID == extra.ID {
			t.Fatal("the merged library is still there")
		}
	}
	var list []store.Item
	c.do("GET", "/api/v1/watchlist", nil, &list)
	if len(list) != 1 || list[0].Title != "Split Show" || list[0].LibraryID != tv.ID {
		t.Fatalf("watch list after merge: %+v", list)
	}
}

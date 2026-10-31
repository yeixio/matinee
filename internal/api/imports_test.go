package api

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/importer/importertest"
	"github.com/yeixio/matinee/internal/store"
)

// An admin points Matinee at another server's data folder, takes the
// suggested path change, maps accounts, and runs the import.
func TestImportFromAnotherServer(t *testing.T) {
	c, tmp, db := newTestServerStore(t)
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"Mike", "correct horse"}, &login)
	c.token = login.Token
	ctx := context.Background()
	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/mnt/media/movies"})
	movie, _ := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Heat", Path: "/mnt/media/movies/Heat.mkv"})
	db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Ronin", Path: "/mnt/media/movies/Ronin.mkv"})

	src := importertest.New(t, filepath.Join(tmp, "other"))
	src.Account(1, "mike")
	heat := src.Item(1, 0, "Heat", "heat", "/data/movies/Heat.mkv")
	src.Item(1, 0, "Ronin", "ronin", "/data/movies/Ronin.mkv")
	src.View(1, "heat", 0, 1, time.Now().Unix()-3600)
	src.View(1, "ronin", 90000, 0, time.Now().Unix()-3600)
	src.Collection("Favourites", heat)
	src.Close()

	var preview struct {
		Database   string
		CanPosters bool
		Accounts   []struct {
			ID                         int64
			Name                       string
			Watched, InProgress, Found int
			Suggested                  int64
		}
		Viewers   []struct{ ID int64 }
		Files     int
		Matched   int
		Suggested []struct {
			From, To string
			Files    int
		}
		Collections int
	}
	if code := c.do("POST", "/api/v1/import/preview", map[string]any{"path": tmp}, &preview); code != 200 {
		t.Fatalf("preview: %d", code)
	}
	if !preview.CanPosters || preview.Files != 2 || preview.Matched != 0 || len(preview.Suggested) != 1 ||
		preview.Suggested[0].From != "/data" || preview.Suggested[0].To != "/mnt/media" {
		t.Fatalf("preview: %+v", preview)
	}
	if len(preview.Accounts) != 1 || preview.Accounts[0].Suggested == 0 || preview.Accounts[0].Watched != 1 || len(preview.Viewers) == 0 {
		t.Fatalf("accounts: %+v", preview)
	}
	changes := []map[string]string{{"from": "/data", "to": "/mnt/media"}}
	if code := c.do("POST", "/api/v1/import/preview", map[string]any{"path": tmp, "changes": changes}, &preview); code != 200 || preview.Matched != 2 || preview.Collections != 1 {
		t.Fatalf("with the change: %d %+v", code, preview)
	}
	me := preview.Accounts[0].Suggested

	if code := c.do("POST", "/api/v1/import/run", map[string]any{"path": tmp, "changes": changes}, nil); code != http.StatusBadRequest {
		t.Fatalf("nothing picked: %d", code)
	}
	run := map[string]any{"path": tmp, "changes": changes, "accounts": map[string]int64{"1": me}, "watched": true, "collections": true}
	if code := c.do("POST", "/api/v1/import/run", run, nil); code != http.StatusAccepted {
		t.Fatalf("run: %d", code)
	}
	var status struct {
		Running bool
		Result  *struct{ Watched, InProgress, Collections int }
		Error   string
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		c.do("GET", "/api/v1/import", nil, &status)
		if !status.Running && (status.Result != nil || status.Error != "") {
			break
		}
	}
	if status.Result == nil || status.Result.Watched != 1 || status.Result.InProgress != 1 || status.Result.Collections != 1 {
		t.Fatalf("status: %+v", status)
	}
	if it, _ := db.Item(ctx, me, movie); !it.Played {
		t.Fatal("not marked watched")
	}

	// The same database, uploaded instead.
	raw, _ := os.ReadFile(filepath.Join(src.Dir, "Plug-in Support", "Databases", "com.plexapp.plugins.library.db"))
	req, _ := http.NewRequest("POST", c.base+"/api/v1/import/upload", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != http.StatusNoContent {
		t.Fatalf("upload: %v %v", res.StatusCode, err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(uploadedDB())) })
	if code := c.do("POST", "/api/v1/import/preview", map[string]any{"changes": changes}, &preview); code != 200 || preview.CanPosters || preview.Matched != 2 {
		t.Fatalf("uploaded: %d %+v", code, preview)
	}
	req, _ = http.NewRequest("POST", c.base+"/api/v1/import/upload", bytes.NewReader([]byte("not a database")))
	req.Header.Set("Authorization", "Bearer "+c.token)
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("junk upload: %d", res.StatusCode)
	}

	var bad struct{ Code string }
	if code := c.do("POST", "/api/v1/import/preview", map[string]any{"path": t.TempDir()}, &bad); code != 400 || bad.Code != "no_media_server_data_found_there" {
		t.Fatalf("empty folder: %d %+v", code, bad)
	}
}

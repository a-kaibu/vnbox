package webplay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-kaibu/vnbox/internal/pkg"
)

// setup makes a vnbox root with a Siglus package "sig" (browser) and "wine" (Wine only),
// and a player directory, and returns the handler.
func setup(t *testing.T) http.Handler {
	root := t.TempDir()
	t.Setenv("VNBOX", root)
	game := filepath.Join(root, "packages", "sig", "files", "Games", "sig", "GameData")
	os.MkdirAll(filepath.Join(game, "g00"), 0o755)
	os.WriteFile(filepath.Join(game, "Scene.pck"), []byte("0123456789"), 0o644)
	os.WriteFile(filepath.Join(game, "g00", "bg.g00"), []byte("bg"), 0o644)
	pkg.WriteLaunch(filepath.Join(root, "packages", "sig"), pkg.Launch{
		Target: "Games/sig/GameData/SiglusEngine.exe", Title: "シグ 体験版", Engine: "SiglusEngine",
		Runner: pkg.RunnerVnrun, Root: "Games/sig/GameData",
	})
	os.MkdirAll(filepath.Join(root, "packages", "wine"), 0o755)
	pkg.WriteLaunch(filepath.Join(root, "packages", "wine"), pkg.Launch{Target: "Games/w/w.exe", Engine: "SiglusEngine"})
	player := filepath.Join(root, "player")
	os.MkdirAll(player, 0o755)
	os.WriteFile(filepath.Join(player, "index.html"), []byte("<title>vnrun</title>"), 0o644)
	mux := http.NewServeMux()
	New(Options{PlayerDir: player}).Register(mux)
	return mux
}

func do(h http.Handler, method, path, body string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestManifestAndFiles(t *testing.T) {
	h := setup(t)
	rec := do(h, "GET", "/p/sig/manifest.json", "")
	var m manifest
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil || rec.Code != 200 {
		t.Fatalf("manifest: %d %v %s", rec.Code, err, rec.Body)
	}
	if m.Engine != "siglus" || m.Title != "シグ 体験版" || len(m.Files) != 2 {
		t.Fatalf("manifest: %+v", m)
	}
	if rec := do(h, "GET", "/p/sig/files/Scene.pck", "", "Range", "bytes=2-4"); rec.Code != http.StatusPartialContent || rec.Body.String() != "234" {
		t.Fatalf("range: %d %q", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/p/sig/files/g00/bg.g00", ""); rec.Code != 200 || rec.Body.String() != "bg" {
		t.Fatalf("file: %d %q", rec.Code, rec.Body)
	}
	// nothing outside the game folder, and nothing from packages that are not for the browser
	for _, p := range []string{"/p/sig/files/../../../launch.ini", "/p/sig/files/..%2F..%2Flaunch.ini", "/p/wine/manifest.json", "/p/nope/manifest.json", "/p/sig/files/missing"} {
		if rec := do(h, "GET", p, ""); rec.Code == 200 {
			t.Errorf("%s: served %q", p, rec.Body)
		}
	}
	if rec := do(h, "GET", "/player/", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "vnrun") {
		t.Fatalf("player: %d", rec.Code)
	}
}

func TestSaves(t *testing.T) {
	h := setup(t)
	if rec := do(h, "GET", "/p/sig/saves/", ""); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty list: %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PUT", "/p/sig/saves/savedata/save001.sav", "SAVE"); rec.Code != http.StatusNoContent {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(SaveDir("sig"), "savedata", "save001.sav")); string(b) != "SAVE" {
		t.Fatalf("stored %q", b)
	}
	var list []entry
	json.Unmarshal(do(h, "GET", "/p/sig/saves/", "").Body.Bytes(), &list)
	if len(list) != 1 || list[0].P != "savedata/save001.sav" || list[0].S != 4 || list[0].M == 0 {
		t.Fatalf("list: %+v", list)
	}
	rec := do(h, "GET", "/p/sig/saves/savedata/save001.sav", "")
	if b, _ := io.ReadAll(rec.Body); string(b) != "SAVE" {
		t.Fatalf("get: %q", b)
	}
	if rec := do(h, "PUT", "/p/sig/saves/../escape", "x"); rec.Code == http.StatusNoContent {
		t.Fatal("put outside the save folder")
	}
	if rec := do(h, "DELETE", "/p/sig/saves/savedata/save001.sav", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := do(h, "DELETE", "/p/sig/saves/savedata/save001.sav", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("delete again: %d", rec.Code)
	}
}

func TestPlayable(t *testing.T) {
	dir := t.TempDir()
	if Playable("SiglusEngine", dir) {
		t.Fatal("no Scene.pck")
	}
	os.WriteFile(filepath.Join(dir, "SCENE.PCK"), nil, 0o644)
	if !Playable("SiglusEngine", dir) || Playable("KiriKiri", dir) {
		t.Fatal("engine check")
	}
}

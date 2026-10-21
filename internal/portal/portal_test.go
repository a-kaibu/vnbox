package portal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-kaibu/vnbox/internal/meta"
	"github.com/a-kaibu/vnbox/internal/pkg"
	"github.com/a-kaibu/vnbox/internal/session"
)

func TestAuth(t *testing.T) {
	t.Setenv("VNBOX", t.TempDir())
	h := New("s3cret", session.Options{}).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/?token=wrong", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/?token=s3cret", nil))
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "token") {
		t.Fatalf("token login: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	cookie := rec.Result().Cookies()[0]
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "vnbox") {
		t.Fatalf("with cookie: %d", rec.Code)
	}
	// a stream of a game that is not running redirects to the index instead of exposing anything
	req = httptest.NewRequest("GET", "/g/none/video", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("stopped game: %d", rec.Code)
	}
}

func TestGallery(t *testing.T) {
	root := t.TempDir()
	t.Setenv("VNBOX", root)
	for _, n := range []string{"alpha", "beta"} {
		os.MkdirAll(filepath.Join(root, "packages", n), 0o755)
	}
	pkg.WriteLaunch(filepath.Join(root, "packages", "alpha"), pkg.Launch{Target: "Games/a/a.exe", Title: "アルファ 体験版", Engine: "BGI"})
	pkg.WriteLaunch(filepath.Join(root, "packages", "beta"), pkg.Launch{Target: "Games/b/b.exe"})
	os.WriteFile(filepath.Join(root, "packages", "alpha", pkg.ThumbFile), []byte("\xff\xd8jpeg\xff\xd9"), 0o644)
	h := New("", session.Options{}).Handler()

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec
	}
	body := get("/").Body.String()
	for _, want := range []string{"アルファ 体験版", "BGI", "beta", `action="start/alpha"`, "thumb/alpha?v="} {
		if !strings.Contains(body, want) {
			t.Errorf("gallery lacks %q", want)
		}
	}
	var games []Game
	if err := json.Unmarshal(get("/api/games").Body.Bytes(), &games); err != nil || len(games) != 2 {
		t.Fatalf("api: %v %+v", err, games)
	}
	if games[0].Title != "beta" { // no title falls back to the name; sorted by title ("b" < "ア")
		t.Errorf("fallback title: %+v", games[0])
	}
	if rec := get("/thumb/alpha"); rec.Body.String() != "\xff\xd8jpeg\xff\xd9" {
		t.Errorf("package thumb: %d %q", rec.Code, rec.Body.String())
	}
	if rec := get("/thumb/beta"); !strings.HasPrefix(rec.Header().Get("Content-Type"), "image/svg") {
		t.Errorf("placeholder: %s", rec.Header().Get("Content-Type"))
	}
	os.WriteFile(filepath.Join(root, "packages", "alpha", meta.InfoFile),
		[]byte(`{"source":"vndb","title":"アルファ","brand":"ブランドA","release":"2010-12-17","score":70,"tags":["Fantasy"],"description":"あらすじ","fetched":"2026-10-19T00:00:00Z"}`), 0o644)
	os.WriteFile(filepath.Join(root, "packages", "beta", meta.CoverFile), []byte("\xff\xd8cover\xff\xd9"), 0o644)
	body = get("/").Body.String()
	for _, want := range []string{"ブランドA · 2010", ">70<small>", `href="game/alpha"`, `shot art`} {
		if !strings.Contains(body, want) {
			t.Errorf("gallery with meta lacks %q", want)
		}
	}
	d := get("/game/alpha").Body.String()
	for _, want := range []string{"あらすじ", "Fantasy", "2010-12-17", `action="../start/alpha"`} {
		if !strings.Contains(d, want) {
			t.Errorf("detail lacks %q", want)
		}
	}
	if rec := get("/cover/beta"); rec.Body.String() != "\xff\xd8cover\xff\xd9" {
		t.Errorf("cover: %d", rec.Code)
	}
	if rec := get("/game/none"); rec.Code != 404 {
		t.Errorf("unknown game: %d", rec.Code)
	}
	if rec := get("/thumb/..%2fx"); rec.Code != 400 && rec.Code != 404 {
		t.Errorf("bad name: %d", rec.Code)
	}
}

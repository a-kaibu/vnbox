// Package webplay lets packages marked runner=vnrun run in the browser with vnrun
// (https://github.com/a-kaibu/vnrun) instead of Wine: it serves the player page, the game's
// files and its saves over vnrun's protocol (docs/protocol.md there). The host only reads and
// writes files, so a game played this way costs it almost nothing.
//
//	/player/...                    the built vnrun player (Options.PlayerDir)
//	/player/fonts/fonts.json       Japanese fonts found on the host, for RealLive / AVG32
//	/p/<name>/manifest.json        engine, title and file list of the package's root= folder
//	/p/<name>/files/<path>         a game file (Range requests work)
//	/p/<name>/saves/[<path>]       saves: GET lists or reads, PUT writes, DELETE removes
//
// Saves live in websaves/<name>/ under the vnbox root and, with a store, are uploaded a little
// after the game stops writing (and restored from the store when there is no local copy).
package webplay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/a-kaibu/vnbox/internal/pkg"
	"github.com/a-kaibu/vnbox/internal/store"
	"github.com/a-kaibu/vnbox/internal/wine"
)

// Engines maps the engine names of internal/detect to vnrun's engine ids.
var Engines = map[string]string{
	"SiglusEngine": "siglus",
	"RealLive":     "reallive",
}

// fontPaths are fonts vnrun can use for game text, by the name the page asks for.
var fontPaths = map[string][]string{
	"ipag.ttf":   {"/usr/share/fonts/opentype/ipafont-gothic/ipag.ttf", "/usr/share/fonts/truetype/fonts-japanese-gothic.ttf"},
	"ipam.ttf":   {"/usr/share/fonts/opentype/ipafont-mincho/ipam.ttf", "/usr/share/fonts/truetype/fonts-japanese-mincho.ttf"},
	"noto.ttc":   {"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"},
	"notoff.ttc": {"/usr/share/fonts/opentype/noto/NotoSerifCJK-Regular.ttc"},
}

const maxSave = 64 << 20 // a single save file; real ones are far smaller

// pushDelay is how long after the last write the saves are uploaded.
var pushDelay = 30 * time.Second

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Options configures the handler.
type Options struct {
	PlayerDir string       // the built vnrun player (vnrun's dist/); empty disables /player/
	Store     *store.Store // optional: where saves are kept besides the host
}

// Server serves browser play.
type Server struct {
	opt    Options
	mu     sync.Mutex
	timers map[string]*time.Timer // pending save uploads by game
}

// New returns a server.
func New(opt Options) *Server { return &Server{opt: opt, timers: map[string]*time.Timer{}} }

// SaveDir is where the browser saves of a game are kept.
func SaveDir(name string) string { return filepath.Join(wine.Root(), "websaves", name) }

// PlayURL is the page that plays a game, relative to the portal root.
func PlayURL(name string) string { return "player/?game=../p/" + name + "/&back=../" }

// Ready reports whether the player is installed.
func (s *Server) Ready() bool {
	if s.opt.PlayerDir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(s.opt.PlayerDir, "index.html"))
	return err == nil
}

// Register adds the routes to mux.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /player/fonts/fonts.json", s.fontList)
	mux.HandleFunc("GET /player/fonts/{font}", s.font)
	mux.HandleFunc("GET /player/", s.player)
	mux.HandleFunc("GET /p/{name}/manifest.json", s.manifest)
	mux.HandleFunc("GET /p/{name}/files/{path...}", s.file)
	mux.HandleFunc("GET /p/{name}/saves/{$}", s.saveList)
	mux.HandleFunc("GET /p/{name}/saves/{path...}", s.saveGet)
	mux.HandleFunc("PUT /p/{name}/saves/{path...}", s.savePut)
	mux.HandleFunc("DELETE /p/{name}/saves/{path...}", s.saveDelete)
}

func (s *Server) player(w http.ResponseWriter, r *http.Request) {
	if !s.Ready() {
		http.Error(w, "vnrun player is not installed: build vnrun and pass its dist/ as -player (or VNBOX_PLAYER)", http.StatusNotFound)
		return
	}
	http.StripPrefix("/player/", http.FileServer(http.Dir(s.opt.PlayerDir))).ServeHTTP(w, r)
}

func findFont(name string) string {
	for _, p := range fontPaths[name] {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

func (s *Server) fontList(w http.ResponseWriter, r *http.Request) {
	names := []string{}
	for _, n := range []string{"ipag.ttf", "noto.ttc", "ipam.ttf", "notoff.ttc"} {
		if findFont(n) != "" {
			names = append(names, n)
		}
	}
	writeJSON(w, names)
}

func (s *Server) font(w http.ResponseWriter, r *http.Request) {
	p := findFont(r.PathValue("font"))
	if p == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "max-age=86400")
	http.ServeFile(w, r, p)
}

type entry struct {
	P string `json:"p"`
	S int64  `json:"s"`
	M int64  `json:"m,omitempty"`
}

type manifest struct {
	Title  string  `json:"title"`
	Engine string  `json:"engine"`
	Files  []entry `json:"files"`
}

// game returns the package and the folder vnrun runs, or writes an error.
func game(w http.ResponseWriter, r *http.Request) (string, pkg.Launch, string, bool) {
	name := r.PathValue("name")
	if !nameRe.MatchString(name) {
		http.Error(w, "bad name", http.StatusBadRequest)
		return "", pkg.Launch{}, "", false
	}
	p, err := pkg.Open(pkg.Dir(name))
	if err != nil {
		http.NotFound(w, r)
		return "", pkg.Launch{}, "", false
	}
	l, err := p.Launch()
	if err != nil || !l.InBrowser() || Engines[l.Engine] == "" {
		http.Error(w, "this package does not run in the browser", http.StatusNotFound)
		return "", pkg.Launch{}, "", false
	}
	root, ok := within(filepath.Join(p.Dir, "files"), l.Root)
	if !ok {
		http.Error(w, "bad root= in launch.ini", http.StatusInternalServerError)
		return "", pkg.Launch{}, "", false
	}
	return name, l, root, true
}

// within joins a slash-separated relative path to dir, refusing paths that leave it.
func within(dir, rel string) (string, bool) {
	if rel == "" {
		return dir, true
	}
	if strings.ContainsAny(rel, "\\\x00") || !fs.ValidPath(rel) {
		return "", false
	}
	return filepath.Join(dir, filepath.FromSlash(rel)), true
}

func list(dir string, withTime bool) []entry {
	out := []entry{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		e := entry{P: filepath.ToSlash(rel), S: info.Size()}
		if withTime {
			e.M = info.ModTime().UnixMilli()
		}
		out = append(out, e)
		return nil
	})
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	name, l, root, ok := game(w, r)
	if !ok {
		return
	}
	s.restore(r.Context(), name)
	title := l.Title
	if title == "" {
		title = name
	}
	writeJSON(w, manifest{Title: title, Engine: Engines[l.Engine], Files: list(root, false)})
}

func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	_, _, root, ok := game(w, r)
	if !ok {
		return
	}
	p, ok := within(root, r.PathValue("path"))
	if !ok {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "max-age=3600")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// savePath returns the file of a save request, or writes an error.
func savePath(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	name, _, _, ok := game(w, r)
	if !ok {
		return "", "", false
	}
	p, ok := within(SaveDir(name), r.PathValue("path"))
	if !ok || r.PathValue("path") == "" {
		http.Error(w, "bad path", http.StatusBadRequest)
		return "", "", false
	}
	return name, p, true
}

func (s *Server) saveList(w http.ResponseWriter, r *http.Request) {
	name, _, _, ok := game(w, r)
	if !ok {
		return
	}
	s.restore(r.Context(), name)
	writeJSON(w, list(SaveDir(name), true))
}

func (s *Server) saveGet(w http.ResponseWriter, r *http.Request) {
	_, p, ok := savePath(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, p)
}

func (s *Server) savePut(w http.ResponseWriter, r *http.Request) {
	name, p, ok := savePath(w, r)
	if !ok {
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSave))
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmp, p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.schedulePush(name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) saveDelete(w http.ResponseWriter, r *http.Request) {
	name, p, ok := savePath(w, r)
	if !ok {
		return
	}
	if err := os.Remove(p); err != nil {
		http.NotFound(w, r)
		return
	}
	s.schedulePush(name)
	w.WriteHeader(http.StatusNoContent)
}

// restore fetches the saves from the store when the host has none for the game.
func (s *Server) restore(ctx context.Context, name string) {
	dir := SaveDir(name)
	if s.opt.Store == nil {
		return
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		return
	}
	os.MkdirAll(filepath.Dir(dir), 0o755)
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	found, err := s.opt.Store.WebSavePull(ctx, name, dir)
	switch {
	case err != nil:
		log.Printf("%s: restore browser saves: %v", name, err)
	case found:
		log.Printf("%s: browser saves restored from the store", name)
	}
}

// schedulePush uploads the saves once the game has stopped writing for pushDelay.
func (s *Server) schedulePush(name string) {
	if s.opt.Store == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.timers[name]; t != nil {
		t.Reset(pushDelay)
		return
	}
	s.timers[name] = time.AfterFunc(pushDelay, func() { s.push(name) })
}

func (s *Server) push(name string) {
	s.mu.Lock()
	delete(s.timers, name)
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := s.opt.Store.WebSavePush(ctx, name, SaveDir(name)); err != nil {
		log.Printf("%s: upload browser saves: %v", name, err)
	}
}

// Shutdown uploads the saves that are still waiting.
func (s *Server) Shutdown() {
	s.mu.Lock()
	var names []string
	for n, t := range s.timers {
		if t.Stop() {
			names = append(names, n)
		}
	}
	s.mu.Unlock()
	for _, n := range names {
		s.push(n)
	}
}

// markers are the files vnrun opens first for each engine; the game folder must hold them.
var markers = map[string]string{"siglus": "scene.pck", "reallive": "seen.txt"}

// Playable reports whether vnrun can run the game whose engine (internal/detect name) and
// folder (where its exe is) are given.
func Playable(engine, dir string) bool {
	m := markers[Engines[engine]]
	if m == "" {
		return false
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range ents {
		if strings.EqualFold(e.Name(), m) {
			return true
		}
	}
	return false
}

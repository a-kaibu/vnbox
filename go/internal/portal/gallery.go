package portal

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vnbox/internal/meta"
	"vnbox/internal/pkg"
	"vnbox/internal/stream"
	"vnbox/internal/wine"
)

//go:embed gallery.html
var galleryHTML string

//go:embed detail.html
var detailHTML string

var (
	galleryTmpl = template.Must(template.New("g").Parse(galleryHTML))
	detailTmpl  = template.Must(template.New("d").Parse(detailHTML))
)

// Game is one gallery entry.
type Game struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Engine  string `json:"engine,omitempty"`
	Local   bool   `json:"local"`
	Remote  bool   `json:"remote"`
	Save    string `json:"save,omitempty"` // time of the latest save in the store
	Running bool   `json:"running"`
	Thumb   string `json:"thumb"` // URL relative to the portal root
	Cover   string `json:"cover,omitempty"`
	Art     bool   `json:"-"` // Thumb is the (portrait) cover, not a game screen

	Info *meta.Info `json:"meta,omitempty"` // from ErogameScape / VNDB (vnbox meta -fetch)
}

// Year is the release year for the card.
func (g Game) Year() string {
	if g.Info == nil || len(g.Info.Release) < 4 {
		return ""
	}
	if y := g.Info.Release[:4]; strings.Trim(y, "0123456789") == "" {
		return y
	}
	return ""
}

type liveShot struct {
	jpeg []byte
	at   time.Time
}

// cacheDir keeps metadata and thumbnails of packages that are only in the store.
func cacheDir() string { return filepath.Join(wine.Root(), "runtime", "gallery") }

func (s *Server) games(ctx context.Context) []Game {
	m := map[string]*Game{}
	get := func(n string) *Game {
		if m[n] == nil {
			m[n] = &Game{Name: n}
		}
		return m[n]
	}
	if ents, err := os.ReadDir(filepath.Join(wine.Root(), "packages")); err == nil {
		for _, e := range ents {
			p, err := pkg.Open(pkg.Dir(e.Name()))
			if err != nil {
				continue
			}
			g := get(e.Name())
			g.Local = true
			if l, err := p.Launch(); err == nil {
				g.Title, g.Engine = l.Title, l.Engine
			}
			if st, err := os.Stat(p.Thumb()); err == nil {
				g.Thumb = "thumb/" + g.Name + "?v=" + strconv.FormatInt(st.ModTime().Unix(), 36)
			}
			g.Info, _ = meta.Load(p.Dir)
			if st, err := os.Stat(filepath.Join(p.Dir, meta.CoverFile)); err == nil {
				g.Cover = "cover/" + g.Name + "?v=" + strconv.FormatInt(st.ModTime().Unix(), 36)
			}
		}
	}
	if s.Opt.Store != nil {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if l, err := s.Opt.Store.List(ctx); err == nil {
			var remote []*Game
			for _, p := range l.Packages {
				g := get(p.Name)
				g.Remote = true
				if !g.Local {
					remote = append(remote, g)
				}
			}
			for _, sv := range l.Saves {
				get(sv.Name).Save = sv.Modified.Local().Format("2006-01-02 15:04")
			}
			s.remoteMeta(ctx, remote)
		}
	}
	s.mu.Lock()
	for n, ss := range s.sessions {
		if ss.Running() {
			g := get(n)
			g.Running = true
			g.Thumb = "thumb/" + n + "?live=1"
		}
	}
	s.mu.Unlock()
	out := make([]Game, 0, len(m))
	for _, g := range m {
		if !g.Local && !g.Remote {
			continue
		}
		if g.Title == "" {
			g.Title = g.Name
		}
		if g.Thumb == "" && g.Cover != "" {
			g.Thumb, g.Art = g.Cover, true
		}
		if g.Thumb == "" {
			g.Thumb = "thumb/" + g.Name // a placeholder (or the store's thumbnail)
		}
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Running != out[j].Running {
			return out[i].Running
		}
		return out[i].Title < out[j].Title
	})
	return out
}

// remoteMeta fills title, engine and database metadata of store-only packages from their side
// objects (launch.ini, meta.json), cached under runtime/gallery so the store is asked once.
func (s *Server) remoteMeta(ctx context.Context, games []*Game) {
	os.MkdirAll(cacheDir(), 0o755)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	side := func(name, ext string) []byte {
		path := filepath.Join(cacheDir(), name+"."+ext)
		if b, err := os.ReadFile(path); err == nil {
			return b
		}
		if recent(path+".none", time.Hour) {
			return nil
		}
		sem <- struct{}{}
		b, err := s.Opt.Store.PackageFile(ctx, name, ext)
		<-sem
		if err != nil {
			os.WriteFile(path+".none", nil, 0o644)
			return nil
		}
		os.WriteFile(path, b, 0o644)
		return b
	}
	for _, g := range games {
		wg.Add(1)
		go func(g *Game) {
			defer wg.Done()
			if l, err := pkg.ParseLaunch(bytes.NewReader(side(g.Name, "ini"))); err == nil {
				g.Title, g.Engine = l.Title, l.Engine
			}
			if b := side(g.Name, "json"); b != nil {
				var in meta.Info
				if json.Unmarshal(b, &in) == nil {
					g.Info = &in
					g.Cover = "cover/" + g.Name
				}
			}
		}(g)
	}
	wg.Wait()
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	games := s.games(r.Context())
	running := 0
	for _, g := range games {
		if g.Running {
			running++
		}
	}
	galleryTmpl.Execute(w, map[string]any{"Games": games, "Running": running, "Store": s.Opt.Store != nil})
}

func (s *Server) apiGames(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.games(r.Context()))
}

// thumb serves a game's picture: a live frame while it runs, else the package thumbnail,
// else the one in the store, else a generated placeholder.
func (s *Server) thumb(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !nameRe.MatchString(name) {
		http.Error(w, "bad name", 400)
		return
	}
	if b := s.liveFrame(r.Context(), name); b != nil {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(b)
		return
	}
	cache := func() { w.Header().Set("Cache-Control", "private, max-age=86400") }
	if p := filepath.Join(pkg.Dir(name), pkg.ThumbFile); fileExists(p) {
		cache()
		http.ServeFile(w, r, p)
		return
	}
	if s.Opt.Store != nil {
		p := filepath.Join(cacheDir(), name+".jpg")
		miss := p + ".none"
		if !fileExists(p) && !recent(miss, time.Hour) {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			b, err := s.Opt.Store.PackageFile(ctx, name, "jpg")
			cancel()
			os.MkdirAll(cacheDir(), 0o755)
			if err == nil && len(b) > 0 {
				os.WriteFile(p, b, 0o644)
			} else {
				os.WriteFile(miss, nil, 0o644)
			}
		}
		if fileExists(p) {
			cache()
			http.ServeFile(w, r, p)
			return
		}
	}
	if s.serveCover(w, r, name) {
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	cache()
	w.Write(placeholder(name))
}

// cover serves the package art fetched by `vnbox meta -fetch`.
func (s *Server) cover(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !nameRe.MatchString(name) {
		http.Error(w, "bad name", 400)
		return
	}
	if !s.serveCover(w, r, name) {
		http.NotFound(w, r)
	}
}

func (s *Server) serveCover(w http.ResponseWriter, r *http.Request, name string) bool {
	p := filepath.Join(pkg.Dir(name), meta.CoverFile)
	if !fileExists(p) && s.Opt.Store != nil {
		c := filepath.Join(cacheDir(), name+".cover.jpg")
		if !fileExists(c) && !recent(c+".none", time.Hour) {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			b, err := s.Opt.Store.PackageFile(ctx, name, "cover.jpg")
			cancel()
			os.MkdirAll(cacheDir(), 0o755)
			if err == nil && len(b) > 0 {
				os.WriteFile(c, b, 0o644)
			} else {
				os.WriteFile(c+".none", nil, 0o644)
			}
		}
		p = c
	}
	if !fileExists(p) {
		return false
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, p)
	return true
}

// detail shows one game: cover, metadata, story, links and the last screen.
func (s *Server) detail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	for _, g := range s.games(r.Context()) {
		if g.Name == name {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			detailTmpl.Execute(w, g)
			return
		}
	}
	http.NotFound(w, r)
}

// liveFrame returns a fresh frame of a running game (at most one capture per 2 seconds).
func (s *Server) liveFrame(ctx context.Context, name string) []byte {
	s.mu.Lock()
	ss := s.sessions[name]
	ls := s.live[name]
	s.mu.Unlock()
	if ss == nil || !ss.Running() {
		return nil
	}
	if time.Since(ls.at) < 2*time.Second && ls.jpeg != nil {
		return ls.jpeg
	}
	b, err := stream.Snapshot(ctx, ss.Display, 480)
	if err != nil {
		return ls.jpeg
	}
	s.mu.Lock()
	s.live[name] = liveShot{b, time.Now()}
	s.mu.Unlock()
	return b
}

// placeholder draws the package name on a colour derived from it.
func placeholder(name string) []byte {
	h := 0
	for _, c := range name {
		h = (h*31 + int(c)) % 360
	}
	label := strings.ToUpper(name)
	if len(label) > 18 {
		label = label[:18]
	}
	var b bytes.Buffer
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 480 300">`)
	b.WriteString(`<defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1">`)
	b.WriteString(`<stop offset="0" stop-color="hsl(` + strconv.Itoa(h) + `,45%,32%)"/>`)
	b.WriteString(`<stop offset="1" stop-color="hsl(` + strconv.Itoa((h+40)%360) + `,50%,18%)"/></linearGradient></defs>`)
	b.WriteString(`<rect width="480" height="300" fill="url(#g)"/>`)
	b.WriteString(`<text x="240" y="160" font-family="system-ui,sans-serif" font-size="28" font-weight="600" fill="rgba(255,255,255,.75)" text-anchor="middle">`)
	template.HTMLEscape(&b, []byte(label))
	b.WriteString(`</text></svg>`)
	return b.Bytes()
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Size() > 0
}

func recent(p string, d time.Duration) bool {
	st, err := os.Stat(p)
	return err == nil && time.Since(st.ModTime()) < d
}

// Package portal is the web front door: it lists games (local packages and those in
// the store), starts and stops sessions, and mounts each session's stream under /g/<name>/.
package portal

import (
	"context"
	"crypto/subtle"
	"html/template"
	"net/http"
	"os"
	"regexp"
	"sort"
	"sync"
	"time"

	"vnbox/internal/session"
	"vnbox/internal/wine"
)

// Server holds the running sessions.
type Server struct {
	Token string // empty disables authentication (only for trusted networks)
	Opt   session.Options

	mu       sync.Mutex
	sessions map[string]*session.Session
}

// New returns a portal.
func New(token string, opt session.Options) *Server {
	return &Server{Token: token, Opt: opt, sessions: map[string]*session.Session{}}
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("POST /start/{name}", s.start)
	mux.HandleFunc("POST /stop/{name}", s.stop)
	mux.HandleFunc("/g/{name}/", s.game)
	return s.auth(mux)
}

// auth accepts the token once as ?token=... and then as a cookie.
func (s *Server) auth(next http.Handler) http.Handler {
	if s.Token == "" {
		return next
	}
	ok := func(v string) bool { return subtle.ConstantTimeCompare([]byte(v), []byte(s.Token)) == 1 }
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if t := r.URL.Query().Get("token"); t != "" && ok(t) {
			http.SetCookie(w, &http.Cookie{Name: "vnbox", Value: t, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 30 * 86400})
			u := *r.URL
			q := u.Query()
			q.Del("token")
			u.RawQuery = q.Encode()
			http.Redirect(w, r, u.String(), http.StatusSeeOther)
			return
		}
		if c, err := r.Cookie("vnbox"); err == nil && ok(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "unauthorized: open the URL with ?token=...", http.StatusUnauthorized)
	})
}

type entry struct {
	Name    string
	Local   bool
	Remote  bool
	Save    string
	Running bool
}

func (s *Server) games(ctx context.Context) []entry {
	m := map[string]*entry{}
	get := func(n string) *entry {
		if m[n] == nil {
			m[n] = &entry{Name: n}
		}
		return m[n]
	}
	if ents, err := os.ReadDir(wine.Root() + "/packages"); err == nil {
		for _, e := range ents {
			if _, err := os.Stat(wine.Root() + "/packages/" + e.Name() + "/launch.ini"); err == nil {
				get(e.Name()).Local = true
			}
		}
	}
	if s.Opt.Store != nil {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if l, err := s.Opt.Store.List(ctx); err == nil {
			for _, p := range l.Packages {
				get(p.Name).Remote = true
			}
			for _, sv := range l.Saves {
				get(sv.Name).Save = sv.Modified.Local().Format("2006-01-02 15:04")
			}
		}
	}
	s.mu.Lock()
	for n, ss := range s.sessions {
		if ss.Running() {
			get(n).Running = true
		}
	}
	s.mu.Unlock()
	out := make([]entry, 0, len(m))
	for _, e := range m {
		if e.Local || e.Remote {
			out = append(out, *e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

var indexTmpl = template.Must(template.New("i").Parse(`<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>vnbox</title><style>
body{font:15px system-ui,sans-serif;margin:0;padding:16px;background:#111;color:#eee}
li{list-style:none;display:flex;gap:8px;align-items:center;padding:8px 0;border-bottom:1px solid #333;flex-wrap:wrap}
ul{padding:0}.n{flex:1;min-width:12em}.m{color:#999;font-size:12px}button,a.b{font:inherit;padding:6px 12px;border-radius:6px;border:0;background:#2a6;color:#fff;text-decoration:none}
button.s{background:#a33}</style></head><body><h1>vnbox</h1><ul>
{{range .}}<li><span class="n">{{.Name}}</span><span class="m">{{if .Local}}local{{end}}{{if .Remote}} s3{{end}}{{if .Save}} / save {{.Save}}{{end}}</span>
{{if .Running}}<a class="b" href="g/{{.Name}}/">続ける</a><form method="post" action="stop/{{.Name}}"><button class="s">終了</button></form>
{{else}}<form method="post" action="start/{{.Name}}"><button>遊ぶ</button></form>{{end}}</li>{{else}}<li>パッケージがありません (vnbox add で追加)</li>{{end}}
</ul></body></html>`))

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	indexTmpl.Execute(w, s.games(r.Context()))
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !nameRe.MatchString(name) {
		http.Error(w, "bad name", 400)
		return
	}
	s.mu.Lock()
	ss := s.sessions[name]
	s.mu.Unlock()
	if ss == nil || !ss.Running() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		var err error
		if ss, err = session.Start(ctx, name, s.Opt); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		s.mu.Lock()
		s.sessions[name] = ss
		s.mu.Unlock()
	}
	http.Redirect(w, r, "../g/"+name+"/", http.StatusSeeOther)
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.Lock()
	ss := s.sessions[name]
	delete(s.sessions, name)
	s.mu.Unlock()
	if ss != nil {
		ss.Stop()
	}
	http.Redirect(w, r, "../", http.StatusSeeOther)
}

func (s *Server) game(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.Lock()
	ss := s.sessions[name]
	s.mu.Unlock()
	if ss == nil || !ss.Running() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	http.StripPrefix("/g/"+name, ss.Game.Handler()).ServeHTTP(w, r)
}

// Shutdown stops every session (uploading their saves).
func (s *Server) Shutdown() {
	s.mu.Lock()
	all := make([]*session.Session, 0, len(s.sessions))
	for _, ss := range s.sessions {
		all = append(all, ss)
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, ss := range all {
		wg.Add(1)
		go func(ss *session.Session) { defer wg.Done(); ss.Stop() }(ss)
	}
	wg.Wait()
}

// Adopt registers an already started session (used by `vnbox play`).
func (s *Server) Adopt(ss *session.Session) {
	s.mu.Lock()
	s.sessions[ss.Name] = ss
	s.mu.Unlock()
}

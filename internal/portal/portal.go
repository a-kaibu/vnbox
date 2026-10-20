// Package portal is the web front door: a gallery of games (local packages and those in
// the store), starting and stopping sessions, and each session's stream under /g/<name>/.
package portal

import (
	"context"
	"crypto/subtle"
	"net/http"
	"regexp"
	"sync"
	"time"

	"vnbox/internal/session"
)

// Server holds the running sessions.
type Server struct {
	// Users maps access tokens to players. Each player has their own sessions, prefixes and
	// saves. A token mapped to "" is the store's own user (VNBOX_USER). No tokens at all
	// disables authentication (only for trusted networks).
	Users map[string]string
	Opt   session.Options

	mu       sync.Mutex
	sessions map[string]*session.Session // by session.Key(user, name)
	live     map[string]liveShot         // recent frames of running games for the gallery
}

// New returns a portal for one player with one token ("" = no authentication).
func New(token string, opt session.Options) *Server {
	users := map[string]string{}
	if token != "" {
		users[token] = ""
	}
	return NewUsers(users, opt)
}

// NewUsers returns a portal for several players (token → player name).
func NewUsers(users map[string]string, opt session.Options) *Server {
	return &Server{Users: users, Opt: opt, sessions: map[string]*session.Session{}, live: map[string]liveShot{}}
}

type userKey struct{}

// user is the player of the request.
func user(r *http.Request) string {
	u, _ := r.Context().Value(userKey{}).(string)
	return u
}

func (s *Server) session(r *http.Request, name string) *session.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[session.Key(user(r), name)]
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /api/games", s.apiGames)
	mux.HandleFunc("GET /thumb/{name}", s.thumb)
	mux.HandleFunc("GET /cover/{name}", s.cover)
	mux.HandleFunc("GET /game/{name}", s.detail)
	mux.HandleFunc("POST /start/{name}", s.start)
	mux.HandleFunc("POST /stop/{name}", s.stop)
	mux.HandleFunc("/g/{name}/", s.game)
	return s.auth(mux)
}

// auth accepts a token once as ?token=... and then as a cookie, and tells the handlers whose
// it is.
func (s *Server) auth(next http.Handler) http.Handler {
	if len(s.Users) == 0 {
		return next
	}
	lookup := func(v string) (string, bool) {
		for t, u := range s.Users {
			if subtle.ConstantTimeCompare([]byte(v), []byte(t)) == 1 {
				return u, true
			}
		}
		return "", false
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if t := r.URL.Query().Get("token"); t != "" {
			if _, ok := lookup(t); !ok {
				http.Error(w, "unauthorized: unknown token", http.StatusUnauthorized)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "vnbox", Value: t, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 30 * 86400})
			u := *r.URL
			q := u.Query()
			q.Del("token")
			u.RawQuery = q.Encode()
			http.Redirect(w, r, u.String(), http.StatusSeeOther)
			return
		}
		if c, err := r.Cookie("vnbox"); err == nil {
			if u, ok := lookup(c.Value); ok {
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
				return
			}
		}
		http.Error(w, "unauthorized: open the URL with ?token=...", http.StatusUnauthorized)
	})
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !nameRe.MatchString(name) {
		http.Error(w, "bad name", 400)
		return
	}
	ss := s.session(r, name)
	if ss == nil || !ss.Running() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		opt := s.Opt
		opt.User = user(r)
		var err error
		if ss, err = session.Start(ctx, name, opt); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		s.mu.Lock()
		s.sessions[session.Key(opt.User, name)] = ss
		s.mu.Unlock()
	}
	http.Redirect(w, r, "../g/"+name+"/", http.StatusSeeOther)
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.Lock()
	ss := s.sessions[session.Key(user(r), name)]
	delete(s.sessions, session.Key(user(r), name))
	s.mu.Unlock()
	if ss != nil {
		ss.Stop()
	}
	http.Redirect(w, r, "../", http.StatusSeeOther)
}

func (s *Server) game(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ss := s.session(r, name)
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
	s.sessions[session.Key(ss.User, ss.Name)] = ss
	s.mu.Unlock()
}

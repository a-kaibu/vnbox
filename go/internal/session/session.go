// Package session runs one game for one player: it prepares the prefix from the package,
// restores the save, starts Wine on a private X display and audio sink, and keeps the
// save in sync with the store while the game runs.
package session

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"vnbox/internal/pkg"
	"vnbox/internal/store"
	"vnbox/internal/stream"
	"vnbox/internal/wine"
	"vnbox/internal/x11"
)

// Options configure a session.
type Options struct {
	Flavor       wine.Flavor
	Store        *store.Store // nil disables S3 sync
	SaveInterval time.Duration
	FPS          int
	Log          io.Writer

	// for vnbox probe (dynamic checks): another prefix than prefixes/run-<name>, where Wine's
	// output goes (default results/play-<name>.log), WINEDEBUG, and no Managed=N restart
	Prefix    string
	WineLog   string
	Debug     string
	NoManaged bool
}

// Session is a running game.
type Session struct {
	Name    string
	Prefix  string
	Display string
	Sink    string
	Game    *stream.Game

	opt    Options
	env    wine.Env
	launch pkg.Launch

	mu      sync.Mutex
	running bool
	done    chan struct{}
	cancel  context.CancelFunc
}

// Prefix returns the prefix path used for a package.
func Prefix(name string) string { return filepath.Join(wine.Root(), "prefixes", "run-"+name) }

// Start launches the package. The package is pulled from the store if it is not local.
func Start(ctx context.Context, name string, o Options) (*Session, error) {
	if o.Log == nil {
		o.Log = io.Discard
	}
	if o.Flavor == "" {
		o.Flavor = wine.Vnbox
	}
	if o.SaveInterval == 0 {
		o.SaveInterval = 5 * time.Minute
	}
	p, err := pkg.Open(pkg.Dir(name))
	if err != nil {
		if o.Store == nil {
			return nil, err
		}
		fmt.Fprintf(o.Log, "[%s] downloading package\n", name)
		if p, err = o.Store.PullPackage(ctx, name); err != nil {
			return nil, fmt.Errorf("pull package: %w", err)
		}
	}
	l, err := p.Launch()
	if err != nil {
		return nil, err
	}
	fl := l.WineFlavor(o.Flavor)
	s := &Session{Name: name, Prefix: Prefix(name), Display: wine.FreeDisplay(name), Sink: wine.SinkFor(name), opt: o, launch: l}
	if o.Prefix != "" {
		s.Prefix = o.Prefix
	}
	s.env = wine.Env{Flavor: fl, Prefix: s.Prefix, Display: s.Display, Sink: s.Sink, Debug: o.Debug}
	if err := wine.EnsureX(s.Display); err != nil {
		return nil, err
	}
	if err := wine.EnsureAudio(s.Sink); err != nil {
		return nil, err
	}
	if err := p.Prepare(ctx, fl, s.Prefix, s.Display); err != nil {
		return nil, fmt.Errorf("prepare: %w", err)
	}
	if o.Store != nil {
		s.env.Kill()
		found, n, err := o.Store.SavePull(ctx, name, s.Prefix, "")
		switch {
		case err != nil:
			fmt.Fprintf(o.Log, "[%s] save restore failed: %v\n", name, err)
		case found:
			fmt.Fprintf(o.Log, "[%s] restored save (%d files)\n", name, n)
		}
	}
	s.Game = &stream.Game{Display: s.Display, Sink: s.Sink, FPS: o.FPS}
	gctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.done, s.running = cancel, make(chan struct{}), true
	go s.run(gctx)
	return s, nil
}

func (s *Session) start(ctx context.Context) (chan error, error) {
	dir, exe := s.launch.GameDir(s.Prefix)
	args := []string{exe}
	if s.launch.Args != "" {
		args = append(args, splitArgs(s.launch.Args)...)
	}
	c := s.env.Command(ctx, dir, args...)
	if s.launch.IsHTML() {
		c = wine.BrowserCommand(ctx, s.env, filepath.Join(dir, exe))
	}
	logPath := s.opt.WineLog
	if logPath == "" {
		logPath = filepath.Join(wine.Root(), "results", "play-"+s.Name+".log")
	}
	lf, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	c.Stdout, c.Stderr = lf, lf
	if err := c.Start(); err != nil {
		return nil, err
	}
	ch := make(chan error, 1)
	go func() { ch <- c.Wait(); lf.Close() }()
	return ch, nil
}

func (s *Session) run(ctx context.Context) {
	defer close(s.done)
	exited, err := s.start(ctx)
	if err != nil {
		fmt.Fprintf(s.opt.Log, "[%s] start: %v\n", s.Name, err)
		s.finish()
		return
	}
	// Games that leave fullscreen to the window manager keep a 1x1 X window without one.
	// Let Wine manage the windows itself (X11 Driver\Managed=N) and start again.
	if !s.opt.NoManaged && !s.launch.IsHTML() && s.wait1x1(ctx) {
		fmt.Fprintf(s.opt.Log, "[%s] 1x1 window -> restarting with Managed=N\n", s.Name)
		s.env.Kill()
		<-exited
		s.env.RegAdd(ctx, `HKCU\Software\Wine\X11 Driver`, "Managed", "", "N")
		s.env.Wait()
		if exited, err = s.start(ctx); err != nil {
			s.finish()
			return
		}
	}
	tick := time.NewTicker(s.opt.SaveInterval)
	defer tick.Stop()
	first := time.After(45 * time.Second) // gallery thumbnail once the title screen is up
	for {
		select {
		case <-first:
			s.thumb(ctx)
		case <-exited:
			// the launcher may have exited after spawning the game: keep going while Wine has windows
			if _, ok := x11.Display(s.Display).Largest(); ok {
				exited = make(chan error) // never fires; stop on Stop() or when windows disappear
				go s.watchWindows(ctx)
				continue
			}
			s.finish()
			return
		case <-tick.C:
			s.thumb(ctx)
			s.save(ctx, false)
		case <-ctx.Done():
			s.finish()
			return
		}
	}
}

func (s *Session) watchWindows(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			if _, ok := x11.Display(s.Display).Largest(); !ok {
				s.cancel()
				return
			}
		}
	}
}

func (s *Session) wait1x1(ctx context.Context) bool {
	for i := 0; i < 2; i++ { // slow starters get 20 + 20 seconds
		select {
		case <-ctx.Done():
			return false
		case <-time.After(20 * time.Second):
		}
		if w, ok := x11.Display(s.Display).Largest(); ok && w.Area() > 1 {
			return false
		}
	}
	w, ok := x11.Display(s.Display).Largest()
	return ok && w.Area() <= 1
}

// finish stops Wine, flushes the registry, uploads the final save and stops the X server.
func (s *Session) finish() {
	s.env.Kill()
	s.env.Wait()
	s.save(context.Background(), true)
	// the display and the sink belong to this run only (the next start picks free ones)
	wine.StopX(s.Display)
	wine.RemoveSink(s.Sink)
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
}

func (s *Session) save(ctx context.Context, final bool) {
	if s.opt.Store == nil {
		return
	}
	if final {
		if p, err := pkg.Open(pkg.Dir(s.Name)); err == nil {
			s.opt.Store.PushThumb(ctx, p)
		}
	}
	r, err := s.opt.Store.SavePush(ctx, s.Name, s.Prefix)
	switch {
	case err != nil:
		fmt.Fprintf(s.opt.Log, "[%s] save failed: %v\n", s.Name, err)
	case r.Unchanged:
		if final {
			fmt.Fprintf(s.opt.Log, "[%s] save unchanged\n", s.Name)
		}
	default:
		fmt.Fprintf(s.opt.Log, "[%s] saved %d files (%d bytes) %s\n", s.Name, r.Files, r.Bytes, r.Timestamp)
	}
}

// thumb stores the current game screen as the package's gallery thumbnail.
// Nearly uniform frames (black screens while loading) compress to a few KB and are skipped.
func (s *Session) thumb(ctx context.Context) {
	b, err := stream.Snapshot(ctx, s.Display, 480)
	if err != nil || len(b) < 4<<10 {
		return
	}
	p := filepath.Join(pkg.Dir(s.Name), pkg.ThumbFile)
	if os.WriteFile(p+".tmp", b, 0o644) == nil {
		os.Rename(p+".tmp", p)
	}
}

// Env returns the Wine environment of the game (prefix, display, sink).
func (s *Session) Env() wine.Env { return s.env }

// Launch returns the package's launch settings.
func (s *Session) Launch() pkg.Launch { return s.launch }

// Running reports whether the game is still running.
func (s *Session) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Done is closed when the session has ended and its save was uploaded.
func (s *Session) Done() <-chan struct{} { return s.done }

// Stop ends the game and waits for the final save.
func (s *Session) Stop() {
	if s.Running() {
		s.thumb(context.Background()) // the scene the player stopped at, before Wine is killed
	}
	s.cancel()
	select {
	case <-s.done:
	case <-time.After(2 * time.Minute):
		log.Printf("[%s] stop timed out", s.Name)
	}
}

// splitArgs splits launch.ini args on spaces, honouring double quotes.
func splitArgs(s string) []string {
	var out []string
	var cur []rune
	q := false
	for _, r := range s {
		switch {
		case r == '"':
			q = !q
		case r == ' ' && !q:
			if len(cur) > 0 {
				out = append(out, string(cur))
				cur = cur[:0]
			}
		default:
			cur = append(cur, r)
		}
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

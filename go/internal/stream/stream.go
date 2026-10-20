// Package stream serves one game window to a browser: video and audio over WebRTC (rtc.go;
// the audio is an Opus track on the same connection), a silent MJPEG fallback over
// multipart/x-mixed-replace (ffmpeg x11grab of the game window only) for browsers that
// cannot connect, and input posted back as JSON and replayed with xdotool.
package stream

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"vnbox/internal/x11"
)

//go:embed page.html
var page []byte

// Game is the stream source.
type Game struct {
	Display string
	Sink    string
	FPS     int

	mu   sync.Mutex
	held map[string]*time.Timer // keys held down by the player, released automatically
	hubs map[string]*hub        // WebRTC encoders by codec
}

// maxHold releases a held key the browser never let go of (closed tab, lost connection).
const maxHold = 2 * time.Minute

// Handler serves "", "video", "input", "rtc" and "rtc-config" relative to where it is mounted.
func (g *Game) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	mux.HandleFunc("GET /video", g.video)
	mux.HandleFunc("POST /input", g.input)
	mux.HandleFunc("POST /rtc", g.rtcOffer)
	mux.HandleFunc("GET /rtc-config", g.rtcConfig)
	return mux
}

func (g *Game) disp() x11.Display { return x11.Display(g.Display) }

// waitWindow waits until the game shows a real window.
func (g *Game) waitWindow(r *http.Request) (x11.Window, bool) {
	for {
		if w, ok := g.disp().Largest(); ok && w.Area() > 1 {
			return w, true
		}
		select {
		case <-r.Context().Done():
			return x11.Window{}, false
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (g *Game) video(w http.ResponseWriter, r *http.Request) {
	win, ok := g.waitWindow(r)
	if !ok {
		return
	}
	fps := g.FPS
	if fps <= 0 {
		fps = 15
	}
	cmd := exec.CommandContext(r.Context(), "ffmpeg", "-v", "error", "-f", "x11grab", "-draw_mouse", "0",
		"-framerate", strconv.Itoa(fps), "-video_size", fmt.Sprintf("%dx%d", win.W, win.H),
		"-i", fmt.Sprintf("%s+%d,%d", g.Display, win.X, win.Y), "-f", "mjpeg", "-q:v", "5", "-")
	cmd.Env = append(os.Environ(), "DISPLAY="+g.Display)
	out, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := cmd.Start(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer cmd.Wait()
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=f")
	w.Header().Set("Cache-Control", "no-store")
	fl, _ := w.(http.Flusher)
	var buf []byte
	chunk := make([]byte, 64<<10)
	for {
		n, err := out.Read(chunk)
		buf = append(buf, chunk[:n]...)
		for { // cut JPEG frames at SOI / EOI markers
			s := bytes.Index(buf, []byte{0xff, 0xd8})
			if s < 0 {
				break
			}
			e := bytes.Index(buf[s+2:], []byte{0xff, 0xd9})
			if e < 0 {
				break
			}
			frame := buf[s : s+2+e+2]
			if _, werr := fmt.Fprintf(w, "--f\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(frame)); werr != nil {
				return
			}
			w.Write(frame)
			w.Write([]byte("\r\n"))
			if fl != nil {
				fl.Flush()
			}
			buf = buf[s+2+e+2:]
		}
		if err != nil {
			return
		}
	}
}

// Input is one browser event.
type Input struct {
	T      string `json:"t"` // "click", "key", "keydown", "keyup" or "text"
	X, Y   int
	B      int    `json:"b"`
	NoMove int    `json:"nomove"`
	K      string `json:"k"`
	S      string `json:"s"` // text for "text"
}

var keyMap = map[string]string{"Enter": "Return", " ": "space", "ArrowUp": "Up", "ArrowDown": "Down",
	"ArrowLeft": "Left", "ArrowRight": "Right", "Escape": "Escape", "Backspace": "BackSpace", "Control": "ctrl"}

var keysymRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func (g *Game) input(w http.ResponseWriter, r *http.Request) {
	var ev Input
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&ev); err != nil {
		http.Error(w, "bad input", 400)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	g.handle(ev)
}

// handle replays one input event (from POST /input or the WebRTC data channel).
func (g *Game) handle(ev Input) {
	win, ok := g.disp().Largest()
	if !ok {
		return
	}
	switch ev.T {
	case "click":
		b := ev.B
		if b < 1 || b > 5 {
			b = 1
		}
		if ev.NoMove != 0 {
			g.disp().Button(b)
		} else {
			g.disp().Click(win, ev.X, ev.Y, b)
		}
	case "key", "keydown", "keyup":
		k := ev.K
		if m, ok := keyMap[k]; ok {
			k = m
		}
		if !keysymRe.MatchString(k) { // only plain keysyms reach xdotool
			return
		}
		switch ev.T {
		case "key":
			g.disp().Key(win, k)
		case "keydown":
			g.hold(win, k)
		case "keyup":
			g.release(k)
		}
	case "text":
		if t := []rune(ev.S); len(t) > 0 && len(t) <= 64 && !strings.ContainsAny(ev.S, "\x00\r\n") {
			g.disp().Type(win, ev.S)
		}
	default:
		log.Printf("unknown input %q", ev.T)
	}
}

func (g *Game) hold(win x11.Window, k string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held == nil {
		g.held = map[string]*time.Timer{}
	}
	if t, ok := g.held[k]; ok {
		t.Reset(maxHold)
		return
	}
	g.disp().KeyDown(win, k)
	g.held[k] = time.AfterFunc(maxHold, func() { g.release(k) })
}

func (g *Game) release(k string) {
	g.mu.Lock()
	t, ok := g.held[k]
	delete(g.held, k)
	g.mu.Unlock()
	if ok {
		t.Stop()
		g.disp().KeyUp(k)
	}
}

// Snapshot returns one JPEG frame of the game window scaled to at most width pixels wide.
func Snapshot(ctx context.Context, display string, width int) ([]byte, error) {
	win, ok := x11.Display(display).Largest()
	if !ok || win.Area() <= 1 {
		return nil, errors.New("no game window")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-f", "x11grab", "-draw_mouse", "0",
		"-video_size", fmt.Sprintf("%dx%d", win.W, win.H), "-i", fmt.Sprintf("%s+%d,%d", display, win.X, win.Y),
		"-frames:v", "1", "-vf", fmt.Sprintf("scale='min(%d,iw)':-2", width), "-f", "mjpeg", "-q:v", "4", "-")
	cmd.Env = append(os.Environ(), "DISPLAY="+display)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	if len(out) < 4 || out[0] != 0xff || out[1] != 0xd8 {
		return nil, errors.New("ffmpeg returned no JPEG")
	}
	return out, nil
}

// Snapshot returns one JPEG frame of this game.
func (g *Game) Snapshot(ctx context.Context, width int) ([]byte, error) {
	return Snapshot(ctx, g.Display, width)
}

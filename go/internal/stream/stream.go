// Package stream serves one game window to a browser:
// video as MJPEG over multipart/x-mixed-replace (ffmpeg x11grab of the game window only),
// audio as Opus/WebM from the game's own PulseAudio sink, and input posted back as JSON
// and replayed with xdotool.
package stream

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
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
}

// Handler serves "", "video", "audio" and "input" relative to where it is mounted.
func (g *Game) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	mux.HandleFunc("GET /video", g.video)
	mux.HandleFunc("GET /audio", g.audio)
	mux.HandleFunc("POST /input", g.input)
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

func (g *Game) audio(w http.ResponseWriter, r *http.Request) {
	if g.Sink == "" {
		http.NotFound(w, r)
		return
	}
	cmd := exec.CommandContext(r.Context(), "ffmpeg", "-v", "error", "-f", "pulse", "-fragment_size", "1024",
		"-i", g.Sink+".monitor", "-ac", "2", "-c:a", "libopus", "-b:a", "96k", "-application", "audio",
		"-f", "webm", "-cluster_time_limit", "100", "-")
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
	w.Header().Set("Content-Type", "audio/webm")
	w.Header().Set("Cache-Control", "no-store")
	fl, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, err := out.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

// Input is one browser event.
type Input struct {
	T      string `json:"t"` // "click" or "key"
	X, Y   int
	B      int    `json:"b"`
	NoMove int    `json:"nomove"`
	K      string `json:"k"`
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
	case "key":
		k := ev.K
		if m, ok := keyMap[k]; ok {
			k = m
		}
		if keysymRe.MatchString(k) { // only plain keysyms reach xdotool
			g.disp().Key(win, k)
		}
	default:
		log.Printf("unknown input %q", ev.T)
	}
}

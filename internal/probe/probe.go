// Package probe checks whether a package runs: it starts the game exactly as `vnbox play`
// does (session: prefix preparation, private X display and sink, Managed=N restart on a 1x1
// window), plays an input sequence, takes screenshots and judges the result.
//
// Results go to results/<run>/: NN_<name>.png, wine.log, sizes.txt, summary.txt.
package probe

import (
	"bufio"
	"context"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"vnbox/internal/session"
	"vnbox/internal/wine"
	"vnbox/internal/x11"
)

// DefaultInput clicks the middle of an 800x600 window and presses Enter, six times.
const DefaultInput = "click:400,300 key:Return click:400,300 key:Return click:400,300 key:Return shot:a " +
	"click:400,300 key:Return click:400,300 key:Return click:400,300 key:Return shot:b"

// Options configure a probe.
type Options struct {
	Flavor wine.Flavor
	Prefix string        // prefix to run in; by default a new prefixes/probe-<run>, removed afterwards
	Run    string        // results/<Run>/ (default the package name)
	Input  string        // operations, see Run
	Wait   time.Duration // after starting, before the first screenshot (default 20s)
	Log    io.Writer
}

// Result is what a probe saw.
type Result struct {
	Dir      string   // results/<run>
	Exe      string   // started program
	Alive    bool     // the game still had a window at the end
	Mismatch bool     // Wine's client area is bigger than the X window (the picture is cut off)
	Sizes    string   // "client=WxH x11=WxH"
	Windows  []string // titles of the visible windows at the end
	Shots    []string // screenshots in order
	Verdict  string   // OK or NG:<reason>
}

// Run starts the package name and plays the operations in o.Input, separated by spaces:
//
//	click:x,y  click at (x, y) in the game window
//	key:K      press the X keysym K (Return, space, ctrl+s ...) in the game window
//	sleep:N    wait N seconds
//	shot:NAME  screenshot of the game window
//	pass:N     get past up to N small dialogs (launch settings, "fullscreen?") with Enter, then Space
func Run(ctx context.Context, name string, o Options) (*Result, error) {
	if o.Log == nil {
		o.Log = io.Discard
	}
	if o.Run == "" {
		o.Run = name
	}
	if o.Input == "" {
		o.Input = DefaultInput
	}
	if o.Wait == 0 {
		o.Wait = 20 * time.Second
	}
	r := &Result{Dir: filepath.Join(wine.Root(), "results", o.Run)}
	os.RemoveAll(r.Dir)
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return nil, err
	}
	// never the player's prefix (prefixes/run-<name>): the probe clicks through the game and
	// would change the saves
	if o.Prefix == "" {
		o.Prefix = filepath.Join(wine.Root(), "prefixes", "probe-"+o.Run)
		os.RemoveAll(o.Prefix)
		defer os.RemoveAll(o.Prefix)
	}
	s, err := session.Start(ctx, name, session.Options{Flavor: o.Flavor, Prefix: o.Prefix, Log: o.Log,
		WineLog: filepath.Join(r.Dir, "wine.log"), Debug: "err+all,fixme-all"})
	if err != nil {
		os.WriteFile(filepath.Join(r.Dir, "summary.txt"), []byte("start failed: "+err.Error()+"\n"), 0o644)
		r.Verdict = "NG:start(" + Short(err.Error()) + ")"
		return r, nil
	}
	defer s.Stop()
	os.WriteFile(filepath.Join(r.Dir, "display.txt"), []byte(s.Display+"\n"), 0o644)
	_, r.Exe = s.Launch().GameDir(s.Prefix)
	d := x11.Display(s.Display)
	sleep(ctx, o.Wait)
	if !s.Launch().IsHTML() { // the session restarts a 1x1 game with Managed=N after 20-40 seconds
		for i := 0; i < 8; i++ {
			if w, ok := gameWindow(d); ok && w.Area() > 1 || !s.Running() {
				break
			}
			sleep(ctx, 5*time.Second)
		}
	}
	p := &player{d: d, dir: r.Dir}
	p.shot("boot")
	for _, op := range strings.Fields(o.Input) {
		if ctx.Err() != nil {
			break
		}
		p.do(ctx, op)
	}
	r.Shots = p.shots
	_, hasWin := gameWindow(d)
	r.Alive = s.Running() && hasWin
	for _, w := range d.Windows() {
		if w.Name != "" {
			r.Windows = append(r.Windows, w.Name)
		}
	}
	if r.Alive {
		r.Mismatch, r.Sizes = sizes(ctx, s.Env(), d, o.Log)
		os.WriteFile(filepath.Join(r.Dir, "sizes.txt"), []byte(r.Sizes+"\n"), 0o644)
	}
	s.Stop()
	r.Verdict = Judge(r)
	r.writeSummary()
	return r, nil
}

// Short is the first line of an error message, at most 70 characters, for one TSV cell.
func Short(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	s = strings.ReplaceAll(s, "\t", " ")
	if r := []rune(s); len(r) > 70 {
		s = string(r[:70])
	}
	return s
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// gameWindow is the biggest window that is not Wine's desktop.
func gameWindow(d x11.Display) (x11.Window, bool) {
	var best x11.Window
	for _, w := range d.Windows() {
		if w.Name == "" || strings.Contains(w.Name, "デスクトップ") || strings.Contains(w.Name, "Desktop") {
			continue
		}
		if w.Area() > best.Area() {
			best = w
		}
	}
	return best, best.ID != ""
}

// dialog is the smallest dialog-sized window (under 400,000 pixels): launch settings and
// confirmations that show up in front of the game window.
func dialog(d x11.Display) (x11.Window, bool) {
	var best x11.Window
	for _, w := range d.Windows() {
		if w.Name == "" || strings.Contains(w.Name, "デスクトップ") || strings.Contains(w.Name, "Desktop") {
			continue
		}
		if w.Area() < 400000 && w.W >= 120 && w.H >= 40 && (best.ID == "" || w.Area() < best.Area()) {
			best = w
		}
	}
	return best, best.ID != ""
}

type player struct {
	d     x11.Display
	dir   string
	n     int
	shots []string
}

func (p *player) shot(label string) {
	p.n++
	path := filepath.Join(p.dir, fmt.Sprintf("%02d_%s.png", p.n, label))
	w, ok := gameWindow(p.d)
	if !ok {
		w = x11.Window{W: 1920, H: 1080} // whole screen
		if xs, err := p.d.ScreenSize(); err == nil {
			w.W, w.H = xs[0], xs[1]
		}
	}
	if p.d.Shot(path, w) == nil {
		p.shots = append(p.shots, path)
	}
}

func (p *player) do(ctx context.Context, op string) {
	kind, arg, _ := strings.Cut(op, ":")
	switch kind {
	case "click":
		var x, y int
		if _, err := fmt.Sscanf(arg, "%d,%d", &x, &y); err != nil {
			return
		}
		if w, ok := gameWindow(p.d); ok {
			p.d.Click(w, x, y, 1)
		}
		sleep(ctx, 1200*time.Millisecond)
	case "key":
		if w, ok := gameWindow(p.d); ok {
			p.d.Key(w, arg)
		}
		sleep(ctx, 1200*time.Millisecond)
	case "sleep":
		n, _ := strconv.ParseFloat(arg, 64)
		sleep(ctx, time.Duration(n*float64(time.Second)))
	case "shot":
		p.shot(arg)
	case "pass":
		n, _ := strconv.Atoi(arg)
		p.pass(ctx, n)
	}
}

// pass presses the default button (Enter, then Space) of small dialogs. Confirmations can come
// one after another a few seconds apart, so it stops only after three checks without one.
func (p *player) pass(ctx context.Context, n int) {
	empty := 0
	for i := 0; i < 2*n && ctx.Err() == nil; i++ {
		w, ok := dialog(p.d)
		if !ok {
			empty++
			if _, game := gameWindow(p.d); empty >= 3 && game {
				return
			}
			sleep(ctx, 3*time.Second)
			continue
		}
		empty = 0
		p.d.ActivateKey(w, "Return")
		sleep(ctx, 4*time.Second)
		if w2, ok := dialog(p.d); ok && w2.ID == w.ID {
			p.d.ActivateKey(w, "space")
			sleep(ctx, 4*time.Second)
		}
	}
}

// sizes compares the biggest client area Wine reports (runtime/tools/winsize.exe) with the
// game's X window. A smaller X window means the window manager did not carry out a maximize /
// fullscreen request and the bottom-right of the picture is cut off.
func sizes(ctx context.Context, env wine.Env, d x11.Display, log io.Writer) (bool, string) {
	exe, err := wine.Tool("winsize")
	if err != nil {
		fmt.Fprintln(log, err)
		return false, ""
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c := env.Command(ctx, "", exe)
	c.WaitDelay = 2 * time.Second
	out, _ := c.Output()
	cw, ch := 0, 0
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 6 {
			continue
		}
		w, _ := strconv.Atoi(f[4])
		h, _ := strconv.Atoi(f[5])
		if w*h > cw*ch {
			cw, ch = w, h
		}
	}
	xw, _ := gameWindow(d)
	s := fmt.Sprintf("client=%dx%d x11=%dx%d", cw, ch, xw.W, xw.H)
	return cw > 0 && xw.ID != "" && (cw-xw.W > 16 || ch-xw.H > 16), s
}

var (
	crashRe = regexp.MustCompile(`Unhandled exception|page fault`)
	stallRe = regexp.MustCompile(`RtlpWaitForCriticalSection`) // a thread waited 60 s for a lock
)

// Judge decides OK or NG:<reason>: the game must still be running, its window must not be cut
// off, Wine must not have reported a crash, and the last screenshot must be at least 400x300
// with some contrast in its middle 80% (not a blank or black screen).
func Judge(r *Result) string {
	if !r.Alive {
		return "NG:dead"
	}
	if r.Mismatch {
		return "NG:size_mismatch(" + r.Sizes + ")"
	}
	log, _ := os.ReadFile(filepath.Join(r.Dir, "wine.log"))
	if crashRe.Match(log) {
		return "NG:crash"
	}
	if len(r.Shots) < 2 {
		return "NG:noshot"
	}
	// a long wait for a lock is a hang only if the game stopped drawing: some games log it
	// while loading and go on (QLIE)
	if stallRe.Match(log) && sameImage(r.Shots[len(r.Shots)-2], r.Shots[len(r.Shots)-1]) {
		return "NG:hang"
	}
	img, err := loadImage(r.Shots[len(r.Shots)-1])
	if err != nil {
		return "NG:noshot"
	}
	if b := img.Bounds(); b.Dx()*b.Dy() < 120000 {
		return "NG:small_window"
	}
	if sd := contrast(img); sd <= 0.05 {
		return fmt.Sprintf("NG:blank(%.3f)", sd)
	}
	return "OK"
}

// sameImage reports whether two screenshots have the same pixels.
func sameImage(a, b string) bool {
	ia, err1 := loadImage(a)
	ib, err2 := loadImage(b)
	if err1 != nil || err2 != nil || ia.Bounds() != ib.Bounds() {
		return false
	}
	r := ia.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if ia.At(x, y) != ib.At(x, y) {
				return false
			}
		}
	}
	return true
}

func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// contrast is the standard deviation of the luminance (0..1) over the middle 80% of the image.
func contrast(img image.Image) float64 {
	b := img.Bounds()
	x0, x1 := b.Min.X+b.Dx()/10, b.Max.X-b.Dx()/10
	y0, y1 := b.Min.Y+b.Dy()/10, b.Max.Y-b.Dy()/10
	var sum, sq, n float64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			l := (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(bl)) / 65535
			sum += l
			sq += l * l
			n++
		}
	}
	if n == 0 {
		return 0
	}
	m := sum / n
	return math.Sqrt(math.Max(0, sq/n-m*m))
}

var errLineRe = regexp.MustCompile(`:err:|Unhandled|not found|could not load|Could not`)
var tidRe = regexp.MustCompile(`^[0-9a-f]+:`)

// writeSummary writes summary.txt: the verdict facts, the windows, and Wine's most frequent errors.
func (r *Result) writeSummary() {
	alive, mism := "no", "no"
	if r.Alive {
		alive = "yes"
	}
	if r.Mismatch {
		mism = "yes"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "verdict=%s exe=%s alive_after=%s size_mismatch=%s %s\n", r.Verdict, r.Exe, alive, mism, r.Sizes)
	fmt.Fprintf(&sb, "windows=%s|\n", strings.Join(r.Windows, "|"))
	sb.WriteString("--- top errors ---\n")
	if f, err := os.Open(filepath.Join(r.Dir, "wine.log")); err == nil {
		count := map[string]int{}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			l := sc.Text()
			if errLineRe.MatchString(l) && !strings.Contains(l, "winebth") {
				count[tidRe.ReplaceAllString(l, "")]++
			}
		}
		f.Close()
		lines := make([]string, 0, len(count))
		for l := range count {
			lines = append(lines, l)
		}
		sort.Slice(lines, func(i, j int) bool { return count[lines[i]] > count[lines[j]] })
		for i, l := range lines {
			if i == 12 {
				break
			}
			fmt.Fprintf(&sb, "%7d %s\n", count[l], l)
		}
	}
	os.WriteFile(filepath.Join(r.Dir, "summary.txt"), []byte(sb.String()), 0o644)
}

// Package x11 drives windows on a headless X display through xdotool / ImageMagick.
// There is no window manager, so keyboard focus has to be set explicitly before each key press.
package x11

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Window is a visible top-level window.
type Window struct {
	ID         string
	Name       string
	X, Y, W, H int
}

// Area returns the window's pixel area.
func (w Window) Area() int { return w.W * w.H }

// Display wraps one X display such as ":123".
type Display string

func (d Display) env() []string { return append(os.Environ(), "DISPLAY="+string(d)) }

func (d Display) xdo(args ...string) string {
	c := exec.Command("xdotool", args...)
	c.Env = d.env()
	out, _ := c.Output()
	return string(out)
}

var geomRe = regexp.MustCompile(`(\w+)=(-?\d+)`)

// Windows lists visible windows, skipping Wine's invisible IME helper.
func (d Display) Windows() []Window {
	var out []Window
	for _, id := range strings.Fields(d.xdo("search", "--onlyvisible", "--name", ".")) {
		g := map[string]int{}
		for _, m := range geomRe.FindAllStringSubmatch(d.xdo("getwindowgeometry", "--shell", id), -1) {
			g[m[1]], _ = strconv.Atoi(m[2])
		}
		if g["WIDTH"] == 0 {
			continue
		}
		name := strings.TrimSpace(d.xdo("getwindowname", id))
		if name == "Default IME" {
			continue
		}
		out = append(out, Window{ID: id, Name: name, X: g["X"], Y: g["Y"], W: g["WIDTH"], H: g["HEIGHT"]})
	}
	return out
}

// Largest returns the biggest visible window (the game window), if any.
func (d Display) Largest() (Window, bool) {
	var best Window
	for _, w := range d.Windows() {
		if w.Area() > best.Area() {
			best = w
		}
	}
	return best, best.ID != ""
}

// Click moves the pointer to (x, y) relative to the window and clicks button.
func (d Display) Click(w Window, x, y, button int) {
	d.xdo("mousemove", "--window", w.ID, strconv.Itoa(x), strconv.Itoa(y))
	time.Sleep(50 * time.Millisecond)
	d.xdo("click", strconv.Itoa(button))
}

// Button clicks at the current pointer position (wheel = 4/5, right = 3).
func (d Display) Button(button int) { d.xdo("click", strconv.Itoa(button)) }

// Key focuses the window and sends an X keysym (e.g. "Return", "space", "ctrl").
func (d Display) Key(w Window, keysym string) {
	d.xdo("windowfocus", w.ID)
	time.Sleep(100 * time.Millisecond)
	d.xdo("key", keysym)
}

// Shot saves a PNG of a screen rectangle. Grabbing from the root window works for
// D3D/GL windows that come out black with `import -window <id>`.
func (d Display) Shot(path string, w Window) error {
	c := exec.Command("import", "-window", "root", "-crop",
		strconv.Itoa(w.W)+"x"+strconv.Itoa(w.H)+"+"+strconv.Itoa(w.X)+"+"+strconv.Itoa(w.Y), "+repage", path)
	c.Env = d.env()
	return c.Run()
}

// ShotHash returns a hash of the window's pixels, to tell whether a key press changed anything.
func (d Display) ShotHash(w Window) string {
	c := exec.Command("import", "-window", "root", "-crop",
		strconv.Itoa(w.W)+"x"+strconv.Itoa(w.H)+"+"+strconv.Itoa(w.X)+"+"+strconv.Itoa(w.Y), "+repage", "png:-")
	c.Env = d.env()
	out, _ := c.Output()
	sum := sha1.Sum(out)
	return hex.EncodeToString(sum[:8])
}

func (d Display) visible(id string) bool {
	for _, w := range d.Windows() {
		if w.ID == id {
			return true
		}
	}
	return false
}

// dialogKeys are tried in order until the dialog changes: the default button, the focused
// button, then the usual mnemonics of Japanese installers: 次へ(N) インストール(I) はい(Y) 完了(F).
var dialogKeys = []string{"Return", "space", "alt+n", "alt+i", "alt+y", "alt+f"}

// PressDialogs walks installer wizards and launcher dialogs: for the newest small window it
// presses dialogKeys until the window shows a screen not seen before (a focus rectangle moving
// back and forth does not count) or closes. Dialogs that ignore the keyboard get a click where
// most wizards put 次へ / OK. It stops after `idle` checks without a dialog, after max dialogs,
// or when a dialog ignores everything ten times in a row.
// Every dialog is passed to onStep (for screenshots / logging) before it is pressed.
func (d Display) PressDialogs(max, idle int, onStep func(i int, w Window)) int {
	quiet, n, stuck := 0, 0, 0
	seen := map[string]bool{}
	for n < max {
		time.Sleep(3 * time.Second)
		var dlg Window
		for _, w := range d.Windows() {
			if w.Name != "" && w.W < 900 && w.H < 700 {
				dlg = w // later = newer
			}
		}
		if dlg.ID == "" {
			if quiet++; quiet >= idle {
				break
			}
			continue
		}
		quiet = 0
		// a dialog whose content changes by itself is showing progress: pressing Return
		// there would hit its only button, キャンセル
		// (progress bars can stand still for a few seconds, so watch for 6 seconds)
		h1, moving := d.ShotHash(dlg), false
		for j := 0; j < 3 && !moving; j++ {
			time.Sleep(2 * time.Second)
			moving = d.visible(dlg.ID) && d.ShotHash(dlg) != h1
		}
		if moving || !d.visible(dlg.ID) {
			continue
		}
		n++
		if onStep != nil {
			onStep(n, dlg)
		}
		seen[d.ShotHash(dlg)] = true
		// BGI's installer dialog ignores the keyboard; its インストールの開始 button is fixed
		if strings.HasPrefix(strings.ToUpper(dlg.Name), "BURIKO GENERAL INTERPRETER") && dlg.W == 460 {
			d.Click(dlg, 307, 216, 1)
			time.Sleep(3 * time.Second)
			continue
		}
		progressed := func() bool {
			if !d.visible(dlg.ID) {
				return true
			}
			h := d.ShotHash(dlg)
			if seen[h] {
				return false
			}
			seen[h] = true
			return true
		}
		changed := false
		for _, k := range dialogKeys {
			d.Key(dlg, k)
			time.Sleep(2 * time.Second)
			if changed = progressed(); changed {
				break
			}
		}
		if !changed {
			// keyboard ignored: click 次へ (bottom bar, left of キャンセル) or a centred OK
			// 次へ in wizards, 起動 in launch-setting dialogs (QLIE etc.), a centred OK
			for _, pt := range [][2]int{{dlg.W - 170, dlg.H - 22}, {dlg.W - 150, dlg.H - 14}, {dlg.W / 2, dlg.H - 25}} {
				d.Click(dlg, pt[0], pt[1], 1)
				time.Sleep(2 * time.Second)
				if changed = progressed(); changed {
					break
				}
			}
		}
		if changed {
			stuck = 0
		} else if stuck++; stuck >= 10 {
			break
		}
	}
	return n
}

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
	time.Sleep(80 * time.Millisecond)
	d.press(button)
}

// press holds the button for a moment: games that poll the mouse state once per frame
// (Eushully AGE etc.) miss xdotool's instantaneous click.
func (d Display) press(button int) {
	b := strconv.Itoa(button)
	d.xdo("mousedown", b)
	time.Sleep(60 * time.Millisecond)
	d.xdo("mouseup", b)
}

// Button clicks at the current pointer position (wheel = 4/5, right = 3).
func (d Display) Button(button int) {
	if button >= 4 { // wheel
		d.xdo("click", strconv.Itoa(button))
		return
	}
	d.press(button)
}

// KeyDown / KeyUp hold and release a key (skip with ctrl held, etc.).
func (d Display) KeyDown(w Window, keysym string) {
	d.xdo("windowfocus", w.ID)
	d.xdo("keydown", keysym)
}

func (d Display) KeyUp(keysym string) { d.xdo("keyup", keysym) }

// Type enters text (name entry and the like) into the focused window.
func (d Display) Type(w Window, text string) {
	d.xdo("windowfocus", w.ID)
	time.Sleep(100 * time.Millisecond)
	d.xdo("type", "--delay", "40", "--", text)
}

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

// dialogKeys are tried in order until the dialog changes: the default button, 次へ(N), clicks
// on the usual button spots, the focused button, then インストール(I) はい(Y) 完了(F).
var dialogKeys = []string{"Return", "alt+n", "click", "space", "alt+i", "alt+y", "alt+f"}

// PressDialogs walks installer wizards and launcher dialogs: for the newest small window it
// presses dialogKeys until the window shows a screen not seen before (a focus rectangle moving
// back and forth does not count) or closes. Dialogs that ignore the keyboard get a click where
// most wizards put 次へ / OK. It stops after `idle` checks without a dialog, after max dialogs,
// or when a dialog ignores everything ten times in a row.
// Every dialog is passed to onStep (for screenshots / logging) before it is pressed.
//
// pressButton, when set, is tried first on each dialog: it clicks a labelled button
// (インストール, 次へ, 同意する, OK ...) from inside Wine and reports whether it found one.
func (d Display) PressDialogs(max, idle int, onStep func(i int, w Window), pressButton func() bool) int {
	quiet, n, stuck, blind := 0, 0, 0, 0
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
			// a large custom-drawn installer menu (BGI 960x540) is not a dialog by size:
			// let pressButton recognise it by its window class
			if pressButton != nil && blind < 6 && len(d.Windows()) > 0 && pressButton() {
				blind++
				quiet = 0
				time.Sleep(3 * time.Second)
				continue
			}
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
			// a blinking text caret (an install folder field) also changes the picture: a page
			// with a labelled 次へ / インストール button is a wizard page, not a progress bar.
			// pressButton never presses キャンセル / 中止, so it is safe to try
			if moving && pressButton != nil && blind < 12 && pressButton() {
				blind++
				time.Sleep(3 * time.Second)
			}
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
		if pressButton != nil && pressButton() {
			time.Sleep(3 * time.Second)
			changed = progressed()
		}
		for _, k := range dialogKeys {
			if changed {
				break
			}
			if k != "click" {
				d.Key(dlg, k)
				time.Sleep(2 * time.Second)
				changed = progressed()
			} else {
				// keyboard ignored (or Space would only toggle a checkbox): click where
				// wizards put 次へ, where launch-setting dialogs (QLIE) put 起動, a centred OK
				for _, pt := range [][2]int{{dlg.W - 170, dlg.H - 22}, {dlg.W - 150, dlg.H - 14}, {dlg.W / 2, dlg.H - 25}} {
					d.Click(dlg, pt[0], pt[1], 1)
					time.Sleep(2 * time.Second)
					if changed = progressed(); changed {
						break
					}
				}
			}
			if changed {
				break
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

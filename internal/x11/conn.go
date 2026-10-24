package x11

import (
	"io"
	"log"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// Conn is a long-lived connection to one display for the stream's hot path: finding the game
// window and replaying the player's input through XTEST, without starting an xdotool process
// (and its own X connection) for every click and key. Keysyms it does not know and text entry
// still go through xdotool.
type Conn struct {
	d    Display
	c    *xgb.Conn
	root xproto.Window
	dead atomic.Bool

	netName, utf8 xproto.Atom

	keyMu   sync.Mutex
	keymap  map[xproto.Keysym]keyPos
	shiftKC xproto.Keycode

	winMu sync.Mutex
	win   Window
	winOK bool
	winAt time.Time
}

type keyPos struct {
	code    xproto.Keycode
	shifted bool // the keysym is on the shifted level of the key
}

// MoveSettle is how long a game gets to see the pointer arrive before the button goes down
// (hover effects, engines that read the cursor once per frame). It is skipped when the pointer
// is already there, which is the usual case for the next click on the same text box.
var MoveSettle = 40 * time.Millisecond

// winCache is how long Largest reuses its last answer: every input event asks for it.
const winCache = 250 * time.Millisecond

func init() { xgb.Logger = log.New(io.Discard, "", 0) } // it logs every connection without an .Xauthority

// Open connects to the display and checks for XTEST.
func Open(d Display) (*Conn, error) {
	c, err := xgb.NewConnDisplay(string(d))
	if err != nil {
		return nil, err
	}
	if err := xtest.Init(c); err != nil {
		c.Close()
		return nil, err
	}
	x := &Conn{d: d, c: c, root: xproto.Setup(c).DefaultScreen(c).Root}
	x.netName = x.atom("_NET_WM_NAME")
	x.utf8 = x.atom("UTF8_STRING")
	go func() { // nothing is selected, but errors of unchecked requests arrive as events: drain them
		for {
			ev, err := c.WaitForEvent()
			if ev == nil && err == nil {
				x.dead.Store(true)
				return
			}
		}
	}()
	return x, nil
}

// Alive reports whether the connection still works (the X server has not gone away).
func (x *Conn) Alive() bool { return !x.dead.Load() }

func (x *Conn) Close() { x.dead.Store(true); x.c.Close() }

func (x *Conn) atom(name string) xproto.Atom {
	r, err := xproto.InternAtom(x.c, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0
	}
	return r.Atom
}

// Largest returns the biggest visible named window, like Display.Largest.
func (x *Conn) Largest() (Window, bool) {
	x.winMu.Lock()
	defer x.winMu.Unlock()
	if time.Since(x.winAt) < winCache {
		return x.win, x.winOK
	}
	var best Window
	for _, w := range x.Windows() {
		if w.Area() > best.Area() {
			best = w
		}
	}
	x.win, x.winOK, x.winAt = best, best.ID != "", time.Now()
	return x.win, x.winOK
}

// Windows lists the viewable windows with a title (Display.Windows without xdotool).
func (x *Conn) Windows() []Window {
	var out []Window
	var walk func(xproto.Window)
	walk = func(parent xproto.Window) {
		tree, err := xproto.QueryTree(x.c, parent).Reply()
		if err != nil {
			return
		}
		// send every request for the children first, then read the replies
		attrs := make([]xproto.GetWindowAttributesCookie, len(tree.Children))
		for i, w := range tree.Children {
			attrs[i] = xproto.GetWindowAttributes(x.c, w)
		}
		var shown []xproto.Window
		for i, w := range tree.Children {
			if a, err := attrs[i].Reply(); err == nil && a.MapState == xproto.MapStateViewable {
				shown = append(shown, w)
			}
		}
		for _, w := range shown {
			if name := x.name(w); name != "" && name != "Default IME" {
				g, err := xproto.GetGeometry(x.c, xproto.Drawable(w)).Reply()
				t, err2 := xproto.TranslateCoordinates(x.c, w, x.root, 0, 0).Reply()
				if err == nil && err2 == nil && g.Width > 0 {
					out = append(out, Window{ID: strconv.FormatUint(uint64(w), 10), Name: name,
						X: int(t.DstX), Y: int(t.DstY), W: int(g.Width), H: int(g.Height)})
				}
			}
			walk(w)
		}
	}
	walk(x.root)
	return out
}

func (x *Conn) name(w xproto.Window) string {
	if x.netName != 0 {
		if p, err := xproto.GetProperty(x.c, false, w, x.netName, x.utf8, 0, 256).Reply(); err == nil && len(p.Value) > 0 {
			return string(p.Value)
		}
	}
	p, err := xproto.GetProperty(x.c, false, w, xproto.AtomWmName, xproto.GetPropertyTypeAny, 0, 256).Reply()
	if err != nil {
		return ""
	}
	return string(p.Value)
}

func wid(w Window) xproto.Window {
	n, _ := strconv.ParseUint(w.ID, 10, 32)
	return xproto.Window(n)
}

func (x *Conn) fake(typ byte, detail byte, rx, ry int16) {
	xtest.FakeInput(x.c, typ, detail, 0, x.root, rx, ry, 0)
}

// Move puts the pointer at (x, y) inside the window.
func (x *Conn) Move(w Window, px, py int) {
	x.fake(xproto.MotionNotify, 0, int16(w.X+px), int16(w.Y+py))
	x.c.Sync()
}

// Click moves the pointer to (x, y) relative to the window and clicks button.
func (x *Conn) Click(w Window, px, py, button int) {
	rx, ry := w.X+px, w.Y+py
	if p, err := xproto.QueryPointer(x.c, x.root).Reply(); err != nil || int(p.RootX) != rx || int(p.RootY) != ry {
		x.fake(xproto.MotionNotify, 0, int16(rx), int16(ry))
		x.c.Sync()
		time.Sleep(MoveSettle)
	}
	x.press(button)
}

// press holds the button for a moment, like Display.press.
func (x *Conn) press(button int) {
	x.fake(xproto.ButtonPress, byte(button), 0, 0)
	x.c.Sync()
	time.Sleep(60 * time.Millisecond)
	x.fake(xproto.ButtonRelease, byte(button), 0, 0)
	x.c.Sync()
}

// Button clicks at the current pointer position (wheel = 4/5, right = 3).
func (x *Conn) Button(button int) {
	if button >= 4 {
		x.fake(xproto.ButtonPress, byte(button), 0, 0)
		x.fake(xproto.ButtonRelease, byte(button), 0, 0)
		x.c.Sync()
		return
	}
	x.press(button)
}

// focus gives the window the keyboard focus unless it (or a child of it) has it already;
// only an actual change waits for the game to notice.
func (x *Conn) focus(w Window) {
	id := wid(w)
	if f, err := xproto.GetInputFocus(x.c).Reply(); err == nil {
		for win, i := f.Focus, 0; win != 0 && win != x.root && i < 16; i++ {
			if win == id {
				return
			}
			t, err := xproto.QueryTree(x.c, win).Reply()
			if err != nil {
				break
			}
			win = t.Parent
		}
	}
	xproto.SetInputFocus(x.c, xproto.InputFocusParent, id, xproto.TimeCurrentTime)
	x.c.Sync()
	time.Sleep(100 * time.Millisecond)
}

// key finds the keycode for a keysym name; ok is false for names this table does not cover.
func (x *Conn) key(name string) (keyPos, bool) {
	sym, ok := keysymByName(name)
	if !ok {
		return keyPos{}, false
	}
	x.keyMu.Lock()
	defer x.keyMu.Unlock()
	if x.keymap == nil {
		x.loadKeymap()
	}
	p, ok := x.keymap[sym]
	return p, ok
}

func (x *Conn) loadKeymap() {
	x.keymap = map[xproto.Keysym]keyPos{}
	s := xproto.Setup(x.c)
	n := byte(s.MaxKeycode - s.MinKeycode + 1)
	m, err := xproto.GetKeyboardMapping(x.c, s.MinKeycode, n).Reply()
	if err != nil {
		return
	}
	per := int(m.KeysymsPerKeycode)
	for i := 0; i < int(n); i++ {
		code := xproto.Keycode(int(s.MinKeycode) + i)
		for lvl := 0; lvl < per && lvl < 2; lvl++ {
			sym := m.Keysyms[i*per+lvl]
			if _, seen := x.keymap[sym]; sym != 0 && !seen {
				x.keymap[sym] = keyPos{code, lvl == 1}
			}
		}
	}
	if p, ok := x.keymap[0xffe1]; ok { // Shift_L
		x.shiftKC = p.code
	}
}

func (x *Conn) keyEvent(p keyPos, down bool) {
	typ := byte(xproto.KeyRelease)
	if down {
		typ = xproto.KeyPress
	}
	if p.shifted && x.shiftKC != 0 && down {
		x.fake(xproto.KeyPress, byte(x.shiftKC), 0, 0)
	}
	x.fake(typ, byte(p.code), 0, 0)
	if p.shifted && x.shiftKC != 0 && !down {
		x.fake(xproto.KeyRelease, byte(x.shiftKC), 0, 0)
	}
	x.c.Sync()
}

// Key focuses the window and taps a keysym ("Return", "space", "ctrl", "a" ...).
func (x *Conn) Key(w Window, keysym string) {
	p, ok := x.key(keysym)
	if !ok {
		x.d.Key(w, keysym)
		return
	}
	x.focus(w)
	x.keyEvent(p, true)
	time.Sleep(20 * time.Millisecond) // games that poll the key state once per frame
	x.keyEvent(p, false)
}

// KeyDown / KeyUp hold and release a key.
func (x *Conn) KeyDown(w Window, keysym string) {
	p, ok := x.key(keysym)
	if !ok {
		x.d.KeyDown(w, keysym)
		return
	}
	x.focus(w)
	x.keyEvent(p, true)
}

func (x *Conn) KeyUp(keysym string) {
	p, ok := x.key(keysym)
	if !ok {
		x.d.KeyUp(keysym)
		return
	}
	x.keyEvent(p, false)
}

// Type enters text through xdotool, which can map any character.
func (x *Conn) Type(w Window, text string) { x.d.Type(w, text) }

// keysymByName covers the names the stream page sends (browser key names are mapped to X
// keysym names before they get here) plus xdotool's modifier aliases.
func keysymByName(name string) (xproto.Keysym, bool) {
	if len(name) == 1 {
		c := name[0]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			return xproto.Keysym(c), true
		}
	}
	if len(name) >= 2 && len(name) <= 3 && name[0] == 'F' {
		if n, err := strconv.Atoi(name[1:]); err == nil && n >= 1 && n <= 12 {
			return xproto.Keysym(0xffbe + n - 1), true
		}
	}
	s, ok := namedKeysyms[name]
	return s, ok
}

var namedKeysyms = map[string]xproto.Keysym{
	"space": 0x20, "Return": 0xff0d, "Escape": 0xff1b, "BackSpace": 0xff08, "Tab": 0xff09, "Delete": 0xffff,
	"Insert": 0xff63, "Home": 0xff50, "End": 0xff57, "Prior": 0xff55, "Page_Up": 0xff55, "Next": 0xff56, "Page_Down": 0xff56,
	"Left": 0xff51, "Up": 0xff52, "Right": 0xff53, "Down": 0xff54,
	"ctrl": 0xffe3, "Control_L": 0xffe3, "Control_R": 0xffe4, "shift": 0xffe1, "Shift_L": 0xffe1, "Shift_R": 0xffe2,
	"alt": 0xffe9, "Alt_L": 0xffe9, "Alt_R": 0xffea, "super": 0xffeb, "Super_L": 0xffeb,
}

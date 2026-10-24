package x11

import (
	"errors"
	"fmt"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/damage"
	"github.com/jezek/xgb/shm"
	"github.com/jezek/xgb/xfixes"
	"github.com/jezek/xgb/xproto"
	"golang.org/x/sys/unix"
)

// Grabber captures one screen rectangle, and only when something was drawn there: XDAMAGE
// says when the screen changed and MIT-SHM copies the pixels into shared memory, so a still
// screen costs neither a capture nor an encode.
type Grabber struct {
	X, Y, W, H int

	c       *xgb.Conn
	root    xproto.Window
	seg     shm.Seg
	mem     []byte
	dmg     damage.Damage
	changed chan struct{}
}

// PixFmt is the ffmpeg pixel format of Grab's frames (32-bit little-endian BGRX).
const PixFmt = "bgr0"

// NewGrabber prepares to capture the rectangle of w (clipped to the screen).
func NewGrabber(d Display, w Window) (*Grabber, error) {
	c, err := xgb.NewConnDisplay(string(d))
	if err != nil {
		return nil, err
	}
	g := &Grabber{c: c, changed: make(chan struct{}, 1)}
	if err := g.init(w); err != nil {
		g.Close()
		return nil, err
	}
	return g, nil
}

func (g *Grabber) init(w Window) error {
	for _, ext := range []func(*xgb.Conn) error{shm.Init, xfixes.Init, damage.Init} {
		if err := ext(g.c); err != nil {
			return err
		}
	}
	if _, err := xfixes.QueryVersion(g.c, 5, 0).Reply(); err != nil {
		return err
	}
	if _, err := damage.QueryVersion(g.c, 1, 1).Reply(); err != nil {
		return err
	}
	scr := xproto.Setup(g.c).DefaultScreen(g.c)
	g.root = scr.Root
	if scr.RootDepth != 24 && scr.RootDepth != 32 {
		return fmt.Errorf("screen depth %d", scr.RootDepth)
	}
	// clip to the screen: GetImage fails on a rectangle that leaves it
	x0, y0 := max(w.X, 0), max(w.Y, 0)
	x1, y1 := min(w.X+w.W, int(scr.WidthInPixels)), min(w.Y+w.H, int(scr.HeightInPixels))
	g.X, g.Y, g.W, g.H = x0, y0, (x1-x0)&^1, (y1-y0)&^1 // even, for 4:2:0
	if g.W <= 0 || g.H <= 0 {
		return errors.New("window is off screen")
	}

	size := g.W * g.H * 4
	id, err := unix.SysvShmGet(unix.IPC_PRIVATE, size, unix.IPC_CREAT|0o600)
	if err != nil {
		return fmt.Errorf("shmget: %w", err)
	}
	g.mem, err = unix.SysvShmAttach(id, 0, 0)
	if err != nil {
		unix.SysvShmCtl(id, unix.IPC_RMID, nil)
		return fmt.Errorf("shmat: %w", err)
	}
	seg, err := shm.NewSegId(g.c)
	if err == nil {
		g.seg = seg
		err = shm.AttachChecked(g.c, seg, uint32(id), false).Check()
	}
	unix.SysvShmCtl(id, unix.IPC_RMID, nil) // freed once both sides detach
	if err != nil {
		g.seg = 0
		return fmt.Errorf("shm attach: %w", err)
	}

	// damage on the root window covers everything drawn on the screen, child windows included
	if g.dmg, err = damage.NewDamageId(g.c); err != nil {
		return err
	}
	if err := damage.CreateChecked(g.c, g.dmg, xproto.Drawable(g.root), damage.ReportLevelNonEmpty).Check(); err != nil {
		g.dmg = 0
		return err
	}
	go g.events()
	g.signal() // the first frame
	return nil
}

func (g *Grabber) events() {
	for {
		ev, err := g.c.WaitForEvent()
		if ev == nil && err == nil { // connection closed
			close(g.changed)
			return
		}
		// the game is the only thing on its display, so any change is taken as one in the window
		if _, ok := ev.(damage.NotifyEvent); ok {
			g.signal()
		}
	}
}

func (g *Grabber) signal() {
	select {
	case g.changed <- struct{}{}:
	default:
	}
}

// Changed receives when the rectangle may have changed since the last Grab; it is closed when
// the X server goes away.
func (g *Grabber) Changed() <-chan struct{} { return g.changed }

// Grab copies the rectangle's pixels and returns them (W*H*4 bytes of BGRX). The slice is
// shared memory, valid until the next Grab.
func (g *Grabber) Grab() ([]byte, error) {
	// clear the damage first: drawing during the copy then reports again
	damage.Subtract(g.c, g.dmg, 0, 0)
	_, err := shm.GetImage(g.c, xproto.Drawable(g.root), int16(g.X), int16(g.Y), uint16(g.W), uint16(g.H),
		0xffffffff, xproto.ImageFormatZPixmap, g.seg, 0).Reply()
	if err != nil {
		return nil, err
	}
	return g.mem, nil
}

func (g *Grabber) Close() {
	if g.dmg != 0 {
		damage.Destroy(g.c, g.dmg)
	}
	if g.seg != 0 {
		shm.Detach(g.c, g.seg)
	}
	g.c.Sync()
	g.c.Close()
	if g.mem != nil {
		unix.SysvShmDetach(g.mem)
		g.mem = nil
	}
}

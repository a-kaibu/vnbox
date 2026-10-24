package x11

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

var nextDisplay = 90 + os.Getpid()%400

// xvfb starts a throwaway X server, or skips the test where there is none. -noreset: without it
// the server resets when its last client leaves, and the next connection may hit the reset.
func xvfb(t *testing.T) Display {
	t.Helper()
	if _, err := exec.LookPath("Xvfb"); err != nil {
		t.Skip("no Xvfb")
	}
	nextDisplay++
	d := Display(fmt.Sprintf(":%d", nextDisplay))
	cmd := exec.Command("Xvfb", string(d), "-screen", "0", "640x480x24", "-nolisten", "tcp", "-noreset")
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { cmd.Process.Signal(os.Interrupt); cmd.Wait() })
	for i := 0; i < 50; i++ {
		if c, err := xgb.NewConnDisplay(string(d)); err == nil {
			c.Close()
			return d
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Skip("Xvfb did not start")
	return ""
}

// window maps a named 200x100 window at (40, 30) and returns it with its connection.
func window(t *testing.T, d Display) (*xgb.Conn, xproto.Window) {
	c, err := xgb.NewConnDisplay(string(d))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	scr := xproto.Setup(c).DefaultScreen(c)
	w, _ := xproto.NewWindowId(c)
	xproto.CreateWindow(c, scr.RootDepth, w, scr.Root, 40, 30, 200, 100, 0, xproto.WindowClassInputOutput, scr.RootVisual,
		xproto.CwBackPixel|xproto.CwEventMask, []uint32{0, xproto.EventMaskKeyPress | xproto.EventMaskKeyRelease | xproto.EventMaskButtonPress})
	xproto.ChangeProperty(c, xproto.PropModeReplace, w, xproto.AtomWmName, xproto.AtomString, 8, 4, []byte("game"))
	xproto.MapWindow(c, w)
	c.Sync()
	return c, w
}

func TestConnInput(t *testing.T) {
	d := xvfb(t)
	gc, gw := window(t, d)
	x, err := Open(d)
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	w, ok := x.Largest()
	if !ok || w.Name != "game" || w.X != 40 || w.Y != 30 || w.W != 200 || w.H != 100 {
		t.Fatalf("Largest = %+v %v", w, ok)
	}
	x.Click(w, 10, 20, 1)
	p, _ := xproto.QueryPointer(gc, gw).Reply()
	if p.WinX != 10 || p.WinY != 20 {
		t.Errorf("pointer at %d,%d", p.WinX, p.WinY)
	}
	x.Key(w, "A")
	x.Key(w, "Return")
	var got []string
	deadline := time.After(2 * time.Second)
	for len(got) < 4 {
		evc := make(chan xgb.Event, 1)
		go func() { ev, _ := gc.WaitForEvent(); evc <- ev }()
		select {
		case ev := <-evc:
			switch e := ev.(type) {
			case xproto.ButtonPressEvent:
				got = append(got, fmt.Sprintf("button%d", e.Detail))
			case xproto.KeyPressEvent:
				got = append(got, fmt.Sprintf("key%d/%d", e.Detail, e.State&xproto.ModMaskShift))
			case xproto.KeyReleaseEvent:
			}
		case <-deadline:
			t.Fatalf("events %v", got)
		}
	}
	// button 1, then Shift, the A key with Shift held, Return
	if got[0] != "button1" || got[3] != fmt.Sprintf("key%d/0", mustKey(t, x, "Return")) ||
		got[2] != fmt.Sprintf("key%d/%d", mustKey(t, x, "a"), xproto.ModMaskShift) {
		t.Errorf("events %v", got)
	}
}

func mustKey(t *testing.T, x *Conn, name string) xproto.Keycode {
	p, ok := x.key(name)
	if !ok {
		t.Fatalf("no key %s", name)
	}
	return p.code
}

func TestGrabber(t *testing.T) {
	d := xvfb(t)
	gc, gw := window(t, d)
	g, err := NewGrabber(d, Window{X: 40, Y: 30, W: 200, H: 100})
	if err != nil {
		t.Skip("no MIT-SHM / DAMAGE here:", err)
	}
	defer g.Close()
	wait := func(want bool) {
		t.Helper()
		select {
		case <-g.Changed():
			if !want {
				t.Fatal("change reported on a still screen")
			}
		case <-time.After(300 * time.Millisecond):
			if want {
				t.Fatal("no change reported")
			}
		}
	}
	wait(true) // the first frame
	time.Sleep(100 * time.Millisecond)
	select { // a new damage object reports the whole screen once
	case <-g.Changed():
	default:
	}
	if _, err := g.Grab(); err != nil {
		t.Fatal(err)
	}
	wait(false)
	// draw into the (child) window
	gcid, _ := xproto.NewGcontextId(gc)
	xproto.CreateGC(gc, gcid, xproto.Drawable(gw), xproto.GcForeground, []uint32{0x00ff8000})
	xproto.PolyFillRectangle(gc, xproto.Drawable(gw), gcid, []xproto.Rectangle{{X: 0, Y: 0, Width: 10, Height: 10}})
	gc.Sync()
	wait(true)
	px, err := g.Grab()
	if err != nil {
		t.Fatal(err)
	}
	if px[0] != 0x00 || px[1] != 0x80 || px[2] != 0xff { // BGRX
		t.Fatalf("pixel %x", px[:4])
	}
	if len(px) != 200*100*4 {
		t.Fatalf("frame %d bytes", len(px))
	}
	wait(false)
}

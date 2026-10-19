// Package wine sets up the environment the games run in: which Wine build,
// the Wine prefix, a headless X display and a PulseAudio sink per game.
package wine

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Flavor selects the Wine build.
type Flavor string

const (
	// Vnbox is the patched build in /opt/wine-vnbox (new WoW64: 32-bit apps run in a 64-bit prefix).
	Vnbox Flavor = "vnbox"
	// Classic is the distribution's Wine with a separate 32-bit prefix. Some copy protections
	// that inspect kernel32 only work here.
	Classic Flavor = "classic"
)

// Root is the vnbox data directory (prefixes/, packages/, results/ ...).
func Root() string {
	if r := os.Getenv("VNBOX"); r != "" {
		return r
	}
	return "/home/user/vnbox"
}

// Bin returns the directory holding wine/wineserver for the flavor ("" means $PATH).
func (f Flavor) Bin() string {
	if f == Vnbox {
		if d := os.Getenv("VNBOX_WINE"); d != "" {
			return d
		}
		return "/opt/wine-vnbox/bin"
	}
	return ""
}

// BasePrefix returns the base prefix every game prefix is cloned from.
func (f Flavor) BasePrefix(x64 bool) string {
	switch {
	case f == Vnbox:
		return filepath.Join(Root(), "prefixes", "base-vnbox")
	case x64:
		return filepath.Join(Root(), "prefixes", "base64")
	default:
		return filepath.Join(Root(), "prefixes", "base")
	}
}

// Env describes one Wine invocation context.
type Env struct {
	Flavor  Flavor
	Prefix  string
	Display string // ":123"
	Sink    string // PulseAudio sink name, optional
	Debug   string // WINEDEBUG, default "-all"
}

// Vars returns the process environment for wine.
func (e Env) Vars() []string {
	env := os.Environ()
	set := func(k, v string) {
		for i, kv := range env {
			if strings.HasPrefix(kv, k+"=") {
				env[i] = k + "=" + v
				return
			}
		}
		env = append(env, k+"="+v)
	}
	if b := e.Flavor.Bin(); b != "" {
		set("PATH", b+":"+os.Getenv("PATH"))
	}
	set("LANG", "ja_JP.UTF-8")
	set("LC_ALL", "ja_JP.UTF-8")
	// Some Japan-only titles check the time zone to decide whether Windows is Japanese.
	if os.Getenv("TZ") == "" {
		set("TZ", "Asia/Tokyo")
	}
	set("WINEPREFIX", e.Prefix)
	if e.Flavor == Vnbox || IsWin64(e.Prefix) {
		set("WINEARCH", "win64")
	} else {
		set("WINEARCH", "win32")
	}
	dbg := e.Debug
	if dbg == "" {
		dbg = "-all"
	}
	set("WINEDEBUG", dbg)
	if os.Getenv("WINEDLLOVERRIDES") == "" {
		set("WINEDLLOVERRIDES", "mscoree,mshtml=;winemenubuilder.exe=d")
	}
	if e.Display != "" {
		set("DISPLAY", e.Display)
	}
	if e.Sink != "" {
		set("PULSE_SINK", e.Sink)
	}
	return env
}

func (e Env) bin(name string) string {
	if b := e.Flavor.Bin(); b != "" {
		return filepath.Join(b, name)
	}
	return name
}

// Command builds a wine command running exe (a path) with dir as working directory.
func (e Env) Command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, e.bin("wine"), args...)
	c.Dir = dir
	c.Env = e.Vars()
	return c
}

// Run runs wine with args and waits; output is discarded unless out is set.
func (e Env) Run(ctx context.Context, out io.Writer, args ...string) error {
	c := e.Command(ctx, "", args...)
	c.Stdout, c.Stderr = out, out
	return c.Run()
}

// RegAdd sets a registry value through `wine reg add` (wineserver must be usable).
func (e Env) RegAdd(ctx context.Context, key, name, typ, data string) error {
	args := []string{"reg", "add", key, "/v", name, "/d", data, "/f"}
	if typ != "" {
		args = append(args, "/t", typ)
	}
	return e.Run(ctx, nil, args...)
}

// Kill stops every process of the prefix; Wait waits for wineserver to exit
// (and therefore for the registry to be flushed to disk).
func (e Env) Kill() { e.server("-k") }
func (e Env) Wait() { e.server("-w") }

func (e Env) server(arg string) {
	c := exec.Command(e.bin("wineserver"), arg)
	c.Env = e.Vars()
	_ = c.Run()
}

// IsWin64 reports whether the prefix was created as a 64-bit prefix.
func IsWin64(prefix string) bool {
	f, err := os.Open(filepath.Join(prefix, "system.reg"))
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	return strings.Contains(string(buf[:n]), "#arch=win64")
}

// DisplayFor returns a stable X display number for a name, in :100..:499.
func DisplayFor(name string) string {
	return fmt.Sprintf(":%d", 100+crc32.ChecksumIEEE([]byte(name))%400)
}

// FreeDisplay returns a display number for name that no other X server uses, starting
// from DisplayFor(name) and probing upwards. Sessions must not share a display, or two
// games would be captured and controlled together.
func FreeDisplay(name string) string {
	var n int
	fmt.Sscanf(DisplayFor(name), ":%d", &n)
	for i := 0; i < 400; i++ {
		d := 100 + (n-100+i)%400
		if _, err := os.Stat(fmt.Sprintf("/tmp/.X%d-lock", d)); err != nil {
			return fmt.Sprintf(":%d", d)
		}
	}
	return DisplayFor(name)
}

// SinkFor returns a stable PulseAudio sink name for a name.
func SinkFor(name string) string {
	return fmt.Sprintf("vn_%08x", crc32.ChecksumIEEE([]byte("sink:"+name)))
}

// EnsureX starts a headless X server (no window manager) on display if none is running.
func EnsureX(display string) error {
	if xUp(display) {
		return nil
	}
	n := strings.TrimPrefix(display, ":")
	lock := "/tmp/.X" + n + "-lock"
	if b, err := os.ReadFile(lock); err == nil { // stale lock from a crashed Xvfb
		var pid int
		fmt.Sscan(strings.TrimSpace(string(b)), &pid)
		if pid <= 0 || exec.Command("kill", "-0", fmt.Sprint(pid)).Run() != nil {
			os.Remove(lock)
			os.Remove("/tmp/.X11-unix/X" + n)
		}
	}
	size := os.Getenv("XVFB_SIZE")
	if size == "" {
		size = "1920x1080"
	}
	c := exec.Command("Xvfb", display, "-screen", "0", size+"x24", "-nolisten", "tcp")
	if err := c.Start(); err != nil {
		return err
	}
	go c.Wait()
	for i := 0; i < 50; i++ {
		if xUp(display) {
			startWM(display)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("Xvfb did not start on " + display)
}

// startWM runs a minimal window manager (openbox without decorations) on the display.
// Without one, Wine's requests to maximize or go fullscreen (_NET_WM_STATE) are never carried
// out: the game draws for 1920x1080 while its X window stays smaller (cut off) or 1x1.
// VNBOX_WM=none disables it.
func startWM(display string) {
	if os.Getenv("VNBOX_WM") == "none" {
		return
	}
	bin, err := exec.LookPath("openbox")
	if err != nil {
		return
	}
	c := exec.Command(bin, "--config-file", filepath.Join(Root(), "scripts", "wm", "openbox-rc.xml"))
	c.Env = append(os.Environ(), "DISPLAY="+display)
	if c.Start() == nil {
		go c.Wait()
		time.Sleep(500 * time.Millisecond)
	}
}

func xUp(display string) bool {
	return exec.Command("xdpyinfo", "-display", display).Run() == nil
}

// EnsureAudio starts PulseAudio if needed and creates a null sink named sink.
func EnsureAudio(sink string) error {
	if exec.Command("pactl", "info").Run() != nil {
		_ = exec.Command("pulseaudio", "-D", "--exit-idle-time=-1", "--disallow-exit", "-n",
			"--load=module-null-sink sink_name=null", "--load=module-native-protocol-unix").Run()
		time.Sleep(time.Second)
	}
	if sink == "" {
		return nil
	}
	out, err := exec.Command("pactl", "list", "short", "sinks").Output()
	if err != nil {
		return err
	}
	if strings.Contains(string(out), "\t"+sink+"\t") {
		return nil
	}
	return exec.Command("pactl", "load-module", "module-null-sink", "sink_name="+sink,
		"sink_properties=device.description="+sink).Run()
}

// ClonePrefix copies a base prefix using hard links, so a new prefix costs a few MB.
// The registry and the user profile are real copies because Wine rewrites them.
func ClonePrefix(src, dst string) error {
	if err := exec.Command("cp", "-al", src, dst).Run(); err != nil {
		return fmt.Errorf("clone %s: %w", src, err)
	}
	for _, f := range []string{"system.reg", "user.reg", "userdef.reg"} {
		os.Remove(filepath.Join(dst, f))
		if err := exec.Command("cp", "-a", filepath.Join(src, f), filepath.Join(dst, f)).Run(); err != nil {
			return err
		}
	}
	users := filepath.Join(dst, "drive_c", "users")
	os.RemoveAll(users)
	return exec.Command("cp", "-a", filepath.Join(src, "drive_c", "users"), users).Run()
}

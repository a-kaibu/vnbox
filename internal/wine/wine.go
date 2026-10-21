// Package wine sets up the environment the games run in: which Wine build,
// the Wine prefix, a headless X display and a PulseAudio sink per game.
package wine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/a-kaibu/vnbox"
)

// Flavor selects the Wine build.
type Flavor string

const (
	// Vnbox is the patched build (vnbox build-wine; new WoW64: 32-bit apps run in a 64-bit prefix).
	Vnbox Flavor = "vnbox"
	// Classic is the distribution's Wine with a separate 32-bit prefix. Some copy protections
	// that inspect kernel32 only work here.
	Classic Flavor = "classic"
)

// Root is the vnbox data directory (packages/, prefixes/, wine/, results/ ...): $VNBOX, or
// else ~/.local/share/vnbox ($XDG_DATA_HOME/vnbox).
func Root() string {
	if r := os.Getenv("VNBOX"); r != "" {
		return r
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "vnbox")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "vnbox")
}

// Bin returns the directory holding wine/wineserver for the flavor ("" means $PATH): for the
// patched build $VNBOX_WINE, or else what vnbox build-wine installed (<root>/wine), or else
// /opt/wine-vnbox.
func (f Flavor) Bin() string {
	if f == Vnbox {
		if d := os.Getenv("VNBOX_WINE"); d != "" {
			return d
		}
		if d := filepath.Join(Root(), "wine", "bin"); exists(filepath.Join(d, "wine")) {
			return d
		}
		return "/opt/wine-vnbox/bin"
	}
	return ""
}

// Installed reports an error when the flavor's Wine cannot be found.
func (f Flavor) Installed() error {
	if f == Vnbox {
		if !exists(filepath.Join(f.Bin(), "wine")) {
			return errors.New("the patched Wine is not installed: vnbox install-wine (or build-wine; or set VNBOX_WINE, or use -wine classic)")
		}
		return nil
	}
	if _, err := exec.LookPath("wine"); err != nil {
		return errors.New("wine is not installed (apt install wine)")
	}
	return nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Extract writes the file or folder name of the files built into vnbox (tools/..., config/...,
// wine-build/...) under <root>/runtime/src and returns its path there. Files are rewritten
// only when their content changed, so their times say when they last did.
func Extract(name string) (string, error) {
	extractMu.Lock()
	defer extractMu.Unlock()
	base := filepath.Join(Root(), "runtime", "src")
	err := fs.WalkDir(vnbox.Files, name, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := vnbox.Files.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(base, filepath.FromSlash(p))
		if old, err := os.ReadFile(dst); err == nil && bytes.Equal(old, b) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	return filepath.Join(base, filepath.FromSlash(name)), err
}

var extractMu sync.Mutex

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
	Display string   // ":123"
	Sink    string   // PulseAudio sink name, optional
	Debug   string   // WINEDEBUG, default "-all"
	Win64   bool     // create a 64-bit prefix (WINEARCH=win64) even with the classic Wine
	Sandbox bool     // run the program under bubblewrap (sandbox.go): for the game and its installer
	Network bool     // with Sandbox: keep the network (games that need online activation)
	Binds   []string // with Sandbox: more folders the program may read and write (an installer's own folder)
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
	if e.Flavor == Vnbox || e.Win64 || IsWin64(e.Prefix) {
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
		// mscoree を止めておくと Wine Mono の導入画面が出ない。ただし .NET (Core) を入れた prefix では
		// IL だけの DLL を読むのに mscoree が要る (止めると System.Runtime.dll が読めない)
		ov := "mscoree,mshtml=;winemenubuilder.exe=d"
		if st, err := os.Stat(filepath.Join(e.Prefix, "drive_c", "Program Files", "dotnet")); err == nil && st.IsDir() {
			ov = "mshtml=;winemenubuilder.exe=d"
		}
		set("WINEDLLOVERRIDES", ov)
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
	return e.command(ctx, dir, e.bin("wine"), args...)
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

// Reg runs `wine reg <args>` (add / delete / query ...).
func (e Env) Reg(ctx context.Context, args ...string) error {
	return e.Run(ctx, nil, append([]string{"reg"}, args...)...)
}

// ImportReg imports a .reg file's text (REGEDIT4) with regedit: into the native view and/or,
// in a 64-bit prefix, the 32-bit view (syswow64\regedit.exe, WOW6432Node) where 32-bit
// programs look. In a 32-bit prefix the native view is the only one.
func (e Env) ImportReg(ctx context.Context, text string, native, wow32 bool) error {
	win64 := e.Flavor == Vnbox || e.Win64 || IsWin64(e.Prefix)
	if !win64 {
		native, wow32 = true, false
	}
	dir := filepath.Join(e.Prefix, "drive_c", "vnbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f := filepath.Join(dir, "import.reg")
	if err := os.WriteFile(f, []byte(strings.ReplaceAll(text, "\n", "\r\n")), 0o644); err != nil {
		return err
	}
	defer os.Remove(f)
	if native {
		if err := e.Run(ctx, nil, "regedit", "/S", `C:\vnbox\import.reg`); err != nil {
			return err
		}
	}
	if wow32 {
		return e.Run(ctx, nil, `C:\windows\syswow64\regedit.exe`, "/S", `C:\vnbox\import.reg`)
	}
	return nil
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
//
// A display handed out stays reserved in this process until StopX, so that two games
// started at the same moment (vnbox verify -j, the portal) do not both pick it before
// either X server has created its lock file.
func FreeDisplay(name string) string {
	var n int
	fmt.Sscanf(DisplayFor(name), ":%d", &n)
	displayMu.Lock()
	defer displayMu.Unlock()
	for i := 0; i < 400; i++ {
		d := fmt.Sprintf(":%d", 100+(n-100+i)%400)
		if _, err := os.Stat("/tmp/.X" + d[1:] + "-lock"); err != nil && !reserved[d] {
			reserved[d] = true
			return d
		}
	}
	return DisplayFor(name)
}

var (
	displayMu sync.Mutex
	reserved  = map[string]bool{}
)

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
	rc, err := Extract("config/openbox-rc.xml")
	if err != nil {
		return
	}
	c := exec.Command(bin, "--config-file", rc)
	c.Env = append(os.Environ(), "DISPLAY="+display)
	if c.Start() == nil {
		go c.Wait()
		time.Sleep(500 * time.Millisecond)
	}
}

// StopX stops the X server on display (and with it the window manager), if one runs,
// and frees the display number.
func StopX(display string) {
	displayMu.Lock()
	delete(reserved, display)
	displayMu.Unlock()
	b, err := os.ReadFile("/tmp/.X" + strings.TrimPrefix(display, ":") + "-lock")
	if err != nil {
		return
	}
	var pid int
	fmt.Sscan(strings.TrimSpace(string(b)), &pid)
	// a stale lock may name a pid that now belongs to another program
	if comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); pid <= 0 || err != nil || strings.TrimSpace(string(comm)) != "Xvfb" {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		p.Signal(os.Interrupt)
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

// RemoveSink unloads the null sink created by EnsureAudio.
func RemoveSink(sink string) {
	out, err := exec.Command("pactl", "list", "short", "modules").Output()
	if err != nil {
		return
	}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) >= 3 && f[1] == "module-null-sink" && strings.Contains(l, "sink_name="+sink+" ") {
			exec.Command("pactl", "unload-module", f[0]).Run()
		}
	}
}

// Tool returns runtime/tools/<name>.exe, compiling tools/<name>/<name>.c (built into vnbox;
// 32-bit, MinGW) when the executable is missing or older than its source.
func Tool(name string) (string, error) {
	toolMu.Lock()
	defer toolMu.Unlock()
	exe := filepath.Join(Root(), "runtime", "tools", name+".exe")
	dir, err := Extract("tools/" + name)
	if err != nil {
		return "", err
	}
	src := filepath.Join(dir, name+".c")
	xs, err := os.Stat(exe)
	if ss, serr := os.Stat(src); err == nil && (serr != nil || !ss.ModTime().After(xs.ModTime())) {
		return exe, nil
	}
	os.MkdirAll(filepath.Dir(exe), 0o755)
	tmp := exe + ".tmp"
	if out, err := exec.Command("i686-w64-mingw32-gcc", "-O1", "-finput-charset=UTF-8", "-o", tmp, src, "-luser32", "-lshell32").CombinedOutput(); err != nil {
		return "", fmt.Errorf("build %s: %v: %s", name, err, out)
	}
	return exe, os.Rename(tmp, exe) // a running copy (another game's probe) keeps its old file
}

var toolMu sync.Mutex

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

// Browser returns the native Chromium used for HTML5 games ($VNBOX_BROWSER, Playwright's
// Chromium, or chromium / google-chrome on PATH), or "".
func Browser() string {
	if b := os.Getenv("VNBOX_BROWSER"); b != "" {
		return b
	}
	cands, _ := filepath.Glob("/opt/pw-browsers/chromium*/chrome-linux/chrome")
	for _, n := range []string{"chromium", "chromium-browser", "google-chrome"} {
		if p, err := exec.LookPath(n); err == nil {
			cands = append(cands, p)
		}
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// BrowserCommand opens an HTML5 game page as a window of its own on the game's display. The
// browser profile (localStorage saves) lives inside the prefix so it is saved with the game.
func BrowserCommand(ctx context.Context, e Env, page string) *exec.Cmd {
	profile := filepath.Join(e.Prefix, "drive_c", "users", "Public", "vnbox-browser")
	os.MkdirAll(profile, 0o755)
	return e.command(ctx, filepath.Dir(page), Browser(), "--no-sandbox", "--test-type", "--no-first-run",
		"--user-data-dir="+profile, "--app=file://"+page, "--window-position=0,0", "--window-size=1280,720",
		"--autoplay-policy=no-user-gesture-required", "--disable-features=Translate", "--lang=ja")
}

package wine

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// The programs a game distribution brings (the game, its installer, a self-extractor) are not
// trusted. With Env.Sandbox they run under bubblewrap:
//
//   - no network (unless Env.Network): only the loopback of a network namespace of its own
//   - the file system read-only, without /home, /root and /tmp: the game sees its own prefix
//     (read-write), the Wine build and the system libraries, not the other games, the packages,
//     the saves of others or the S3 credentials
//   - only its own X display, audio sink socket and wineserver
//   - an environment without credentials (AWS_*, VNBOX_TOKEN ...)
//
// VNBOX_SANDBOX=0 turns it off (debugging, or a host without bubblewrap).

// SandboxAvailable reports whether bubblewrap can be used.
func SandboxAvailable() bool {
	sandboxOnce.Do(func() {
		if os.Getenv("VNBOX_SANDBOX") == "0" {
			return
		}
		p, err := exec.LookPath("bwrap")
		if err != nil {
			log.Print("warning: bubblewrap (bwrap) is not installed: games run without a sandbox")
			return
		}
		// some hosts do not let it create namespaces (no user namespaces, AppArmor)
		if out, err := exec.Command(p, "--ro-bind", "/", "/", "--unshare-net", "true").CombinedOutput(); err != nil {
			log.Printf("warning: bubblewrap cannot run here (%v: %s): games run without a sandbox", err, strings.TrimSpace(string(out)))
			return
		}
		bwrapPath = p
	})
	return bwrapPath != ""
}

var (
	sandboxOnce sync.Once
	bwrapPath   string
)

// sandboxEnvKeep are the variables a sandboxed program gets (by prefix).
var sandboxEnvKeep = []string{"PATH=", "LANG=", "LC_", "TZ=", "HOME=", "USER=", "DISPLAY=", "WINE", "PULSE_",
	"LD_LIBRARY_PATH=", "FONTCONFIG", "XDG_", "VK_", "MESA_", "LP_", "GALLIUM_", "DXVK_", "VKD3D_", "__GL", "LIBGL_"}

// command builds the command running bin with args for this environment: directly, or under
// bubblewrap when the environment is sandboxed.
func (e Env) command(ctx context.Context, dir, bin string, args ...string) *exec.Cmd {
	env := e.Vars()
	if !e.Sandbox || !SandboxAvailable() {
		c := exec.CommandContext(ctx, bin, args...)
		c.Dir, c.Env = dir, env
		return c
	}
	bw, env := e.sandboxArgs(dir, env)
	c := exec.CommandContext(ctx, bwrapPath, append(append(bw, "--", bin), args...)...)
	c.Dir, c.Env = dir, env
	return c
}

// sandboxArgs returns the bubblewrap options for this environment and the environment
// variables the sandboxed program gets.
func (e Env) sandboxArgs(dir string, vars []string) ([]string, []string) {
	args := []string{"--die-with-parent", "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc",
		"--tmpfs", "/dev/shm", "--tmpfs", "/home", "--tmpfs", "/root", "--tmpfs", "/tmp", "--tmpfs", "/run"}
	if !e.Network {
		args = append(args, "--unshare-net")
	}
	if _, err := os.Stat("/dev/dri"); err == nil { // GPU (VAAPI / Vulkan)
		args = append(args, "--dev-bind", "/dev/dri", "/dev/dri")
	}
	bind := func(rw bool, src, dst string) {
		if _, err := os.Stat(src); err != nil {
			return
		}
		flag := "--ro-bind"
		if rw {
			flag = "--bind"
		}
		args = append(args, flag, src, dst)
	}
	bind(true, e.Prefix, e.Prefix)
	for _, b := range e.Binds {
		bind(true, b, b)
	}
	// the wineserver of this prefix: /tmp/.wine-<uid>/server-<dev>-<inode>, created up front so
	// that wine inside and wineserver -k / -w outside meet at the same socket. Wine refuses a
	// /tmp/.wine-<uid> that others can enter.
	if sd := serverDir(e.Prefix); sd != "" {
		os.MkdirAll(sd, 0o700)
		args = append(args, "--perms", "0700", "--dir", filepath.Dir(sd))
		bind(true, sd, sd)
	}
	if n := strings.TrimPrefix(e.Display, ":"); n != "" {
		bind(false, "/tmp/.X11-unix/X"+n, "/tmp/.X11-unix/X"+n)
	}
	env := []string{}
	for _, kv := range vars {
		for _, k := range sandboxEnvKeep {
			if strings.HasPrefix(kv, k) {
				env = append(env, kv)
				break
			}
		}
	}
	if sock, cookie := pulseSocket(); sock != "" {
		bind(false, sock, "/tmp/pulse/native")
		env = append(env, "PULSE_SERVER=unix:/tmp/pulse/native")
		if cookie != "" {
			bind(false, cookie, "/tmp/pulse/cookie")
			env = append(env, "PULSE_COOKIE=/tmp/pulse/cookie")
		}
	}
	// tools vnbox runs inside Wine (runtime/tools/*.exe)
	tools := filepath.Join(Root(), "runtime", "tools")
	bind(false, tools, tools)
	if dir != "" {
		args = append(args, "--chdir", dir)
	}
	return args, env
}

// serverDir is the wineserver directory Wine uses for prefix.
func serverDir(prefix string) string {
	var st syscall.Stat_t
	if syscall.Stat(prefix, &st) != nil {
		return ""
	}
	return fmt.Sprintf("/tmp/.wine-%d/server-%x-%x", os.Getuid(), st.Dev, st.Ino)
}

// pulseSocket returns the PulseAudio socket and cookie (pactl info, cached).
func pulseSocket() (sock, cookie string) {
	pulseOnce.Do(func() {
		out, _ := exec.Command("pactl", "info").Output()
		for _, l := range strings.Split(string(out), "\n") {
			if s, ok := strings.CutPrefix(l, "Server String: "); ok && strings.HasPrefix(s, "/") {
				pulseSock = strings.TrimSpace(s)
			}
		}
		home, _ := os.UserHomeDir()
		for _, c := range []string{filepath.Join(home, ".config", "pulse", "cookie"), filepath.Join(home, ".pulse-cookie")} {
			if _, err := os.Stat(c); err == nil {
				pulseCookie = c
				break
			}
		}
	})
	return pulseSock, pulseCookie
}

var (
	pulseOnce              sync.Once
	pulseSock, pulseCookie string
)

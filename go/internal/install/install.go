// Package install runs Windows installers under Wine: silently when the installer
// type allows it, otherwise by pressing the default button of every dialog.
package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"vnbox/internal/capture"
	"vnbox/internal/detect"
	"vnbox/internal/wine"
	"vnbox/internal/x11"
)

// Kind is the installer technology.
type Kind string

const (
	MSI              Kind = "msi"
	Inno             Kind = "inno"
	NSIS             Kind = "nsis"
	InstallShieldCab Kind = "installshield_cab"
	GUI              Kind = "gui" // anything else: drive the wizard
)

// Find returns the installer inside an extracted folder ("" if it looks like a ready-to-run game).
func Find(dir string) string {
	var found string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if strings.Count(rel, string(filepath.Separator)) > 2 && d.IsDir() {
			return filepath.SkipDir
		}
		n := strings.ToLower(d.Name())
		if strings.HasSuffix(n, ".msi") || n == "data1.hdr" {
			found = p
		}
		return nil
	})
	if found != "" {
		return found
	}
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || found != "" || d.IsDir() {
			return nil
		}
		n := strings.ToLower(d.Name())
		if n == "setup.exe" || (strings.HasPrefix(n, "install") && strings.HasSuffix(n, ".exe")) || strings.Contains(d.Name(), "インストール") && strings.HasSuffix(n, ".exe") {
			found = p
		}
		return nil
	})
	if found == "" {
		return ""
	}
	// setup.exe next to a recognisable game is usually a config tool, not an installer.
	// Look at the folder without the installer's own subfolder: if no engine is left
	// (only a launcher menu, say), the game has to be installed first.
	instDir := filepath.Dir(found)
	if instDir == dir {
		if detect.Detect(dir).Engine != "unknown" {
			return ""
		}
		return found
	}
	// the game exe sits next to setup.exe: it is a config tool of a ready-to-run game
	if main := detect.Detect(dir).MainExe(); main != "" && filepath.Join(dir, main) != found && filepath.Dir(filepath.Join(dir, main)) == instDir {
		return ""
	}
	if detect.DetectSkip(dir, instDir).Engine != "unknown" {
		return ""
	}
	return found
}

// Classify returns the installer kind of path.
func Classify(path string) Kind {
	low := strings.ToLower(path)
	switch {
	case strings.HasSuffix(low, ".msi"):
		return MSI
	case strings.HasSuffix(low, "data1.hdr"):
		return InstallShieldCab
	}
	for _, h := range detect.ScanBinary(path) {
		switch h {
		case "Installer:InnoSetup":
			return Inno
		case "Installer:NSIS":
			return NSIS
		}
	}
	return GUI
}

// Run installs path inside the capture session and returns the installer kind used.
// Dialog screenshots go to shotDir when it is not empty.
func Run(ctx context.Context, s *capture.Session, path, display, shotDir string, log io.Writer) (Kind, error) {
	if err := wine.EnsureX(display); err != nil {
		return "", err
	}
	env := s.Env(display)
	kind := Classify(path)
	fmt.Fprintf(log, "[%s] installer=%s %s\n", s.Name, kind, path)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	dir, exe := filepath.Dir(path), filepath.Base(path)
	run := func(args ...string) error {
		c := env.Command(ctx, dir, args...)
		c.Stdout, c.Stderr = log, log
		return c.Run()
	}
	var err error
	switch kind {
	case MSI:
		err = run("msiexec", "/i", exe, "/qn")
	case Inno:
		err = run(exe, "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/SP-")
	case NSIS:
		err = run(exe, "/S")
	case InstallShieldCab: // registry cannot be reproduced, but the files can be unpacked
		out := filepath.Join(s.Prefix, "drive_c", "Program Files", s.Name)
		os.MkdirAll(out, 0o755)
		err = exec.CommandContext(ctx, "unshield", "-d", out, "x", filepath.Join(dir, "data1.cab")).Run()
	case GUI:
		err = Drive(ctx, env, dir, exe, shotDir, log)
	}
	env.Wait()
	return kind, err
}

// Drive starts exe and presses through its dialogs until none are left.
func Drive(ctx context.Context, env wine.Env, dir, exe, shotDir string, log io.Writer) error {
	c := env.Command(ctx, dir, exe)
	if err := c.Start(); err != nil {
		return err
	}
	go c.Wait()
	time.Sleep(10 * time.Second)
	if shotDir != "" {
		os.MkdirAll(shotDir, 0o755)
	}
	d := x11.Display(env.Display)
	d.PressDialogs(80, 4, func(i int, w x11.Window) {
		fmt.Fprintf(log, "  %02d [%s] %dx%d\n", i, w.Name, w.W, w.H)
		if shotDir != "" {
			d.Shot(filepath.Join(shotDir, fmt.Sprintf("wiz_%02d.png", i)), w)
		}
	})
	return nil
}

// ExtractSFX runs a self-extracting archive (e.g. WinRAR SFX, which Linux tools cannot
// always open) in an empty folder and waits until its output stops growing.
func ExtractSFX(ctx context.Context, fl wine.Flavor, sfx, outDir, display string, log io.Writer) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	prefix := outDir + ".prefix"
	defer os.RemoveAll(prefix)
	if err := wine.ClonePrefix(fl.BasePrefix(false), prefix); err != nil {
		return err
	}
	if err := wine.EnsureX(display); err != nil {
		return err
	}
	env := wine.Env{Flavor: fl, Prefix: prefix, Display: display}
	abs, _ := filepath.Abs(sfx)
	if err := Drive(ctx, env, outDir, abs, "", log); err != nil {
		return err
	}
	prev := int64(-1)
	for {
		n := dirSize(outDir)
		if n == prev {
			break
		}
		prev = n
		time.Sleep(10 * time.Second)
	}
	env.Kill()
	return nil
}

func dirSize(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if i, err := d.Info(); err == nil {
				n += i.Size()
			}
		}
		return nil
	})
	return n
}

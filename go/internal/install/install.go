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
//
// Many ready-to-run games ship a setup.exe / installer.exe (a config tool or a file copier),
// while some distributions are only a launcher menu with the real game behind an installer.
// The game counts as ready to run when the game exe sits right next to the installer, or when
// an engine is still recognised after ignoring the files directly beside the installer
// (its subfolders, e.g. gamedata/, are still looked at).
func Find(dir string) string {
	inst := find(dir)
	if inst == "" {
		// an installer or self-extractor with an arbitrary name (e.g. HimeTrial.exe is InstallShield)
		r := detect.Detect(dir)
		if r.Engine == "unknown" && len(r.Exes) > 0 {
			for _, h := range r.Exes[0].Hits {
				// SFX:ZIP / SFX:LZH also match resources embedded in ordinary game exes
				if strings.HasPrefix(h, "Installer:") || h == "SFX:WinRAR" || h == "SFX:7z" || h == "SFX:CAB" {
					return filepath.Join(dir, r.Exes[0].Path)
				}
			}
		}
		return ""
	}
	if inst == "" || strings.HasSuffix(strings.ToLower(inst), ".msi") || strings.EqualFold(filepath.Base(inst), "data1.hdr") {
		return inst
	}
	instDir := filepath.Dir(inst)
	// BGI trials ship BGIForInstalling.exe / installer.arc: the game only starts once Setup.exe
	// has written its install key, so Setup.exe is the real installer here
	if needsInstall(instDir) {
		return inst
	}
	if main := detect.Detect(dir).MainExe(); main != "" {
		m := filepath.Join(dir, main)
		if m != inst && filepath.Dir(m) == instDir {
			return ""
		}
	}
	if detect.DetectSkip(dir, instDir).FileEngine != "unknown" {
		return ""
	}
	return inst
}

func needsInstall(dir string) bool {
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		n := strings.ToLower(e.Name())
		if strings.Contains(n, "forinstalling") || n == "installer.arc" {
			return true
		}
	}
	return false
}

func find(dir string) string {
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
func Run(ctx context.Context, s *capture.Session, path, display, shotDir string, clicks [][2]int, log io.Writer) (Kind, error) {
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
		if err := c.Start(); err != nil {
			return err
		}
		done := make(chan error, 1)
		go func() { done <- c.Wait() }()
		// Many installers launch the game when they finish and wait for it to exit.
		// A game-sized window means the installation is complete: stop everything.
		d := x11.Display(display)
		for {
			select {
			case err := <-done:
				return err
			case <-time.After(5 * time.Second):
				if w, ok := d.Largest(); ok && w.W >= 640 && w.H >= 400 {
					fmt.Fprintf(log, "[%s] game window %q appeared: installation finished\n", s.Name, w.Name)
					time.Sleep(3 * time.Second)
					env.Kill()
					<-done
					return nil
				}
			}
		}
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
		err = Drive(ctx, env, dir, exe, shotDir, clicks, log)
	}
	// installers that copy in the background after their last dialog: wait until drive C
	// stops growing (checked every 5 s, two equal readings in a row)
	driveC := filepath.Join(s.Prefix, "drive_c")
	for prev, same := int64(-1), 0; same < 2 && ctx.Err() == nil; {
		time.Sleep(5 * time.Second)
		if n := dirSize(driveC); n == prev {
			same++
		} else {
			prev, same = n, 0
		}
	}
	// the installer is done; whatever still runs (a launched game, a readme viewer, a
	// dialog nobody answers) would keep wineserver alive forever
	time.Sleep(3 * time.Second)
	env.Kill()
	env.Wait()
	return kind, err
}

// Drive starts exe, performs the given clicks (relative to the newest window; for menus that
// have no default button, e.g. an installer menu's インストール item) and then presses
// through its dialogs until none are left.
func Drive(ctx context.Context, env wine.Env, dir, exe, shotDir string, clicks [][2]int, log io.Writer) error {
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
	for _, c := range clicks {
		ws := d.Windows()
		if len(ws) == 0 {
			break
		}
		w := ws[len(ws)-1]
		fmt.Fprintf(log, "  click %d,%d on [%s]\n", c[0], c[1], w.Name)
		d.Click(w, c[0], c[1], 1)
		time.Sleep(5 * time.Second)
	}
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
	if err := Drive(ctx, env, outDir, abs, "", nil, log); err != nil {
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

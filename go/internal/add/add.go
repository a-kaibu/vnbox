// Package add turns one distribution file (archive, self-extractor, installer or folder)
// into a package.
package add

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"vnbox/internal/capture"
	"vnbox/internal/detect"
	"vnbox/internal/extract"
	"vnbox/internal/install"
	"vnbox/internal/pkg"
	"vnbox/internal/wine"
)

// ErrNoExecutable means the archive holds no program (e.g. a promotional movie).
var ErrNoExecutable = errors.New("no executable in the archive (video-only distribution?)")

// Options controls Add.
type Options struct {
	Flavor wine.Flavor
	Exe    string   // launch target to force (relative to the game folder or to C:\ for installers)
	Clicks [][2]int // clicks on the installer's first window before the dialog walker runs
	Log    io.Writer
}

// Add creates packages/<name> from src and returns the package.
func Add(ctx context.Context, name, src string, o Options) (*pkg.Package, error) {
	if o.Log == nil {
		o.Log = io.Discard
	}
	if o.Flavor == "" {
		o.Flavor = wine.Vnbox
	}
	dst := pkg.Dir(name)
	if _, err := os.Stat(dst); err == nil {
		return nil, fmt.Errorf("already exists: %s", dst)
	}
	src, err := filepath.Abs(src)
	if err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(filepath.Join(wine.Root(), "downloads"), "add-"+name+".")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	display := wine.DisplayFor("add-" + name)

	// 1) unpack
	var dir string
	st, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		dir = src
	} else if err := extract.Extract(src, filepath.Join(work, "x")); err == nil && hasExe(filepath.Join(work, "x")) {
		dir = filepath.Join(work, "x")
	} else if err == nil && !strings.EqualFold(filepath.Ext(src), ".exe") {
		return nil, ErrNoExecutable
	} else if isWinRARSFX(src) {
		fmt.Fprintf(o.Log, "[%s] WinRAR SFX: extracting with Wine\n", name)
		out := filepath.Join(work, "sfx")
		if err := install.ExtractSFX(ctx, o.Flavor, src, out, display, o.Log); err != nil {
			return nil, err
		}
		if hasExe(out) {
			dir = out
		}
	}

	// 2) installer -> capture what it installs
	inst := src
	if dir != "" {
		inst = install.Find(dir)
	}
	if inst != "" {
		s, err := capture.Begin(name, o.Flavor)
		if err != nil {
			return nil, err
		}
		if install.Classify(inst) == install.GUI {
			if staged, err := s.Stage(inst); err == nil {
				inst = staged
			}
		}
		if _, err := install.Run(ctx, s, inst, display, filepath.Join(wine.Root(), "results", "add-"+name), o.Clicks, o.Log); err != nil {
			fmt.Fprintf(o.Log, "installer: %v\n", err)
		}
		r, err := s.End()
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(o.Log, "captured %d files; candidates: %v\n", r.Files, r.Candidates)
		p, err := pkg.Open(dst)
		if err != nil {
			return nil, err
		}
		// nothing was installed: the "installer" was the game itself (or a launcher of it)
		if l, _ := p.Launch(); l.Target == "" && r.Files == 0 && dir != "" {
			fmt.Fprintf(o.Log, "[%s] installer produced nothing; packaging the folder as is\n", name)
			os.RemoveAll(dst)
			return addFolder(dir, dst, name, o)
		}
		if o.Exe != "" {
			l, _ := p.Launch()
			l.Target = filepath.ToSlash(o.Exe)
			pkg.WriteLaunch(dst, l)
		}
		return checkTarget(p)
	}

	return addFolder(dir, dst, name, o)
}

// addFolder packages a ready-to-run game folder under C:\Games\<name>.
func addFolder(dir, dst, name string, o Options) (*pkg.Package, error) {
	gdir := filepath.Join(dst, "files", "Games", name)
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		return nil, err
	}
	if out, err := exec.Command("cp", "-a", dir+"/.", gdir+"/").CombinedOutput(); err != nil {
		os.RemoveAll(dst)
		return nil, fmt.Errorf("copy: %v: %s", err, out)
	}
	exe := o.Exe
	if exe == "" {
		exe = detect.Detect(gdir).MainExe()
	}
	if exe == "" {
		os.RemoveAll(dst)
		return nil, errors.New("no executable found")
	}
	if err := pkg.WriteLaunch(dst, pkg.Launch{Target: filepath.ToSlash(filepath.Join("Games", name, exe))}); err != nil {
		return nil, err
	}
	os.WriteFile(filepath.Join(dst, "system.reg.frag"), nil, 0o644)
	os.WriteFile(filepath.Join(dst, "user.reg.frag"), nil, 0o644)
	p, err := pkg.Open(dst)
	if err != nil {
		return nil, err
	}
	return checkTarget(p)
}

func checkTarget(p *pkg.Package) (*pkg.Package, error) {
	l, err := p.Launch()
	if err != nil {
		return nil, err
	}
	if l.Target == "" {
		return p, fmt.Errorf("could not determine the executable; edit %s/launch.ini", p.Dir)
	}
	return p, nil
}

func hasExe(dir string) bool {
	found := false
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(d.Name()), ".exe") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func isWinRARSFX(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 4<<20)
	n, _ := io.ReadFull(f, buf)
	return bytes.Contains(buf[:n], []byte("WinRAR SFX"))
}

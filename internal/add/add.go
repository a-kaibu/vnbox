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

	noRetry bool
	Log     io.Writer
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
	display := wine.FreeDisplay("add-" + name) // never share an X server with another add or game
	defer wine.StopX(display)

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
		// an HTML5 game (index.html, no Windows program): runs in the browser, not in Wine
		if page := htmlEntry(filepath.Join(work, "x")); page != "" {
			fmt.Fprintf(o.Log, "[%s] HTML5 game: %s\n", name, page)
			return addHTML5(filepath.Join(work, "x"), page, dst, name)
		}
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

	// an archive holding nothing but a self-extracting exe (ZIP / 7z / LZH SFX): unpack that too
	if dir != "" {
		if only := soleFile(dir); strings.EqualFold(filepath.Ext(only), ".exe") && !isWinRARSFX(only) {
			out := filepath.Join(work, "sfx2")
			if err := extract.Extract(only, out); err == nil && hasExe(out) {
				fmt.Fprintf(o.Log, "[%s] nested self-extractor %s unpacked\n", name, filepath.Base(only))
				dir = out
			}
		}
	}

	// 2) installer -> capture what it installs
	inst := src
	if dir != "" {
		inst = install.Find(dir)
	}
	// InstallShield bootstrappers: install the embedded .msi directly (some stall under Wine
	// while unpacking it)
	if strings.EqualFold(filepath.Ext(inst), ".exe") {
		if files, err := install.ExtractInstallShield(inst, filepath.Join(work, "isx")); err == nil {
			for _, f := range files {
				if strings.EqualFold(filepath.Ext(f), ".msi") {
					fmt.Fprintf(o.Log, "[%s] InstallShield payload: %s\n", name, filepath.Base(f))
					inst = f
				}
			}
		}
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
			// a failed capture (e.g. the disk filled up while copying) cannot be resumed
			// by add: do not leave the installed prefix and a half-written package behind
			os.RemoveAll(s.Prefix)
			os.RemoveAll(dst)
			return nil, err
		}
		fmt.Fprintf(o.Log, "captured %d files; candidates: %v\n", r.Files, r.Candidates)
		p, err := pkg.Open(dst)
		if err != nil {
			return nil, err
		}
		// nothing installed on the patched Wine (Win10): old installers often refuse unknown
		// Windows versions, so try once more on the classic Wine with its Win7 prefix
		if r.Files == 0 && o.Flavor == wine.Vnbox && !o.noRetry {
			fmt.Fprintf(o.Log, "[%s] installer produced nothing; retrying on classic Wine (Win7)\n", name)
			os.RemoveAll(dst)
			os.RemoveAll(work) // the retry unpacks again: do not keep two copies on disk
			o2 := o
			o2.Flavor, o2.noRetry = wine.Classic, true
			if p2, err := Add(ctx, name, src, o2); err == nil {
				return p2, nil
			}
			os.RemoveAll(dst)
			return Add(ctx, name, src, Options{Flavor: wine.Vnbox, Exe: o.Exe, Clicks: o.Clicks, Log: o.Log, noRetry: true})
		}
		// no program was installed: the "installer" was the game itself (or a launcher of it),
		// which may have written its settings while it ran (a big single-exe game)
		if l, _ := p.Launch(); l.Target == "" && dir != "" && (r.Files == 0 || o.Exe == "") {
			fmt.Fprintf(o.Log, "[%s] installer installed no program; packaging the folder as is\n", name)
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
	if err := os.MkdirAll(filepath.Dir(gdir), 0o755); err != nil {
		return nil, err
	}
	// our own unpacked copy (downloads/add-*) is deleted afterwards: move it instead of
	// needing the game's size twice on disk
	moved := false
	if rel, err := filepath.Rel(filepath.Join(wine.Root(), "downloads"), dir); err == nil && strings.HasPrefix(rel, "add-") {
		os.RemoveAll(gdir)
		moved = os.Rename(dir, gdir) == nil
	}
	if !moved {
		if err := os.MkdirAll(gdir, 0o755); err != nil {
			return nil, err
		}
		if out, err := exec.Command("cp", "-a", dir+"/.", gdir+"/").CombinedOutput(); err != nil {
			os.RemoveAll(dst)
			return nil, fmt.Errorf("copy: %v: %s", err, out)
		}
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

// htmlEntry returns the index.html of an HTML5 game under dir (at most 2 levels deep), or "".
func htmlEntry(dir string) string {
	var found string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() && strings.Count(rel, string(filepath.Separator)) >= 2 {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.EqualFold(d.Name(), "index.html") {
			found = rel
		}
		return nil
	})
	return found
}

// addHTML5 packages an HTML5 game under C:\Games\<name>; launch.ini points at the page and the
// session opens it in the browser (pkg.Launch.IsHTML).
func addHTML5(dir, page, dst, name string) (*pkg.Package, error) {
	gdir := filepath.Join(dst, "files", "Games", name)
	if err := os.MkdirAll(filepath.Dir(gdir), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(dir, gdir); err != nil {
		if out, err := exec.Command("cp", "-a", dir+"/.", gdir+"/").CombinedOutput(); err != nil {
			os.RemoveAll(dst)
			return nil, fmt.Errorf("copy: %v: %s", err, out)
		}
	}
	l := pkg.Launch{Target: filepath.ToSlash(filepath.Join("Games", name, page)), Engine: "HTML5"}
	if err := pkg.WriteLaunch(dst, l); err != nil {
		return nil, err
	}
	os.WriteFile(filepath.Join(dst, "system.reg.frag"), nil, 0o644)
	os.WriteFile(filepath.Join(dst, "user.reg.frag"), nil, 0o644)
	return pkg.Open(dst)
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

// soleFile returns the path of the only file under dir (ignoring readme-like text files), or "".
func soleFile(dir string) string {
	var files []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			switch strings.ToLower(filepath.Ext(d.Name())) {
			case ".txt", ".url", ".html", ".htm", ".pdf":
			default:
				files = append(files, p)
			}
		}
		return nil
	})
	if len(files) == 1 {
		return files[0]
	}
	return ""
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

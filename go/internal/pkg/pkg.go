// Package pkg handles game packages: a directory holding only what the game added to a
// Windows system (files under C:\ and registry fragments) plus how to launch it.
//
//	packages/<name>/
//	  launch.ini        [launch] target=<path under C:\> args= renderer= winetricks= title= engine=
//	  thumb.jpg         optional gallery thumbnail (taken while the game runs)
//	  files/            copied into drive_c
//	  system.reg.frag   HKLM changes
//	  user.reg.frag     HKCU changes
package pkg

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"vnbox/internal/detect"
	"vnbox/internal/regdiff"
	"vnbox/internal/wine"
)

// Marker files inside a prepared prefix.
const (
	AppliedMarker = ".vnbox-applied" // package applied; files newer than this are user data (saves)
	RegSnapshot   = ".vnbox-reg0"    // registry right after preparation, the baseline for save diffs
)

// Launch is the content of launch.ini.
type Launch struct {
	Target     string // exe path relative to drive_c, with forward slashes
	Args       string
	Renderer   string // WineD3D renderer: gl / vulkan / gdi
	Winetricks []string
	Flavor     string // optional: "classic" to force the distribution Wine
	Title      string // display name for the gallery (defaults to the package name)
	Engine     string // detected engine, shown in the gallery
	DRM        string // DRM / protector found in the files (DRM:*, Protect:* tags of detect), comma separated
}

// ThumbFile is the gallery thumbnail inside a package directory.
const ThumbFile = "thumb.jpg"

// Thumb returns the thumbnail path of the package (it may not exist).
func (p *Package) Thumb() string { return filepath.Join(p.Dir, ThumbFile) }

// Package is a package directory.
type Package struct {
	Dir  string
	Name string
}

// Open returns the package in dir.
func Open(dir string) (*Package, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(abs, "launch.ini")); err != nil {
		return nil, fmt.Errorf("not a package (no launch.ini): %s", abs)
	}
	return &Package{Dir: abs, Name: filepath.Base(abs)}, nil
}

// Dir returns packages/<name> under the vnbox root.
func Dir(name string) string { return filepath.Join(wine.Root(), "packages", name) }

// Launch reads launch.ini.
func (p *Package) Launch() (Launch, error) {
	f, err := os.Open(filepath.Join(p.Dir, "launch.ini"))
	if err != nil {
		return Launch{}, err
	}
	defer f.Close()
	return ParseLaunch(f)
}

// ParseLaunch parses the content of a launch.ini.
func ParseLaunch(r io.Reader) (Launch, error) {
	var l Launch
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "target":
			l.Target = filepath.ToSlash(v)
		case "args":
			l.Args = v
		case "renderer":
			l.Renderer = v
		case "winetricks":
			l.Winetricks = strings.Fields(v)
		case "flavor":
			l.Flavor = v
		case "title":
			l.Title = v
		case "engine":
			l.Engine = v
		case "drm":
			l.DRM = v
		}
	}
	return l, sc.Err()
}

// WriteLaunch writes launch.ini.
func WriteLaunch(dir string, l Launch) error {
	s := fmt.Sprintf("[launch]\ntarget=%s\nargs=%s\nrenderer=%s\n", l.Target, l.Args, l.Renderer)
	if len(l.Winetricks) > 0 {
		s += "winetricks=" + strings.Join(l.Winetricks, " ") + "\n"
	}
	if l.Flavor != "" {
		s += "flavor=" + l.Flavor + "\n"
	}
	if l.Title != "" {
		s += "title=" + l.Title + "\n"
	}
	if l.Engine != "" {
		s += "engine=" + l.Engine + "\n"
	}
	if l.DRM != "" {
		s += "drm=" + l.DRM + "\n"
	}
	return os.WriteFile(filepath.Join(dir, "launch.ini"), []byte(s), 0o644)
}

// Flavor returns the Wine flavor the package should run with.
func (l Launch) WineFlavor(def wine.Flavor) wine.Flavor {
	if l.Flavor == "classic" {
		return wine.Classic
	}
	if l.Flavor == "vnbox" {
		return wine.Vnbox
	}
	return def
}

// Prepared reports whether prefix already has this package applied.
func Prepared(prefix string) bool {
	_, err := os.Stat(filepath.Join(prefix, AppliedMarker))
	return err == nil
}

// Prepare creates prefix from the flavor's base prefix and applies the package
// (files, registry fragments, renderer, winetricks verbs). It does nothing if the
// prefix is already prepared.
func (p *Package) Prepare(ctx context.Context, fl wine.Flavor, prefix string) error {
	if Prepared(prefix) {
		return nil
	}
	l, err := p.Launch()
	if err != nil {
		return err
	}
	if l.Target == "" {
		return errors.New("launch.ini has no target")
	}
	x64 := detect.PEArch(filepath.Join(p.Dir, "files", filepath.FromSlash(l.Target))) == "x64"
	os.RemoveAll(prefix)
	if err := os.MkdirAll(filepath.Dir(prefix), 0o755); err != nil {
		return err
	}
	if err := wine.ClonePrefix(fl.BasePrefix(x64), prefix); err != nil {
		return err
	}
	for _, r := range []string{"system", "user"} {
		frag := filepath.Join(p.Dir, r+".reg.frag")
		if st, err := os.Stat(frag); err == nil && st.Size() > 0 {
			if err := regdiff.Apply(frag, filepath.Join(prefix, r+".reg")); err != nil {
				return fmt.Errorf("apply %s: %w", frag, err)
			}
		}
	}
	// cp -a keeps timestamps, so package files stay older than the marker and are not saves.
	// the prefix is a hardlink clone of the base: replace files instead of writing through the links
	if err := exec.Command("cp", "-a", "--remove-destination", filepath.Join(p.Dir, "files")+"/.", filepath.Join(prefix, "drive_c")+"/").Run(); err != nil {
		return fmt.Errorf("copy files: %w", err)
	}
	env := wine.Env{Flavor: fl, Prefix: prefix, Display: os.Getenv("DISPLAY")}
	if env.Display == "" {
		env.Display = ":61"
	}
	if l.Renderer != "" || len(l.Winetricks) > 0 {
		if err := wine.EnsureX(env.Display); err != nil {
			return err
		}
	}
	if l.Renderer != "" {
		env.RegAdd(ctx, `HKCU\Software\Wine\Direct3D`, "renderer", "", l.Renderer)
	}
	for _, v := range l.Winetricks {
		if !strings.HasPrefix(v, "vnbox-") {
			// winetricks copies DLLs into system32 in place, which would write through the
			// prefix's hard links into the base prefix: give this prefix its own copies first
			if err := unshareSystemDirs(prefix); err != nil {
				return err
			}
			break
		}
	}
	for _, v := range l.Winetricks {
		// "vnbox-<name>" は vnbox 自前の手順 (scripts/verbs/<name>.sh <prefix>)、それ以外は winetricks の verb
		c := exec.CommandContext(ctx, "winetricks", "-q", v)
		if name, ok := strings.CutPrefix(v, "vnbox-"); ok { // vnbox-<名前>[:引数:...]
			parts := strings.Split(name, ":")
			args := append([]string{filepath.Join(wine.Root(), "scripts", "verbs", parts[0]+".sh"), prefix}, parts[1:]...)
			c = exec.CommandContext(ctx, "bash", args...)
		}
		c.Env = env.Vars()
		if out, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %v: %s", v, err, out)
		}
	}
	env.Wait()
	return Snapshot(prefix)
}

// unshareSystemDirs replaces the hard-linked windows/system32, syswow64 and Fonts of a cloned
// prefix with real copies.
func unshareSystemDirs(prefix string) error {
	for _, d := range []string{"system32", "syswow64", "Fonts"} {
		dir := filepath.Join(prefix, "drive_c", "windows", d)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		tmp := dir + ".vnbox-copy"
		os.RemoveAll(tmp)
		if out, err := exec.Command("cp", "-a", dir, tmp).CombinedOutput(); err != nil {
			os.RemoveAll(tmp)
			return fmt.Errorf("unshare %s: %v: %s", d, err, out)
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		if err := os.Rename(tmp, dir); err != nil {
			return err
		}
	}
	return nil
}

// Snapshot records the current registry as the save baseline and marks the prefix prepared.
func Snapshot(prefix string) error {
	snap := filepath.Join(prefix, RegSnapshot)
	if err := os.MkdirAll(snap, 0o755); err != nil {
		return err
	}
	for _, r := range []string{"system.reg", "user.reg"} {
		b, err := os.ReadFile(filepath.Join(prefix, r))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(snap, r), b, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(prefix, AppliedMarker), nil, 0o644)
}

// GameDir returns the working directory and exe name of the target inside a prepared prefix.
func (l Launch) GameDir(prefix string) (dir, exe string) {
	full := filepath.Join(prefix, "drive_c", filepath.FromSlash(l.Target))
	return filepath.Dir(full), filepath.Base(full)
}

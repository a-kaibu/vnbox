// Package capture turns an installer run into a package: it snapshots a fresh prefix,
// lets the installer run, and keeps only the files and registry entries it added
// (the Wine counterpart of DoNotInstaller, without hooking anything).
package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"vnbox/internal/detect"
	"vnbox/internal/pkg"
	"vnbox/internal/regdiff"
	"vnbox/internal/wine"
)

const snapDir = ".vnbox-snap"

type entry struct {
	Size  int64 `json:"s"`
	Mtime int64 `json:"m"`
}

func manifest(driveC string) (map[string]entry, error) {
	m := map[string]entry{}
	err := filepath.WalkDir(driveC, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(driveC, p)
		m[filepath.ToSlash(rel)] = entry{Size: info.Size(), Mtime: info.ModTime().UnixNano()}
		return nil
	})
	return m, err
}

// Session is an installer capture in progress.
type Session struct {
	Name   string
	Prefix string
	Flavor wine.Flavor
	snap   map[string]entry
}

// Begin creates prefixes/<name> from the base prefix and records its state.
func Begin(name string, fl wine.Flavor) (*Session, error) {
	s := &Session{Name: name, Prefix: filepath.Join(wine.Root(), "prefixes", name), Flavor: fl}
	os.RemoveAll(s.Prefix)
	if err := wine.ClonePrefix(fl.BasePrefix(false), s.Prefix); err != nil {
		return nil, err
	}
	// let Wine bring the prefix up to date first (a base made by another Wine version is
	// upgraded on first start), otherwise the upgrade would show up as installed files
	env := wine.Env{Flavor: fl, Prefix: s.Prefix}
	env.Run(context.Background(), nil, "wineboot")
	env.Wait()
	snap := filepath.Join(s.Prefix, snapDir)
	if err := os.MkdirAll(snap, 0o755); err != nil {
		return nil, err
	}
	for _, r := range []string{"system.reg", "user.reg"} {
		if err := copyFile(filepath.Join(s.Prefix, r), filepath.Join(snap, r)); err != nil {
			return nil, err
		}
	}
	var err error
	if s.snap, err = manifest(filepath.Join(s.Prefix, "drive_c")); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(s.snap)
	return s, os.WriteFile(filepath.Join(snap, "manifest.json"), b, 0o644)
}

// Resume reopens a capture started by Begin (possibly in another process),
// so the installer can be run by hand in between.
func Resume(name string, fl wine.Flavor) (*Session, error) {
	s := &Session{Name: name, Prefix: filepath.Join(wine.Root(), "prefixes", name), Flavor: fl}
	b, err := os.ReadFile(filepath.Join(s.Prefix, snapDir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("no capture in progress for %s: %w", name, err)
	}
	return s, json.Unmarshal(b, &s.snap)
}

// Stage copies the installer's folder into the prefix (C:\_vnbox_src) and returns the new
// path of the installer. Self-extractors often unpack next to themselves; staged there,
// the result lands on drive C and is captured. The staged copy itself is not packaged.
func (s *Session) Stage(installer string) (string, error) {
	src := filepath.Dir(installer)
	dst := filepath.Join(s.Prefix, "drive_c", "_vnbox_src")
	if out, err := exec.Command("cp", "-a", src+"/.", dst+"/").CombinedOutput(); err != nil {
		return "", fmt.Errorf("stage: %v: %s", err, out)
	}
	m, err := manifest(filepath.Join(s.Prefix, "drive_c"))
	if err != nil {
		return "", err
	}
	for p, e := range m {
		if strings.HasPrefix(p, "_vnbox_src/") {
			s.snap[p] = e
		}
	}
	return filepath.Join(dst, filepath.Base(installer)), nil
}

// Env returns the Wine environment to run the installer in.
func (s *Session) Env(display string) wine.Env {
	return wine.Env{Flavor: s.Flavor, Prefix: s.Prefix, Display: display}
}

// temporary files, logs and Wine's own bookkeeping are never part of a package
var junk = regexp.MustCompile(`(?i)^(windows/(temp|logs|installer|inf|winsxs|prefetch)/|users/[^/]+/(temp|appdata/local/temp|appdata/local/microsoft)/|.*\.(tmp|log)$)`)

// Result summarizes a finished capture.
type Result struct {
	Package    string
	Files      int
	Candidates []string
}

// End stops the prefix, writes packages/<name>/ and removes the capture prefix.
func (s *Session) End() (Result, error) {
	env := s.Env("")
	env.Kill()
	env.Wait()
	dst := pkg.Dir(s.Name)
	os.RemoveAll(dst)
	files := filepath.Join(dst, "files")
	if err := os.MkdirAll(files, 0o755); err != nil {
		return Result{}, err
	}
	for _, r := range []string{"system", "user"} {
		a, err := regdiff.Parse(filepath.Join(s.Prefix, snapDir, r+".reg"))
		if err != nil {
			return Result{}, err
		}
		b, err := regdiff.Parse(filepath.Join(s.Prefix, r+".reg"))
		if err != nil {
			return Result{}, err
		}
		if err := regdiff.Diff(a, b).WriteFragment(filepath.Join(dst, r+".reg.frag")); err != nil {
			return Result{}, err
		}
	}
	driveC := filepath.Join(s.Prefix, "drive_c")
	after, err := manifest(driveC)
	if err != nil {
		return Result{}, err
	}
	var added []string
	for p, e := range after {
		if old, ok := s.snap[p]; (!ok || old != e) && !junk.MatchString(p) {
			added = append(added, p)
		}
	}
	sort.Strings(added)
	for _, p := range added {
		if err := copyFile(filepath.Join(driveC, p), filepath.Join(files, filepath.FromSlash(p))); err != nil {
			return Result{}, err
		}
	}
	os.WriteFile(filepath.Join(dst, "files.txt"), []byte(strings.Join(added, "\n")+"\n"), 0o644)
	// launch candidates: new executables, largest first, minus tools
	res := detect.Detect(files)
	var cands []string
	for _, e := range res.Exes {
		cands = append(cands, filepath.ToSlash(e.Path))
	}
	target := res.MainExe()
	if err := pkg.WriteLaunch(dst, pkg.Launch{Target: filepath.ToSlash(target)}); err != nil {
		return Result{}, err
	}
	os.RemoveAll(s.Prefix)
	return Result{Package: dst, Files: len(added), Candidates: cands}, nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// cp -a keeps timestamps and does not follow the hard links shared with the base prefix
	if out, err := exec.Command("cp", "-a", "--remove-destination", src, dst).CombinedOutput(); err != nil {
		return fmt.Errorf("cp %s: %v: %s", src, err, out)
	}
	return nil
}

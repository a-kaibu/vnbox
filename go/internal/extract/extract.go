// Package extract unpacks game archives and converts Shift-JIS file names to UTF-8
// (Wine only sees UTF-8 names correctly).
package extract

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
)

// ErrNotArchive means the file is an executable that is not a self-extracting archive
// (typically an installer).
var ErrNotArchive = errors.New("not an extractable archive")

// Extract unpacks src into dst (created if missing). It succeeds when files came out,
// even if the tool reported warnings (e.g. odd name encodings).
func Extract(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	low := strings.ToLower(src)
	var err error
	switch {
	case strings.HasSuffix(low, ".zip"):
		err = unzip(src, dst)
		if err != nil {
			err = run(dst, "7z", "x", "-y", "-mcp=932", src)
		}
	case strings.HasSuffix(low, ".lzh"), strings.HasSuffix(low, ".lha"):
		err = run(dst, "lha", "-xqf", src)
	case strings.HasSuffix(low, ".7z"), strings.HasSuffix(low, ".rar"), strings.HasSuffix(low, ".cab"):
		err = run(dst, "7z", "x", "-y", src)
	case strings.HasSuffix(low, ".exe"):
		// 7z also crashes on some WinRAR SFX; a partial result is not usable
		// -mcp=932: ZIP self-extractors carry Shift_JIS names. 7z refuses the switch for
		// 7-Zip self-extractors (E_INVALIDARG), so try again without it.
		if run(dst, "7z", "x", "-y", "-mcp=932", src) != nil {
			clear(dst)
			if run(dst, "7z", "x", "-y", src) != nil {
				clear(dst)
				return ErrNotArchive
			}
		}
	default:
		return fmt.Errorf("unknown archive type: %s", src)
	}
	if err != nil && empty(dst) {
		return err
	}
	return FixNames(dst)
}

func run(dir, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Dir = dir
	return c.Run()
}

func clear(dir string) {
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		os.RemoveAll(filepath.Join(dir, e.Name()))
	}
}

func empty(dir string) bool {
	ents, err := os.ReadDir(dir)
	return err != nil || len(ents) == 0
}

var sjis = japanese.ShiftJIS.NewDecoder()

// decodeName returns the UTF-8 form of a zip entry name. Entries without the UTF-8 flag
// (archive/zip reports them as NonUTF8) are decoded as CP932 unless that produces garbage.
func decodeName(name string, nonUTF8 bool) string {
	if !nonUTF8 {
		return name
	}
	s, err := sjis.String(name)
	if err != nil || (strings.ContainsRune(s, utf8.RuneError) && utf8.ValidString(name)) {
		return name
	}
	return s
}

func unzip(src, dst string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	root, _ := filepath.Abs(dst)
	for _, f := range r.File {
		name := decodeName(f.Name, f.NonUTF8)
		name = strings.ReplaceAll(name, `\`, "/")
		p := filepath.Join(root, filepath.FromSlash(name))
		if !strings.HasPrefix(p, root+string(filepath.Separator)) && p != root { // zip slip
			return fmt.Errorf("unsafe path in archive: %q", f.Name)
		}
		if f.FileInfo().IsDir() || strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := writeEntry(f, p); err != nil {
			return err
		}
	}
	return nil
}

func writeEntry(f *zip.File, p string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	w, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, rc); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return os.Chtimes(p, f.Modified, f.Modified)
}

// FixNames renames every entry under root whose name is not valid UTF-8, decoding it as CP932.
// Deepest entries are renamed first so that parent paths stay valid.
func FixNames(root string) error {
	var paths []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && p != root && !utf8.ValidString(d.Name()) {
			paths = append(paths, p)
		}
		return nil
	})
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	for _, p := range paths {
		dir, name := filepath.Split(p)
		if s, err := sjis.String(name); err == nil && s != name {
			if err := os.Rename(p, filepath.Join(dir, s)); err != nil {
				return err
			}
		}
	}
	return nil
}

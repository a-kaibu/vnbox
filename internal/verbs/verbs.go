// Package verbs installs vnbox's own prefix additions, named "vnbox-<verb>[:arg:...]" in
// launch.ini's winetricks= (the other names there are winetricks verbs): runtime DLLs that
// Wine lacks, fonts, DXVK, .NET and the XAML Islands stand-in. Downloads and generated files
// are cached under runtime/.
package verbs

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/a-kaibu/vnbox/internal/wine"
)

// Apply installs one verb ("icu", "dotnet:8:x64:desktop" ...) into env's prefix.
func Apply(ctx context.Context, env wine.Env, verb string) error {
	name, rest, _ := strings.Cut(verb, ":")
	// parallel checks (vnbox verify -j) share the downloads and generated files under runtime/
	mu := verbLock(name)
	mu.Lock()
	defer mu.Unlock()
	var args []string
	if rest != "" {
		args = strings.Split(rest, ":")
	}
	var err error
	switch name {
	case "icu":
		err = ICU(ctx, env)
	case "msfonts":
		err = MSFonts(ctx, env)
	case "dxvk":
		err = DXVK(ctx, env)
	case "vcrun":
		err = VCRun(ctx, env)
	case "dotnet":
		err = DotNet(ctx, env, args)
	case "xamlstub":
		err = XamlStub(ctx, env)
	case "sdrt":
		err = SoftDenchi(ctx, env)
	default:
		return fmt.Errorf("unknown verb vnbox-%s", name)
	}
	if err != nil {
		return fmt.Errorf("vnbox-%s: %w", verb, err)
	}
	env.Wait()
	return nil
}

var (
	locksMu sync.Mutex
	locks   = map[string]*sync.Mutex{}
)

func verbLock(name string) *sync.Mutex {
	locksMu.Lock()
	defer locksMu.Unlock()
	if locks[name] == nil {
		locks[name] = &sync.Mutex{}
	}
	return locks[name]
}

// cache is the directory for downloaded and generated files of a verb.
func cache(name string) string { return filepath.Join(wine.Root(), "runtime", name) }

func win64(env wine.Env) bool {
	return env.Flavor == wine.Vnbox || env.Win64 || wine.IsWin64(env.Prefix)
}

// sysDirs returns where 64-bit and 32-bit DLLs go: system32 / syswow64 in a 64-bit prefix,
// "" / system32 in a 32-bit one.
func sysDirs(env wine.Env) (dir64, dir32 string) {
	w := filepath.Join(env.Prefix, "drive_c", "windows")
	if win64(env) {
		return filepath.Join(w, "system32"), filepath.Join(w, "syswow64")
	}
	return "", filepath.Join(w, "system32")
}

// copyInto copies the files matching pattern into dir. The prefix shares files with its base
// through hard links, so existing files are replaced, never written through.
func copyInto(pattern, dir string) error {
	files, _ := filepath.Glob(pattern)
	if len(files) == 0 {
		return fmt.Errorf("nothing matches %s", pattern)
	}
	for _, f := range files {
		if err := copyFile(f, filepath.Join(dir, filepath.Base(f))); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	os.Remove(dst)
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// download fetches url into path (through a temporary file) unless path already exists.
func download(ctx context.Context, url, path string) error {
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// unzip extracts the entries of a zip file accepted by keep (all when nil) below dst,
// at the path returned by keep.
func unzip(src, dst string, keep func(name string) (string, bool)) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	root, _ := filepath.Abs(dst)
	for _, f := range r.File {
		name := f.Name
		if keep != nil {
			var ok bool
			if name, ok = keep(name); !ok {
				continue
			}
		}
		p := filepath.Join(root, filepath.FromSlash(name))
		if !strings.HasPrefix(p, root+string(filepath.Separator)) {
			return fmt.Errorf("unsafe path in %s: %q", src, f.Name)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(p, 0o755)
			continue
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
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.Remove(p)
	w, err := os.Create(p)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, rc); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// untarGz extracts a .tar.gz below dst.
func untarGz(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	root, _ := filepath.Abs(dst)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		p := filepath.Join(root, filepath.FromSlash(h.Name))
		if !strings.HasPrefix(p, root+string(filepath.Separator)) {
			return fmt.Errorf("unsafe path in %s: %q", src, h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(p, 0o755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(p), 0o755)
			w, err := os.Create(p)
			if err != nil {
				return err
			}
			if _, err := io.Copy(w, tr); err != nil {
				w.Close()
				return err
			}
			w.Close()
		}
	}
}

// ICU puts the icu.dll forwarder (Windows 10 has icu.dll, Wine does not; AdvHD fails with
// c0000135 without it) and the official ICU DLLs it forwards to into system32 / syswow64.
// runtime/icu/{32,64} is built on first use (buildICU).
func ICU(ctx context.Context, env wine.Env) error {
	if _, err := os.Stat(filepath.Join(cache("icu"), "32", "icu.dll")); err != nil {
		if err := buildICU(ctx, cache("icu")); err != nil {
			return err
		}
	}
	d64, d32 := sysDirs(env)
	if d64 != "" {
		if err := copyInto(filepath.Join(cache("icu"), "64", "*.dll"), d64); err != nil {
			return err
		}
	}
	return copyInto(filepath.Join(cache("icu"), "32", "*.dll"), d32)
}

// DXVKVersion is the DXVK release used when downloads/dxvk holds none.
const DXVKVersion = "2.7.1"

// DXVK installs DXVK (Direct3D 8-11 on Vulkan): Unity 6 and SharpDX titles stay black with wined3d.
func DXVK(ctx context.Context, env wine.Env) error {
	dirs, _ := filepath.Glob(filepath.Join(wine.Root(), "downloads", "dxvk", "dxvk-*", "x32"))
	if len(dirs) == 0 {
		tgz := filepath.Join(wine.Root(), "downloads", "dxvk", "dxvk-"+DXVKVersion+".tar.gz")
		url := fmt.Sprintf("https://github.com/doitsujin/dxvk/releases/download/v%s/dxvk-%s.tar.gz", DXVKVersion, DXVKVersion)
		if err := download(ctx, url, tgz); err != nil {
			return err
		}
		if err := untarGz(tgz, filepath.Dir(tgz)); err != nil {
			return err
		}
		dirs, _ = filepath.Glob(filepath.Join(wine.Root(), "downloads", "dxvk", "dxvk-*", "x32"))
		if len(dirs) == 0 {
			return errors.New("no x32 folder in the DXVK archive")
		}
	}
	d := filepath.Dir(dirs[len(dirs)-1])
	if want := filepath.Join(wine.Root(), "downloads", "dxvk", "dxvk-"+DXVKVersion); slices.Contains(dirs, filepath.Join(want, "x32")) {
		d = want // names sort as text (dxvk-2.10 < dxvk-2.7): prefer the known version
	}
	d64, d32 := sysDirs(env)
	if d64 != "" {
		if err := copyInto(filepath.Join(d, "x64", "*.dll"), d64); err != nil {
			return err
		}
	}
	if err := copyInto(filepath.Join(d, "x32", "*.dll"), d32); err != nil {
		return err
	}
	for _, dll := range []string{"d3d8", "d3d9", "d3d10core", "d3d11", "dxgi"} {
		if err := env.Reg(ctx, "add", `HKCU\Software\Wine\DllOverrides`, "/v", dll, "/d", "native", "/f"); err != nil {
			return err
		}
	}
	return nil
}

// Package winebuild builds vnbox's own Wine: the Wine release's source with the patches in
// wine-build/ (built into vnbox), configured for the new WoW64 (32-bit programs in a 64-bit
// prefix) and installed under <root>/wine, where wine.Vnbox finds it.
package winebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/a-kaibu/vnbox"
	"github.com/a-kaibu/vnbox/internal/wine"
)

// Version is the Wine release the patches are made for.
const Version = "11.0"

// Patches are applied in this order (wine-build/ of the repository).
var Patches = []string{
	"third_party/fitudao-wine-patches/softdenchi-fixes.patch",
	"vnbox-quartz-seek-deadlock.patch",
	"vnbox-kernelbase-mitigation-policy.patch",
	"vnbox-ntdll-writecopy-oldprot.patch",
	"vnbox-qasf-dmo-no-output-loop.patch",
	"vnbox-wined3d-read-scanline.patch",
	"vnbox-kernelbase-safe-disk-free.patch",
	"vnbox-wmreader-allocator-retry.patch",
	"vnbox-quartz-videowindow-unlocked.patch",
	"vnbox-win32u-own-frame-visible.patch",
	"vnbox-win32u-mouse-in-pointer.patch",
}

// configureArgs are the configure options besides --prefix (new WoW64, no tests).
var configureArgs = []string{"--enable-archs=i386,x86_64", "--disable-tests"}

// Repo is the GitHub repository whose releases hold the built Wine.
const Repo = "a-kaibu/vnbox"

// Tag names the release of the Wine these patches make: wine-<version>-vnbox-<hash>, the hash
// being over the version, the configure options and the patches. A vnbox binary therefore
// always fetches the Wine of the patches built into it, and changing a patch makes a new one.
func Tag() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n", Version, strings.Join(configureArgs, " "))
	for _, p := range Patches {
		b, _ := vnbox.Files.ReadFile("wine-build/" + p)
		fmt.Fprintf(h, "%s %d\n", p, len(b))
		h.Write(b)
	}
	return "wine-" + Version + "-vnbox-" + hex.EncodeToString(h.Sum(nil))[:10]
}

// Asset is the file name of the built Wine in the release: a tar.xz holding wine-vnbox/
// (bin/, lib/, share/), which runs from any folder.
func Asset() string { return Tag() + "-x86_64.tar.xz" }

// tagFile records in the installed Wine the release it came from.
const tagFile = ".vnbox-tag"

// Install downloads the release of Tag() and unpacks it into <root>/wine (replacing an older
// one). It fails when no build of these patches is published yet (then build-wine makes it).
func Install(ctx context.Context, log io.Writer) error {
	if log == nil {
		log = io.Discard
	}
	dl := filepath.Join(wine.Root(), "downloads")
	if err := os.MkdirAll(dl, 0o755); err != nil {
		return err
	}
	archive := filepath.Join(dl, Asset())
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", Repo, Tag(), Asset())
	if err := download(ctx, url, archive, log); err != nil {
		if errors.Is(err, errNotFound) {
			return fmt.Errorf("no published build of this Wine (%s) yet: make it with vnbox build-wine", Tag())
		}
		return err
	}
	defer os.Remove(archive)
	tmp := filepath.Join(wine.Root(), "wine.new")
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if out, err := exec.CommandContext(ctx, "tar", "-xJf", archive, "-C", tmp).CombinedOutput(); err != nil {
		return fmt.Errorf("unpack %s: %v: %s", Asset(), err, out)
	}
	dst := filepath.Join(wine.Root(), "wine")
	if err := os.WriteFile(filepath.Join(tmp, "wine-vnbox", tagFile), []byte(Tag()+"\n"), 0o644); err != nil {
		return err
	}
	os.RemoveAll(dst)
	return os.Rename(filepath.Join(tmp, "wine-vnbox"), dst)
}

// Outdated reports whether <root>/wine came from a release other than Tag() (the patches
// built into this vnbox changed since it was installed). A Wine built here is never outdated.
func Outdated() bool {
	b, err := os.ReadFile(filepath.Join(wine.Root(), "wine", tagFile))
	return err == nil && strings.TrimSpace(string(b)) != Tag()
}

// Options of Build.
type Options struct {
	Jobs   int    // make -j (0 = number of CPUs)
	Prefix string // where to install ("" = <root>/wine)
	Log    io.Writer
}

// Build downloads the source, patches, configures, builds and installs it. The work folder
// (<root>/wine-build-src) stays for a rebuild; a finished step is not repeated.
func Build(ctx context.Context, o Options) error {
	if o.Prefix == "" {
		o.Prefix = filepath.Join(wine.Root(), "wine")
	}
	if o.Log == nil {
		o.Log = io.Discard
	}
	work := filepath.Join(wine.Root(), "wine-build-src")
	src := filepath.Join(work, "wine-"+Version)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	run := func(dir, name string, args ...string) error {
		fmt.Fprintf(o.Log, "+ %s %v\n", name, args)
		c := exec.CommandContext(ctx, name, args...)
		c.Dir, c.Stdout, c.Stderr = dir, o.Log, o.Log
		if err := c.Run(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	}
	if _, err := os.Stat(filepath.Join(src, ".vnbox-patched")); err != nil {
		tarball := filepath.Join(work, "wine-"+Version+".tar.xz")
		if _, err := os.Stat(tarball); err != nil {
			if err := download(ctx, fmt.Sprintf("https://dl.winehq.org/wine/source/%s/wine-%s.tar.xz", Version, Version), tarball, o.Log); err != nil {
				return err
			}
		}
		os.RemoveAll(src) // a half-patched tree from an earlier try
		if err := run(work, "tar", "-xJf", tarball); err != nil {
			return err
		}
		patches, err := wine.Extract("wine-build")
		if err != nil {
			return err
		}
		for _, p := range Patches {
			if err := run(src, "patch", "-p1", "--forward", "-i", filepath.Join(patches, filepath.FromSlash(p))); err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
		}
		if err := os.WriteFile(filepath.Join(src, ".vnbox-patched"), nil, 0o644); err != nil {
			return err
		}
	}
	build := filepath.Join(work, "build")
	if _, err := os.Stat(filepath.Join(build, "Makefile")); err != nil {
		if err := os.MkdirAll(build, 0o755); err != nil {
			return err
		}
		// no debug information: the installed Wine is a third of the size
		args := append([]string{"--prefix=" + o.Prefix, "CFLAGS=-O2", "CROSSCFLAGS=-O2"}, configureArgs...)
		if err := run(build, filepath.Join(src, "configure"), args...); err != nil {
			return err
		}
	}
	jobs := o.Jobs
	if jobs <= 0 {
		jobs = runtime.NumCPU()
	}
	if err := run(build, "make", fmt.Sprintf("-j%d", jobs)); err != nil {
		return err
	}
	return run(build, "make", "install")
}

func download(ctx context.Context, url, dst string, log io.Writer) error {
	fmt.Fprintf(log, "+ download %s\n", url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	f, err := os.Create(dst + ".part")
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(dst+".part", dst)
}

var errNotFound = errors.New("not found")

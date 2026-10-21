package verbs

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/a-kaibu/vnbox/internal/wine"
)

// vcDLLs get native,builtin overrides so the installed runtime wins over Wine's own msvcp140.
var vcDLLs = []string{"msvcp140", "msvcp140_1", "msvcp140_2", "msvcp140_atomic_wait", "msvcp140_codecvt_ids",
	"vcruntime140", "vcruntime140_1", "vcruntime140_threads", "concrt140", "vcomp140"}

// VCRun installs the latest Visual C++ 2015-2022 runtime DLLs and registers their version under
// VC\Runtimes. Unreal Engine 5 checks both before starting and otherwise asks for the
// redistributable. The DLLs are taken out of vc_redist's cabinets (cabextract) instead of
// running the installer under Wine.
func VCRun(ctx context.Context, env wine.Env) error {
	d64, d32 := sysDirs(env)
	for _, arch := range []string{"x64", "x86"} {
		sys := d32
		if arch == "x64" {
			if d64 == "" {
				continue
			}
			sys = d64
		}
		dir := filepath.Join(cache("vcrun"), arch)
		if err := fetchVCRun(ctx, arch, dir); err != nil {
			return err
		}
		if err := copyInto(filepath.Join(dir, "*.dll"), sys); err != nil {
			return err
		}
		v, err := fileVersion(filepath.Join(dir, "msvcp140.dll"))
		if err != nil {
			return err
		}
		reg := fmt.Sprintf("REGEDIT4\n\n[HKEY_LOCAL_MACHINE\\SOFTWARE\\Microsoft\\VisualStudio\\14.0\\VC\\Runtimes\\%s]\n"+
			"\"Installed\"=dword:00000001\n\"Major\"=dword:%08x\n\"Minor\"=dword:%08x\n\"Bld\"=dword:%08x\n\"Rbld\"=dword:%08x\n"+
			"\"Version\"=\"v%d.%d.%d.%02d\"\n", arch, v[0], v[1], v[2], v[3], v[0], v[1], v[2], v[3])
		// x86 in a 64-bit prefix goes to the 32-bit view (WOW6432Node), where 32-bit games look
		wow := arch == "x86" && d64 != ""
		if err := env.ImportReg(ctx, reg, !wow, wow); err != nil {
			return err
		}
	}
	for _, dll := range vcDLLs {
		if err := env.Reg(ctx, "add", `HKCU\Software\Wine\DllOverrides`, "/v", dll, "/d", "native,builtin", "/f"); err != nil {
			return err
		}
	}
	return nil
}

func fetchVCRun(ctx context.Context, arch, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "msvcp140.dll")); err == nil {
		return nil
	}
	tmp := dir + ".tmp"
	os.RemoveAll(tmp)
	defer os.RemoveAll(tmp)
	exe := filepath.Join(tmp, "vc_redist.exe")
	if err := download(ctx, "https://aka.ms/vc14/vc_redist."+arch+".exe", exe); err != nil {
		return err
	}
	// the bundle carries the MSI cabinets as attached containers (a0, a1 ...); the DLLs inside
	// are named <dll>_amd64 / <dll>_x86
	exec.CommandContext(ctx, "cabextract", "-q", "-d", filepath.Join(tmp, "b"), exe).Run()
	suf := "_x86"
	if arch == "x64" {
		suf = "_amd64"
	}
	cabs, _ := filepath.Glob(filepath.Join(tmp, "b", "a*"))
	for _, cab := range cabs {
		exec.CommandContext(ctx, "cabextract", "-q", "-d", filepath.Join(tmp, "d"), "-F", "*.dll"+suf, cab).Run()
	}
	dlls, _ := filepath.Glob(filepath.Join(tmp, "d", "*.dll"+suf))
	if len(dlls) == 0 {
		return fmt.Errorf("no DLLs found in vc_redist.%s.exe", arch)
	}
	os.RemoveAll(dir)
	for _, f := range dlls {
		b := strings.TrimSuffix(filepath.Base(f), suf)
		if strings.HasPrefix(b, "mfc") || strings.HasPrefix(b, "vcamp") || strings.HasPrefix(b, "vccorlib") {
			continue
		}
		if err := copyFile(f, filepath.Join(dir, b)); err != nil {
			return err
		}
	}
	return nil
}

// fileVersion reads the file version from a PE's VS_FIXEDFILEINFO.
func fileVersion(path string) ([4]uint32, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return [4]uint32{}, err
	}
	sig := binary.LittleEndian.AppendUint32(nil, 0xfeef04bd)
	i := bytes.Index(b, sig)
	if i < 0 || i+16 > len(b) {
		return [4]uint32{}, fmt.Errorf("%s: no version resource", path)
	}
	ms, ls := binary.LittleEndian.Uint32(b[i+8:]), binary.LittleEndian.Uint32(b[i+12:])
	return [4]uint32{ms >> 16, ms & 0xffff, ls >> 16, ls & 0xffff}, nil
}

// DotNet unpacks the .NET runtime into Program Files\dotnet: args are major version, x64|x86
// and optionally "desktop" for the WindowsDesktop runtime (WPF / WinForms). Apps built for
// .NET 5+ (apphost) refuse to start with ".NET location: Not found" otherwise. The latest
// release of that major version is taken from Microsoft's release metadata.
func DotNet(ctx context.Context, env wine.Env, args []string) error {
	if len(args) < 1 {
		return errors.New("usage: vnbox-dotnet:<major>[:x64|x86[:desktop]]")
	}
	major, arch, kind := args[0], "x64", ""
	if len(args) > 1 {
		arch = args[1]
	}
	if len(args) > 2 {
		kind = args[2]
	}
	rel := filepath.Join(cache("dotnet"), "releases-"+major+".json")
	if err := download(ctx, "https://builds.dotnet.microsoft.com/dotnet/release-metadata/"+major+".0/releases.json", rel); err != nil {
		return err
	}
	var meta struct {
		Releases []map[string]json.RawMessage `json:"releases"`
	}
	b, err := os.ReadFile(rel)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &meta); err != nil || len(meta.Releases) == 0 {
		return fmt.Errorf("%s: no releases", rel)
	}
	keys := []string{"runtime"}
	if kind == "desktop" {
		keys = append(keys, "windowsdesktop")
	}
	dst := filepath.Join(env.Prefix, "drive_c", "Program Files", "dotnet")
	if arch == "x86" && win64(env) {
		dst = filepath.Join(env.Prefix, "drive_c", "Program Files (x86)", "dotnet")
	}
	for _, k := range keys {
		var part struct {
			Files []struct{ Name, Rid, URL string }
		}
		if err := json.Unmarshal(meta.Releases[0][k], &part); err != nil {
			return fmt.Errorf("%s: %s: %w", rel, k, err)
		}
		found := false
		for _, f := range part.Files {
			if f.Rid != "win-"+arch || !strings.HasSuffix(f.Name, ".zip") {
				continue
			}
			z := filepath.Join(cache("dotnet"), filepath.Base(f.URL))
			if err := download(ctx, f.URL, z); err != nil {
				return err
			}
			if err := unzip(z, dst, nil); err != nil {
				return err
			}
			found = true
		}
		if !found {
			return fmt.Errorf(".NET %s %s win-%s: no zip in the release metadata", major, k, arch)
		}
	}
	return nil
}

// sdkContracts is the NuGet package carrying the Windows SDK metadata (.winmd) xamlstub is
// generated from.
const sdkContracts = "10.0.28000.2705"

// XamlStub installs xamlstub32.dll, a do-nothing WinRT implementation of the XAML Islands
// classes (Windows.UI.Xaml.Hosting.DesktopWindowXamlSource ...), and registers each class.
// Wine has no Windows.UI.Xaml and the game dies on an unhandled exception without it
// (せをはやみ。). The DLL is generated from the Windows SDK metadata on first use by
// tools/xamlstub (gen.py writes the C vtables, MinGW builds them).
func XamlStub(ctx context.Context, env wine.Env) error {
	dll := filepath.Join(cache("xamlstub"), "xamlstub32.dll")
	if _, err := os.Stat(dll); err != nil {
		if err := buildXamlStub(ctx, dll); err != nil {
			return err
		}
	}
	if err := copyFile(dll, filepath.Join(env.Prefix, "drive_c", "vnbox", "xamlstub32.dll")); err != nil {
		return err
	}
	src, err := wine.Extract("tools/xamlstub")
	if err != nil {
		return err
	}
	classes, err := os.ReadFile(filepath.Join(src, "classes.txt"))
	if err != nil {
		return err
	}
	var reg strings.Builder
	reg.WriteString("REGEDIT4\n")
	for _, c := range strings.Fields(string(classes)) {
		fmt.Fprintf(&reg, "\n[HKEY_LOCAL_MACHINE\\Software\\Microsoft\\WindowsRuntime\\ActivatableClassId\\%s]\n"+
			"\"DllPath\"=\"C:\\\\vnbox\\\\xamlstub32.dll\"\n\"ActivationType\"=dword:00000000\n", c)
	}
	return env.ImportReg(ctx, reg.String(), true, true)
}

func buildXamlStub(ctx context.Context, dll string) error {
	src, err := wine.Extract("tools/xamlstub")
	if err != nil {
		return err
	}
	winmd := filepath.Join(cache("xamlstub"), "winmd")
	if m, _ := filepath.Glob(filepath.Join(winmd, "*.winmd")); len(m) == 0 {
		nupkg := filepath.Join(cache("xamlstub"), "sdk-contracts-"+sdkContracts+".nupkg")
		url := fmt.Sprintf("https://api.nuget.org/v3-flatcontainer/microsoft.windows.sdk.contracts/%s/microsoft.windows.sdk.contracts.%s.nupkg", sdkContracts, sdkContracts)
		if err := download(ctx, url, nupkg); err != nil {
			return err
		}
		err := unzip(nupkg, winmd, func(n string) (string, bool) {
			return filepath.Base(n), strings.HasPrefix(n, "ref/netstandard2.0/") && strings.HasSuffix(n, ".winmd")
		})
		if err != nil {
			return err
		}
	}
	venv := filepath.Join(cache("xamlstub"), "venv")
	py := filepath.Join(venv, "bin", "python")
	if _, err := os.Stat(py); err != nil {
		if out, err := exec.CommandContext(ctx, "python3", "-m", "venv", venv).CombinedOutput(); err != nil {
			return fmt.Errorf("venv: %v: %s", err, out)
		}
		if out, err := exec.CommandContext(ctx, filepath.Join(venv, "bin", "pip"), "install", "-q", "dnfile").CombinedOutput(); err != nil {
			return fmt.Errorf("pip install dnfile: %v: %s", err, out)
		}
	}
	tmp, err := os.MkdirTemp("", "xamlstub")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	gen := filepath.Join(tmp, "gen.c")
	c := exec.CommandContext(ctx, py, filepath.Join(src, "gen.py"), winmd, gen) // gen.py imports winmd.py next to it
	c.Dir = src
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("gen.py: %v: %s", err, out)
	}
	os.MkdirAll(filepath.Dir(dll), 0o755)
	c = exec.CommandContext(ctx, "i686-w64-mingw32-gcc", "-O1", "-shared", "-o", dll, "runtime.c", gen, "-I.",
		"-lruntimeobject", "-Wl,--kill-at")
	c.Dir = src
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("build xamlstub32.dll: %v: %s", err, out)
	}
	return nil
}

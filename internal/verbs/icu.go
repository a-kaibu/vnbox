package verbs

import (
	"bytes"
	"context"
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// icuRelease is the ICU the icu.dll forwarder is built against.
const icuRelease, icuMajor = "74_2", "74"

// buildICU makes runtime/icu/{32,64}: Windows 10's icu.dll exports the ICU functions without
// the version suffix (u_strlen, ...); here it is a DLL that forwards each of them to the
// official ICU build (icuuc74.dll u_strlen_74, ...), next to those DLLs.
func buildICU(ctx context.Context, out string) error {
	for _, arch := range []string{"32", "64"} {
		zipName := fmt.Sprintf("icu4c-%s-Win%s-MSVC2019.zip", icuRelease, arch)
		z := filepath.Join(cache("icu"), "src", zipName)
		url := fmt.Sprintf("https://github.com/unicode-org/icu/releases/download/release-%s/%s",
			strings.ReplaceAll(icuRelease, "_", "-"), zipName)
		if err := download(ctx, url, z); err != nil {
			return err
		}
		dir := filepath.Join(out, arch)
		os.MkdirAll(dir, 0o755)
		err := unzip(z, dir, func(n string) (string, bool) {
			b := filepath.Base(n)
			return b, strings.HasPrefix(n, "bin") && strings.HasPrefix(b, "icu") && strings.HasSuffix(b, icuMajor+".dll")
		})
		if err != nil {
			return err
		}
		var def bytes.Buffer
		def.WriteString("LIBRARY icu\nEXPORTS\n")
		for _, lib := range []string{"icuuc", "icuin"} {
			names, err := exports(filepath.Join(dir, lib+icuMajor+".dll"))
			if err != nil {
				return err
			}
			for _, n := range names {
				if base, ok := strings.CutSuffix(n, "_"+icuMajor); ok {
					fmt.Fprintf(&def, "  %s=%s%s.%s\n", base, lib, icuMajor, n)
				}
			}
		}
		tmp, err := os.MkdirTemp("", "icu")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		os.WriteFile(filepath.Join(tmp, "icu.def"), def.Bytes(), 0o644)
		os.WriteFile(filepath.Join(tmp, "stub.c"), []byte("int __stdcall DllMain(void*h,unsigned r,void*p){return 1;}\n"), 0o644)
		cc, entry := "i686-w64-mingw32-gcc", "_DllMain@12"
		if arch == "64" {
			cc, entry = "x86_64-w64-mingw32-gcc", "DllMain"
		}
		c := exec.CommandContext(ctx, cc, "-shared", "-nostdlib", "-o", filepath.Join(dir, "icu.dll"), "stub.c", "icu.def",
			"-Wl,--entry,"+entry, "-Wl,--enable-stdcall-fixup")
		c.Dir = tmp
		if o, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("build icu.dll (win%s): %v: %s", arch, err, o)
		}
	}
	return nil
}

// exports lists the names a PE DLL exports.
func exports(path string) ([]string, error) {
	f, err := pe.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var dd pe.DataDirectory
	switch h := f.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		dd = h.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_EXPORT]
	case *pe.OptionalHeader64:
		dd = h.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_EXPORT]
	default:
		return nil, errors.New("no optional header")
	}
	data := map[*pe.Section][]byte{}
	read := func(rva, n uint32) ([]byte, error) {
		for _, s := range f.Sections {
			if rva >= s.VirtualAddress && rva+n <= s.VirtualAddress+s.Size {
				if data[s] == nil {
					if data[s], err = s.Data(); err != nil {
						return nil, err
					}
				}
				off := rva - s.VirtualAddress
				if int(off+n) > len(data[s]) {
					return nil, fmt.Errorf("rva %#x past the section data", rva)
				}
				return data[s][off : off+n], nil
			}
		}
		return nil, fmt.Errorf("rva %#x not in a section", rva)
	}
	d, err := read(dd.VirtualAddress, 40)
	if err != nil {
		return nil, err
	}
	count, namesRVA := binary.LittleEndian.Uint32(d[24:]), binary.LittleEndian.Uint32(d[32:])
	ptrs, err := read(namesRVA, 4*count)
	if err != nil {
		return nil, err
	}
	var names []string
	for i := uint32(0); i < count; i++ {
		rva := binary.LittleEndian.Uint32(ptrs[4*i:])
		var name []byte
		for {
			b, err := read(rva+uint32(len(name)), 1)
			if err != nil || b[0] == 0 {
				break
			}
			name = append(name, b[0])
		}
		names = append(names, string(name))
	}
	return names, nil
}

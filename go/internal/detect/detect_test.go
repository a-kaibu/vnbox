package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPEArch(t *testing.T) {
	for path, want := range map[string]string{
		"/opt/wine-vnbox/lib/wine/i386-windows/notepad.exe":   "x86",
		"/opt/wine-vnbox/lib/wine/x86_64-windows/notepad.exe": "x64",
	} {
		if _, err := os.Stat(path); err != nil {
			t.Skip("Wine build not installed")
		}
		if got := PEArch(path); got != want {
			t.Errorf("%s: got %s want %s", path, got, want)
		}
	}
	if got := PEArch("/etc/hostname"); got != "" {
		t.Errorf("non-PE file: %q", got)
	}
}

func TestDetectAndMainExe(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, size int, content string) {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		b := make([]byte, size)
		copy(b, content)
		os.WriteFile(p, b, 0o644)
	}
	write("game/data.xp3", 10, "")
	write("game/game.exe", 2000, "MZ TVP(KIRIKIRI)")
	write("game/uninstall.exe", 9000, "MZ") // bigger, but a tool
	write("game/設定.exe", 8000, "MZ")        // config tool
	r := Detect(dir)
	if r.Engine != "KiriKiri" {
		t.Errorf("engine %q (scores %v)", r.Engine, r.Scores)
	}
	if got := r.MainExe(); filepath.ToSlash(got) != "game/game.exe" {
		t.Errorf("main exe %q", got)
	}
}

func TestDetectProtection(t *testing.T) {
	dir := t.TempDir()
	b := make([]byte, 4096)
	copy(b, "MZ")
	copy(b[0x200:], ".text\x00\x00\x00")
	copy(b[0x228:], ".themida")
	os.WriteFile(filepath.Join(dir, "game.exe"), b, 0o644)
	os.WriteFile(filepath.Join(dir, "auth.dll"), []byte("MZ ... PlayDRM ..."), 0o644)
	r := Detect(dir)
	has := func(tag string) bool {
		for _, t := range r.Tags {
			if t == tag {
				return true
			}
		}
		return false
	}
	if !has("Protect:Themida") || !has("DRM:PlayDRM") {
		t.Errorf("tags %v", r.Tags)
	}
}

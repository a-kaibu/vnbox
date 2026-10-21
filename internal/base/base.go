// Package base creates the base Wine prefix every game prefix is cloned from: Japanese
// locale (code page 932), Windows 10 (64-bit) or 7 (32-bit), Japanese font replacements,
// real fonts named ＭＳ ゴシック etc., icu.dll, no virtual desktop.
package base

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/a-kaibu/vnbox/internal/verbs"
	"github.com/a-kaibu/vnbox/internal/wine"
)

// settings are written into every base prefix.
var settings = []struct{ key, name, typ, data string }{
	// no XRandR / XVidMode: a game asking for fullscreen must not change the X server's mode
	{`HKCU\Software\Wine\X11 Driver`, "UseXRandR", "", "N"},
	{`HKCU\Software\Wine\X11 Driver`, "UseXVidMode", "", "N"},
	// old Japanese games stop at the "start a debugger?" dialog when they crash
	{`HKLM\Software\Microsoft\Windows NT\CurrentVersion\AeDebug`, "Auto", "", "0"},
	{`HKLM\Software\Microsoft\Windows NT\CurrentVersion\AeDebug`, "Debugger", "", ""},
	{`HKCU\Software\Wine\WineDbg`, "ShowCrashDialog", "REG_DWORD", "0"},
	// Chromium (Electron / NW.js) ships its own Vulkan loader for SwiftShader. Since Wine 11.x
	// the builtin vulkan-1 wins, ANGLE then misses SwiftShader, renders on the host's lavapipe
	// and grows past 12 GB (梨乃ねね 子種を乞う村). Games without their own keep the builtin.
	{`HKCU\Software\Wine\DllOverrides`, "vulkan-1", "", "native,builtin"},
}

// fontReplacements map Windows Japanese font names to the IPA / Noto fonts of the host.
var fontReplacements = [][2]string{
	{"MS Gothic", "IPAGothic"}, {"MS PGothic", "IPAPGothic"}, {"MS UI Gothic", "IPAPGothic"},
	{"MS Mincho", "IPAMincho"}, {"MS PMincho", "IPAPMincho"},
	{"ＭＳ ゴシック", "IPAGothic"}, {"ＭＳ Ｐゴシック", "IPAPGothic"},
	{"ＭＳ 明朝", "IPAMincho"}, {"ＭＳ Ｐ明朝", "IPAPMincho"},
	{"Meiryo", "Noto Sans CJK JP"}, {"Meiryo UI", "Noto Sans CJK JP"}, {"メイリオ", "Noto Sans CJK JP"},
	{"Yu Gothic", "Noto Sans CJK JP"}, {"Yu Gothic UI", "Noto Sans CJK JP"}, {"游ゴシック", "Noto Sans CJK JP"},
	{"Yu Mincho", "Noto Serif CJK JP"}, {"游明朝", "Noto Serif CJK JP"},
}

// Make creates the base prefix dir for the flavor. win64 selects a 64-bit Windows 10 prefix
// (always so with the vnbox Wine, whose new WoW64 runs 32-bit games in it too); otherwise a
// 32-bit Windows 7 one.
func Make(ctx context.Context, fl wine.Flavor, win64 bool, dir string, log io.Writer) error {
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("%s already exists", dir)
	}
	if fl == wine.Vnbox {
		win64 = true
	}
	// a Japanese code page needs the locale to exist when the prefix is created
	if out, _ := exec.Command("locale", "-a").Output(); !strings.Contains(strings.ToLower(string(out)), "ja_jp.utf8") {
		return errors.New("locale ja_JP.UTF-8 is missing (locale-gen ja_JP.UTF-8)")
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	display := wine.FreeDisplay("mkbase")
	if err := wine.EnsureX(display); err != nil {
		return err
	}
	defer wine.StopX(display)
	env := wine.Env{Flavor: fl, Prefix: dir, Display: display, Win64: win64}
	defer env.Kill()
	fmt.Fprintf(log, "creating %s (%s Wine, %s)\n", dir, fl, map[bool]string{true: "64-bit", false: "32-bit"}[win64])
	if err := env.Run(ctx, log, "wineboot", "-i"); err != nil {
		return fmt.Errorf("wineboot: %w", err)
	}
	env.Wait()
	ver := "win7"
	if win64 {
		ver = "win10"
	}
	if err := env.Reg(ctx, "add", `HKCU\Software\Wine`, "/v", "Version", "/d", ver, "/f"); err != nil {
		return err
	}
	for _, s := range settings {
		args := []string{"add", s.key, "/v", s.name, "/d", s.data, "/f"}
		if s.typ != "" {
			args = append(args, "/t", s.typ)
		}
		if err := env.Reg(ctx, args...); err != nil {
			return fmt.Errorf("reg add %s %s: %w", s.key, s.name, err)
		}
	}
	for _, f := range fontReplacements {
		if err := env.Reg(ctx, "add", `HKCU\Software\Wine\Fonts\Replacements`, "/v", f[0], "/d", f[1], "/f"); err != nil {
			return err
		}
	}
	env.Wait()
	for _, v := range []string{"icu", "msfonts"} {
		if err := verbs.Apply(ctx, env, v); err != nil {
			return err
		}
	}
	out, _ := env.Command(ctx, "", "reg", "query", `HKLM\System\CurrentControlSet\Control\Nls\CodePage`, "/v", "ACP").Output()
	if !strings.Contains(string(out), "932") {
		return fmt.Errorf("code page is not 932: %s", strings.TrimSpace(string(out)))
	}
	env.Kill()
	fmt.Fprintf(log, "base prefix ready: %s (code page 932, %s)\n", dir, ver)
	return nil
}

// Ensure makes the flavor's (32-bit for classic) base prefix when there is none yet, so the
// first vnbox add needs no mkbase. It takes a few minutes (fonts, ICU).
func Ensure(ctx context.Context, fl wine.Flavor, log io.Writer) error {
	dir := fl.BasePrefix(false)
	if _, err := os.Stat(filepath.Join(dir, "system.reg")); err == nil {
		return nil
	}
	fmt.Fprintf(log, "making the base prefix %s (first time only, a few minutes)\n", dir)
	tmp := dir + ".new"
	os.RemoveAll(tmp) // an interrupted earlier try
	if err := Make(ctx, fl, false, tmp, log); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	return os.Rename(tmp, dir)
}

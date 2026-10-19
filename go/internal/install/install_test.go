package install

import (
	"os"
	"path/filepath"
	"testing"
)

func mk(t *testing.T, root string, files map[string]string) {
	for p, c := range files {
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(c), 0o644)
	}
}

func TestFind(t *testing.T) {
	// ready-to-run game with a config tool named setup.exe: not an installer
	a := t.TempDir()
	mk(t, a, map[string]string{"g/data.xp3": "", "g/game.exe": "MZ", "g/setup.exe": "MZ"})
	if got := Find(a); got != "" {
		t.Errorf("game with setup.exe tool: %q", got)
	}
	// launcher menu on top, the real game only inside setup/: installer
	b := t.TempDir()
	mk(t, b, map[string]string{"g/launcher.exe": "MZ", "g/setup/INSTALL.exe": "MZ", "g/setup/cs2.exe": "MZ", "g/setup/scene.int": ""})
	if got := Find(b); filepath.Base(got) != "INSTALL.exe" {
		t.Errorf("installer in subfolder: %q", got)
	}
	// only an Inno Setup installer in a subfolder: installer
	d := t.TempDir()
	mk(t, d, map[string]string{"G web/setup.exe": "MZ", "G web/setup-1.bin": ""})
	if got := Find(d); filepath.Base(got) != "setup.exe" {
		t.Errorf("lone setup.exe: %q", got)
	}
	// ready-to-run KiriKiri game that ships an installer.exe and a WinRAR setup next to it
	e := t.TempDir()
	mk(t, e, map[string]string{"t/installer.exe": "MZ", "t/gamedata/data.xp3": "", "t/gamedata/game.exe": "MZ TVP(KIRIKIRI)xxxxxxxxxxxxxxxxxxxx"})
	if got := Find(e); got != "" {
		t.Errorf("game with bundled installer.exe: %q", got)
	}
	// installer with an arbitrary name, recognised by its signature
	f := t.TempDir()
	mk(t, f, map[string]string{"x/HimeTrial.exe": "MZ InstallShield"})
	if got := Find(f); filepath.Base(got) != "HimeTrial.exe" {
		t.Errorf("signature installer: %q", got)
	}
	// CatSystem2 launcher menu that names the engine, real game only behind setup/INSTALL.exe
	g := t.TempDir()
	mk(t, g, map[string]string{"n/menu.exe": "MZ CatSystem2 CS2CONF", "n/boot.dfn": "", "n/setup/INSTALL.exe": "MZ", "n/setup/cs2.exe": "MZ CatSystem2", "n/setup/scene.int": ""})
	if got := Find(g); filepath.Base(got) != "INSTALL.exe" {
		t.Errorf("launcher menu + installer: %q", got)
	}
	// MSI always wins
	c := t.TempDir()
	mk(t, c, map[string]string{"x/game.msi": "", "x/data.xp3": ""})
	if got := Find(c); filepath.Base(got) != "game.msi" {
		t.Errorf("msi: %q", got)
	}
}

func TestIsInstallerName(t *testing.T) {
	for name, want := range map[string]bool{
		"setup.exe": true, "Install.exe": true, "NSM_MYST_trial_Install.exe": true, "game-setup.exe": true,
		"インストール.exe": true, "UnInstaller.exe": false, "uninstall.exe": false, "BGIForInstalling.exe": false,
		"Game.exe": false, "setup.ini": false, "SetupTool.exe": false,
	} {
		if got := isInstallerName(name); got != want {
			t.Errorf("isInstallerName(%q) = %v, want %v", name, got, want)
		}
	}
}

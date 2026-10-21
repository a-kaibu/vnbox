package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/a-kaibu/vnbox/internal/pkg"
)

func TestSaveFilesSkipsCaches(t *testing.T) {
	prefix := t.TempDir()
	marker := filepath.Join(prefix, pkg.AppliedMarker)
	os.WriteFile(marker, nil, 0o644)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(marker, old, old)
	write := func(rel string) {
		p := filepath.Join(prefix, "drive_c", filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	write("Games/g/save/data01.sav")
	write("users/root/AppData/Local/Microsoft/Windows/INetCache/Content.IE5/index.dat")
	write("users/root/AppData/Local/Temp/x.tmp")
	got, err := SaveFiles(prefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "Games/g/save/data01.sav" {
		t.Errorf("SaveFiles = %v, want only the game's save", got)
	}
}

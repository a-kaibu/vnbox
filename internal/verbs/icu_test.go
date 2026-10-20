package verbs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestBuildICU downloads ICU and builds the forwarder (network and MinGW needed):
// VNBOX_TEST_ICU=1 go test ./internal/verbs -run ICU
func TestBuildICU(t *testing.T) {
	if os.Getenv("VNBOX_TEST_ICU") == "" {
		t.Skip("set VNBOX_TEST_ICU=1")
	}
	out := t.TempDir()
	if err := buildICU(context.Background(), out); err != nil {
		t.Fatal(err)
	}
	for _, arch := range []string{"32", "64"} {
		names, err := exports(filepath.Join(out, arch, "icu.dll"))
		if err != nil {
			t.Fatal(err)
		}
		if len(names) < 1000 {
			t.Errorf("win%s icu.dll exports %d names", arch, len(names))
		}
		for _, f := range []string{"icuuc74.dll", "icuin74.dll", "icudt74.dll"} {
			if _, err := os.Stat(filepath.Join(out, arch, f)); err != nil {
				t.Error(err)
			}
		}
		t.Logf("win%s: %d exports", arch, len(names))
	}
}

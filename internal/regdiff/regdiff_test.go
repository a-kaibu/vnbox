package regdiff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const before = `WINE REGISTRY Version 2
;; All keys relative to \\User\\S-1-5-21-0-0-0-1000

#arch=win64

[Software\\Wine] 1700000000
#time=1d9
"Version"="win10"

[Software\\Wine\\Fonts\\Cache] 1700000000
"a"="1"
`

const after = `WINE REGISTRY Version 2
;; All keys relative to \\User\\S-1-5-21-0-0-0-1000

#arch=win64

[Software\\Wine] 1700000001
#time=1da
"Version"="win10"
"New"=dword:00000001

[Software\\Wine\\Fonts\\Cache] 1700000001
"a"="2"

[Software\\Brand\\Game] 1700000001
"Blob"=hex:01,02,\
  03,04
"Empty"=""

[Software\\Brand\\EmptyKey] 1700000001
`

func write(t *testing.T, dir, name, s string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDiffApply(t *testing.T) {
	dir := t.TempDir()
	a, _ := Parse(write(t, dir, "a.reg", before))
	b, _ := Parse(write(t, dir, "b.reg", after))
	d := Diff(a, b)
	if _, ok := d.Keys[`Software\\Wine\\Fonts\\Cache`]; ok {
		t.Error("noisy key not filtered")
	}
	if k := d.Keys[`Software\\Wine`]; k == nil || len(k.Names) != 1 || k.Values[`"New"`] != "dword:00000001" {
		t.Errorf("changed value: %+v", k)
	}
	if k := d.Keys[`Software\\Brand\\Game`]; k == nil || k.Values[`"Blob"`] != "hex:01,02,\\\n  03,04" || k.Values[`"Empty"`] != `""` {
		t.Errorf("new key / continued hex value: %+v", k)
	}
	if _, ok := d.Keys[`Software\\Brand\\EmptyKey`]; !ok {
		t.Error("new empty key must be kept")
	}
	frag := filepath.Join(dir, "f.reg")
	if err := d.WriteFragment(frag); err != nil {
		t.Fatal(err)
	}
	// applying the fragment to the old file must reproduce every non-noisy value
	target := write(t, dir, "t.reg", before)
	if err := Apply(frag, target); err != nil {
		t.Fatal(err)
	}
	got, _ := Parse(target)
	if got.Keys[`Software\\Wine`].Values[`"New"`] != "dword:00000001" || got.Keys[`Software\\Brand\\Game`].Values[`"Blob"`] == "" {
		t.Error("apply lost values")
	}
	raw, _ := os.ReadFile(target)
	if !strings.HasPrefix(string(raw), "WINE REGISTRY Version 2\n") || !strings.Contains(string(raw), "#arch=win64") {
		t.Errorf("header not kept:\n%s", raw)
	}
	if !strings.Contains(string(raw), "#time=1d9") {
		t.Error("meta lines of existing keys must be kept")
	}
	if again := Diff(got, got); len(again.Keys) != 0 {
		t.Error("diff of identical hives must be empty")
	}
}

func TestUnescape(t *testing.T) {
	if got := Unescape(`\x30b2\x30fc\x30e0`); got != "ゲーム" {
		t.Errorf("got %q", got)
	}
}

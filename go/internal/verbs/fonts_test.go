package verbs

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

func TestRenameFont(t *testing.T) {
	src := msFonts[0].src
	if _, err := os.Stat(src); err != nil {
		t.Skip("IPA Gothic not installed")
	}
	dst := filepath.Join(t.TempDir(), "msgothic.ttf")
	if err := renameFont(src, dst, "MS Gothic", "ＭＳ ゴシック"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if s := checksum(b); s != 0xB1B0AFBA {
		t.Errorf("whole-font checksum = %#x, want 0xB1B0AFBA", s)
	}
	tables, err := readSFNT(b)
	if err != nil {
		t.Fatal(err)
	}
	recs, err := parseNames(tables["name"])
	if err != nil {
		t.Fatal(err)
	}
	got := map[[2]uint16]string{}
	for _, r := range recs {
		if r.platform == 3 && r.encoding == 1 {
			u := make([]uint16, len(r.data)/2)
			for i := range u {
				u[i] = binary.BigEndian.Uint16(r.data[2*i:])
			}
			got[[2]uint16{r.lang, r.nameID}] = string(utf16.Decode(u))
		}
	}
	for k, want := range map[[2]uint16]string{
		{0x409, 1}: "MS Gothic", {0x411, 1}: "ＭＳ ゴシック", {0x411, 4}: "ＭＳ ゴシック",
		{0x409, 2}: "Regular", {0x409, 6}: "MSGothic",
	} {
		if got[k] != want {
			t.Errorf("name %#x/%d = %q, want %q", k[0], k[1], got[k], want)
		}
	}
	if _, ok := tables["glyf"]; !ok {
		t.Error("glyf table lost")
	}
}

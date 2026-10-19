package install

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// fakeISSetup builds a minimal PE whose overlay holds an InstallShield payload.
func fakeISSetup(t *testing.T, files map[string][]byte, flags uint32) string {
	var pe bytes.Buffer
	img := make([]byte, 0x200)
	copy(img, "MZ")
	binary.LittleEndian.PutUint32(img[0x3c:], 0x80)
	copy(img[0x80:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(img[0x80+6:], 1)  // one section
	binary.LittleEndian.PutUint16(img[0x80+20:], 0) // no optional header
	sec := img[0x80+24:]
	binary.LittleEndian.PutUint32(sec[16:], 0x100) // raw size
	binary.LittleEndian.PutUint32(sec[20:], 0x100) // raw offset -> overlay at 0x200
	pe.Write(img)
	hdr := make([]byte, isHeaderLen)
	copy(hdr, "InstallShield\x00")
	binary.LittleEndian.PutUint16(hdr[14:], uint16(len(files)))
	pe.Write(hdr)
	for name, data := range files {
		attr := make([]byte, isAttrLen)
		copy(attr, name)
		binary.LittleEndian.PutUint32(attr[260:], flags)
		binary.LittleEndian.PutUint32(attr[268:], uint32(len(data)))
		pe.Write(attr)
		enc := make([]byte, len(data))
		k := 0
		for i, b := range data {
			if flags&4 != 0 && i%1024 == 0 {
				k = 0
			}
			key := name[k%len(name)] ^ []byte{0x13, 0x35, 0x86, 0x07}[k%len(name)%4]
			x := ^b ^ key // inverse of ^(key ^ swap(e))
			enc[i] = x<<4 | x>>4
			k++
		}
		if flags&2 == 0 {
			enc = data
		}
		pe.Write(enc)
	}
	p := filepath.Join(t.TempDir(), "setup.exe")
	os.WriteFile(p, pe.Bytes(), 0o644)
	return p
}

func TestExtractInstallShield(t *testing.T) {
	big := bytes.Repeat([]byte("MSI data 0123456789 "), 300) // > 1024 to exercise the key restart
	for _, flags := range []uint32{0x6, 0x2, 0x0} {
		exe := fakeISSetup(t, map[string][]byte{"game.msi": big}, flags)
		out := t.TempDir()
		got, err := ExtractInstallShield(exe, out)
		if err != nil || len(got) != 1 {
			t.Fatalf("flags %#x: %v %v", flags, got, err)
		}
		if b, _ := os.ReadFile(got[0]); !bytes.Equal(b, big) {
			t.Fatalf("flags %#x: content differs", flags)
		}
	}
	plain := filepath.Join(t.TempDir(), "x.exe")
	os.WriteFile(plain, []byte("MZ not really"), 0o644)
	if _, err := ExtractInstallShield(plain, t.TempDir()); err != ErrNotInstallShield {
		t.Fatalf("plain exe: %v", err)
	}
}

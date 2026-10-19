package extract

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestZipShiftJISNames(t *testing.T) {
	dir := t.TempDir()
	sj, _ := japanese.ShiftJIS.NewEncoder().String("体験版/ゲーム.exe")
	zp := filepath.Join(dir, "a.zip")
	f, _ := os.Create(zp)
	zw := zip.NewWriter(f)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: sj, Method: zip.Store, NonUTF8: true})
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("MZ"))
	u, _ := zw.CreateHeader(&zip.FileHeader{Name: "readme.txt", Method: zip.Store})
	u.Write([]byte("hi"))
	zw.Close()
	f.Close()

	out := filepath.Join(dir, "out")
	if err := Extract(zp, out); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"体験版/ゲーム.exe", "readme.txt"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
}

func TestZipSlip(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "evil.zip")
	f, _ := os.Create(zp)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("../escape.txt")
	w.Write([]byte("x"))
	zw.Close()
	f.Close()
	if err := unzip(zp, filepath.Join(dir, "out")); err == nil {
		t.Fatal("expected an error for a path escaping the destination")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); err == nil {
		t.Fatal("file escaped the destination")
	}
}

func TestFixNames(t *testing.T) {
	dir := t.TempDir()
	sj, _ := japanese.ShiftJIS.NewEncoder().String("セーブ")
	if err := os.MkdirAll(filepath.Join(dir, sj, sj), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := FixNames(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "セーブ", "セーブ")); err != nil {
		t.Fatal("nested Shift-JIS names were not converted")
	}
}

func TestPickDecoder(t *testing.T) {
	enc := func(e *encoding.Encoder, s string) string { b, _ := e.String(s); return b }
	jp := []string{enc(japanese.ShiftJIS.NewEncoder(), "体験版/データ.xp3"), enc(japanese.ShiftJIS.NewEncoder(), "ゲーム.exe")}
	if pickDecoder(jp) != sjis {
		t.Error("Shift_JIS names not read as CP932")
	}
	cn := []string{enc(simplifiedchinese.GBK.NewEncoder(), "UI素材"), enc(simplifiedchinese.GBK.NewEncoder(), "开始游戏背景.png")}
	if pickDecoder(cn) != gbk {
		t.Error("GBK names not read as GBK")
	}
}

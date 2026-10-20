package probe

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePNG(t *testing.T, path string, w, h int, fill func(x, y int) color.Color) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, fill(x, y))
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestJudge(t *testing.T) {
	black := func(x, y int) color.Color { return color.Black }
	stripes := func(x, y int) color.Color {
		if (x/40)%2 == 0 {
			return color.White
		}
		return color.Black
	}
	cases := []struct {
		name  string
		alive bool
		mism  bool
		log   string
		w, h  int
		fill  func(x, y int) color.Color
		want  string
	}{
		{"ok", true, false, "", 800, 600, stripes, "OK"},
		{"dead", false, false, "", 800, 600, stripes, "NG:dead"},
		{"cut off", true, true, "", 800, 600, stripes, "NG:size_mismatch(client=1280x720 x11=800x600)"},
		{"crash", true, false, "wine: Unhandled exception 0xc0000005", 800, 600, stripes, "NG:crash"},
		{"lock wait", true, false, "err:sync:RtlpWaitForCriticalSection section 0 wait timed out", 800, 600, stripes, "OK"},
		{"small", true, false, "", 300, 200, stripes, "NG:small_window"},
		{"blank", true, false, "", 800, 600, black, "NG:blank(0.000)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "wine.log"), []byte(c.log), 0o644)
			r := &Result{Dir: dir, Alive: c.alive, Mismatch: c.mism, Sizes: "client=1280x720 x11=800x600"}
			r.Shots = []string{
				writePNG(t, filepath.Join(dir, "01_boot.png"), c.w, c.h, c.fill),
				writePNG(t, filepath.Join(dir, "02_a.png"), c.w, c.h, c.fill),
			}
			if got := Judge(r); got != c.want {
				t.Errorf("Judge = %q, want %q", got, c.want)
			}
		})
	}
}

func TestShort(t *testing.T) {
	s := Short(strings.Repeat("あ", 100) + "\nsecond line")
	if r := []rune(s); len(r) != 70 {
		t.Errorf("len = %d runes", len(r))
	}
	if Short("a\tb\nc") != "a b" {
		t.Errorf("Short = %q", Short("a\tb\nc"))
	}
}

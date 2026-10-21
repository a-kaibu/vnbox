package verbs

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/a-kaibu/vnbox/internal/wine"
)

// msFonts are copies of the IPA fonts renamed to the Windows Japanese font names
// (file, source, English name, Japanese name).
var msFonts = []struct{ file, src, en, ja string }{
	{"msgothic.ttf", "/usr/share/fonts/opentype/ipafont-gothic/ipag.ttf", "MS Gothic", "ＭＳ ゴシック"},
	{"mspgothic.ttf", "/usr/share/fonts/opentype/ipafont-gothic/ipagp.ttf", "MS PGothic", "ＭＳ Ｐゴシック"},
	{"msuigothic.ttf", "/usr/share/fonts/opentype/ipafont-gothic/ipagp.ttf", "MS UI Gothic", "MS UI Gothic"},
	{"msmincho.ttf", "/usr/share/fonts/opentype/ipafont-mincho/ipam.ttf", "MS Mincho", "ＭＳ 明朝"},
	{"mspmincho.ttf", "/usr/share/fonts/opentype/ipafont-mincho/ipamp.ttf", "MS PMincho", "ＭＳ Ｐ明朝"},
}

// MSFonts registers real fonts named ＭＳ ゴシック / 明朝 etc. Wine's font replacements do not
// show up in EnumFontFamiliesEx, so games that look the font up by name crashed
// (こすぷれえっち). The renamed IPA fonts are generated once into runtime/msfonts.
func MSFonts(ctx context.Context, env wine.Env) error {
	dir := cache("msfonts")
	fonts := filepath.Join(env.Prefix, "drive_c", "windows", "Fonts")
	for _, f := range msFonts {
		p := filepath.Join(dir, f.file)
		if _, err := os.Stat(p); err != nil {
			if err := renameFont(f.src, p, f.en, f.ja); err != nil {
				return err
			}
		}
		if err := copyFile(p, filepath.Join(fonts, f.file)); err != nil {
			return err
		}
		if err := env.Reg(ctx, "add", `HKLM\Software\Microsoft\Windows NT\CurrentVersion\Fonts`, "/v", f.en+" (TrueType)", "/d", f.file, "/f"); err != nil {
			return err
		}
		env.Reg(ctx, "delete", `HKCU\Software\Wine\Fonts\Replacements`, "/v", f.en, "/f") // the real font wins
	}
	return nil
}

// renameFont writes a copy of the TrueType font src whose family / full / PostScript names are
// en (English) and ja (Japanese).
func renameFont(src, dst, en, ja string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tables, err := readSFNT(b)
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	name, ok := tables["name"]
	if !ok {
		return fmt.Errorf("%s: no name table", src)
	}
	recs, err := parseNames(name)
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	// drop every family / unique / full / PostScript / typographic name, then set ours
	var kept []nameRec
	for _, r := range recs {
		switch r.nameID {
		case 1, 3, 4, 6, 16, 17, 18:
		default:
			kept = append(kept, r)
		}
	}
	set := func(id, lang uint16, s string) {
		for i, r := range kept {
			if r.platform == 3 && r.encoding == 1 && r.lang == lang && r.nameID == id {
				kept[i].data = utf16be(s)
				return
			}
		}
		kept = append(kept, nameRec{3, 1, lang, id, utf16be(s)})
	}
	for _, l := range []struct {
		lang  uint16
		label string
	}{{0x409, en}, {0x411, ja}} {
		set(1, l.lang, l.label)
		set(4, l.lang, l.label)
		set(2, l.lang, "Regular")
		set(3, l.lang, l.label+";vnbox")
	}
	set(6, 0x409, strings.ReplaceAll(en, " ", ""))
	tables["name"] = buildNames(kept)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, writeSFNT(binary.BigEndian.Uint32(b), tables), 0o644)
}

func utf16be(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, 2*len(u))
	for i, c := range u {
		binary.BigEndian.PutUint16(out[2*i:], c)
	}
	return out
}

func readSFNT(b []byte) (map[string][]byte, error) {
	if len(b) < 12 {
		return nil, errors.New("not a font")
	}
	n := int(binary.BigEndian.Uint16(b[4:]))
	if len(b) < 12+16*n {
		return nil, errors.New("truncated table directory")
	}
	t := map[string][]byte{}
	for i := 0; i < n; i++ {
		r := b[12+16*i:]
		off, ln := binary.BigEndian.Uint32(r[8:]), binary.BigEndian.Uint32(r[12:])
		if uint64(off)+uint64(ln) > uint64(len(b)) {
			return nil, fmt.Errorf("table %q out of range", r[:4])
		}
		t[string(r[:4])] = b[off : off+ln]
	}
	return t, nil
}

func checksum(b []byte) uint32 {
	var sum uint32
	for i := 0; i < len(b); i += 4 {
		var w [4]byte
		copy(w[:], b[i:])
		sum += binary.BigEndian.Uint32(w[:])
	}
	return sum
}

// writeSFNT lays out the tables (sorted by tag, 4-byte aligned) with a fresh directory and
// fixes head.checkSumAdjustment.
func writeSFNT(version uint32, tables map[string][]byte) []byte {
	tags := make([]string, 0, len(tables))
	for k := range tables {
		tags = append(tags, k)
	}
	sort.Strings(tags)
	n := len(tags)
	es := 0
	for 1<<(es+1) <= n {
		es++
	}
	var out bytes.Buffer
	hdr := make([]byte, 12+16*n)
	binary.BigEndian.PutUint32(hdr, version)
	binary.BigEndian.PutUint16(hdr[4:], uint16(n))
	binary.BigEndian.PutUint16(hdr[6:], uint16(16<<es))
	binary.BigEndian.PutUint16(hdr[8:], uint16(es))
	binary.BigEndian.PutUint16(hdr[10:], uint16(16*n-16<<es))
	out.Write(hdr)
	headOff := -1
	for i, tag := range tags {
		data := tables[tag]
		if tag == "head" && len(data) >= 12 {
			data = append([]byte(nil), data...)
			binary.BigEndian.PutUint32(data[8:], 0)
			headOff = out.Len()
		}
		r := hdr[12+16*i:]
		copy(r, tag)
		binary.BigEndian.PutUint32(r[4:], checksum(data))
		binary.BigEndian.PutUint32(r[8:], uint32(out.Len()))
		binary.BigEndian.PutUint32(r[12:], uint32(len(data)))
		out.Write(data)
		for out.Len()%4 != 0 {
			out.WriteByte(0)
		}
	}
	b := out.Bytes()
	copy(b, hdr)
	if headOff >= 0 {
		binary.BigEndian.PutUint32(b[headOff+8:], 0xB1B0AFBA-checksum(b))
	}
	return b
}

type nameRec struct {
	platform, encoding, lang, nameID uint16
	data                             []byte
}

func parseNames(t []byte) ([]nameRec, error) {
	if len(t) < 6 {
		return nil, errors.New("short name table")
	}
	count, store := int(binary.BigEndian.Uint16(t[2:])), int(binary.BigEndian.Uint16(t[4:]))
	if len(t) < 6+12*count {
		return nil, errors.New("short name records")
	}
	var recs []nameRec
	for i := 0; i < count; i++ {
		r := t[6+12*i:]
		ln, off := int(binary.BigEndian.Uint16(r[8:])), int(binary.BigEndian.Uint16(r[10:]))
		if store+off+ln > len(t) {
			return nil, errors.New("name string out of range")
		}
		recs = append(recs, nameRec{binary.BigEndian.Uint16(r), binary.BigEndian.Uint16(r[2:]),
			binary.BigEndian.Uint16(r[4:]), binary.BigEndian.Uint16(r[6:]), t[store+off : store+off+ln]})
	}
	return recs, nil
}

func buildNames(recs []nameRec) []byte {
	sort.Slice(recs, func(i, j int) bool {
		a, b := recs[i], recs[j]
		if a.platform != b.platform {
			return a.platform < b.platform
		}
		if a.encoding != b.encoding {
			return a.encoding < b.encoding
		}
		if a.lang != b.lang {
			return a.lang < b.lang
		}
		return a.nameID < b.nameID
	})
	hdr := make([]byte, 6+12*len(recs))
	binary.BigEndian.PutUint16(hdr[2:], uint16(len(recs)))
	binary.BigEndian.PutUint16(hdr[4:], uint16(len(hdr)))
	var store []byte
	for i, r := range recs {
		p := hdr[6+12*i:]
		binary.BigEndian.PutUint16(p, r.platform)
		binary.BigEndian.PutUint16(p[2:], r.encoding)
		binary.BigEndian.PutUint16(p[4:], r.lang)
		binary.BigEndian.PutUint16(p[6:], r.nameID)
		binary.BigEndian.PutUint16(p[8:], uint16(len(r.data)))
		binary.BigEndian.PutUint16(p[10:], uint16(len(store)))
		store = append(store, r.data...)
	}
	return append(hdr, store...)
}

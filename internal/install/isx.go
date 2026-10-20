package install

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/encoding/japanese"
)

// ErrNotInstallShield means the exe carries no InstallShield 12+ payload in its overlay.
var ErrNotInstallShield = errors.New("no InstallShield payload")

// InstallShield setup.exe (version 12 and later) appends its files after the PE image
// (the "ISSetupStream" form of 2010+ is handled too, see ExtractInstallShield):
//
//	header: "InstallShield\0" (14) + count (2) + type (4) + reserved (8+2+16)
//	per file: name (260, ANSI) + flags (4) + reserved (4) + size (4) + reserved (40) + data
//
// Files with flags&2 are obfuscated with a key derived from their name:
// b' = ^(key[i] ^ swapNibbles(b)), key = name ^ {13 35 86 07}; with flags&4 the key
// position restarts every 1024 bytes. Some bootstrappers stall under Wine while unpacking
// the .msi to %TEMP%, so we unpack without running them.
const (
	isHeaderLen = 14 + 2 + 4 + 8 + 2 + 16
	isAttrLen   = 260 + 4 + 4 + 4 + 40
)

// ExtractInstallShield writes the embedded files of exe into outDir and returns their paths.
func ExtractInstallShield(exe, outDir string) ([]string, error) {
	f, err := os.Open(exe)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	off, err := peOverlay(f)
	if err != nil {
		return nil, ErrNotInstallShield
	}
	hdr := make([]byte, isHeaderLen)
	if _, err := f.ReadAt(hdr, off); err != nil {
		return nil, ErrNotInstallShield
	}
	// "InstallShield": attributes of 312 bytes with an ANSI name.
	// "ISSetupStream" (InstallShield 2010+): 48 bytes + a UTF-16 name; the key is the UTF-8 name
	// and the decrypted data is zlib-compressed.
	stream := bytes.HasPrefix(hdr, []byte("ISSetupStream\x00"))
	if !stream && !bytes.HasPrefix(hdr, []byte("InstallShield\x00")) {
		return nil, ErrNotInstallShield
	}
	n := int(binary.LittleEndian.Uint16(hdr[14:]))
	pos := off + isHeaderLen
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	var out []string
	dec := japanese.ShiftJIS.NewDecoder()
	for i := 0; i < n; i++ {
		var raw []byte
		var name string
		var flags uint32
		var size int64
		if stream {
			attr := make([]byte, 48)
			if _, err := f.ReadAt(attr, pos); err != nil {
				return out, err
			}
			nlen := int64(binary.LittleEndian.Uint32(attr[0:]))
			flags = binary.LittleEndian.Uint32(attr[4:])
			size = int64(binary.LittleEndian.Uint32(attr[10:]))
			wname := make([]byte, nlen)
			if _, err := f.ReadAt(wname, pos+48); err != nil {
				return out, err
			}
			u := make([]uint16, nlen/2)
			for j := range u {
				u[j] = binary.LittleEndian.Uint16(wname[2*j:])
			}
			name = string(utf16.Decode(u))
			raw = []byte(name)
			pos += 48 + nlen
		} else {
			attr := make([]byte, isAttrLen)
			if _, err := f.ReadAt(attr, pos); err != nil {
				return out, err
			}
			raw = attr[:260]
			if j := bytes.IndexByte(raw, 0); j >= 0 {
				raw = raw[:j]
			}
			flags = binary.LittleEndian.Uint32(attr[260:])
			size = int64(binary.LittleEndian.Uint32(attr[268:]))
			pos += isAttrLen
			if name, err = dec.String(string(raw)); err != nil {
				name = string(raw)
			}
		}
		name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
		if name == "." || name == "/" || name == "" {
			return out, errors.New("bad file name in InstallShield payload")
		}
		dst := filepath.Join(outDir, name)
		if err := writeISFile(dst, io.NewSectionReader(f, pos, size), raw, flags, stream); err != nil {
			return out, err
		}
		pos += size
		out = append(out, dst)
	}
	return out, nil
}

// isDecoder undoes the obfuscation: b' = ^(key[i] ^ swapNibbles(b)); with flags&4 the key
// position restarts every 1024 bytes.
type isDecoder struct {
	r     io.Reader
	key   []byte
	flags uint32
	n     int64 // bytes decoded so far
}

func (d *isDecoder) Read(p []byte) (int, error) {
	n, err := d.r.Read(p)
	if d.flags&2 != 0 && len(d.key) > 0 {
		for i := 0; i < n; i++ {
			k := d.n
			if d.flags&4 != 0 {
				k %= 1024
			}
			b := p[i]
			p[i] = ^(d.key[k%int64(len(d.key))] ^ (b<<4 | b>>4))
			d.n++
		}
	}
	return n, err
}

func writeISFile(dst string, r io.Reader, name []byte, flags uint32, stream bool) error {
	key := make([]byte, len(name))
	for i, c := range name {
		key[i] = c ^ []byte{0x13, 0x35, 0x86, 0x07}[i%4]
	}
	var src io.Reader = bufio.NewReaderSize(&isDecoder{r: r, key: key, flags: flags}, 1<<20)
	if stream {
		br := src.(*bufio.Reader)
		if head, _ := br.Peek(2); len(head) == 2 && head[0] == 0x78 && head[1] == 0x9c {
			zr, err := zlib.NewReader(br)
			if err != nil {
				return err
			}
			defer zr.Close()
			src = zr
		}
	}
	w, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, src); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// peOverlay returns the file offset right after the last PE section.
func peOverlay(f io.ReaderAt) (int64, error) {
	var b [4]byte
	if _, err := f.ReadAt(b[:2], 0); err != nil || string(b[:2]) != "MZ" {
		return 0, errors.New("not a PE file")
	}
	if _, err := f.ReadAt(b[:4], 0x3c); err != nil {
		return 0, err
	}
	pe := int64(binary.LittleEndian.Uint32(b[:]))
	h := make([]byte, 24)
	if _, err := f.ReadAt(h, pe); err != nil || string(h[:4]) != "PE\x00\x00" {
		return 0, errors.New("not a PE file")
	}
	nsec := int64(binary.LittleEndian.Uint16(h[6:]))
	optSize := int64(binary.LittleEndian.Uint16(h[20:]))
	var end int64
	sec := make([]byte, 40)
	for i := int64(0); i < nsec; i++ {
		if _, err := f.ReadAt(sec, pe+24+optSize+40*i); err != nil {
			return 0, err
		}
		if e := int64(binary.LittleEndian.Uint32(sec[20:])) + int64(binary.LittleEndian.Uint32(sec[16:])); e > end {
			end = e
		}
	}
	return end, nil
}

package install

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/encoding/japanese"
)

// ErrNotInstallShield means the exe carries no InstallShield 12+ payload in its overlay.
var ErrNotInstallShield = errors.New("no InstallShield payload")

// InstallShield setup.exe (version 12 and later) appends its files after the PE image:
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
	if _, err := f.ReadAt(hdr, off); err != nil || !bytes.HasPrefix(hdr, []byte("InstallShield\x00")) {
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
		attr := make([]byte, isAttrLen)
		if _, err := f.ReadAt(attr, pos); err != nil {
			return out, err
		}
		raw := attr[:260]
		if j := bytes.IndexByte(raw, 0); j >= 0 {
			raw = raw[:j]
		}
		flags := binary.LittleEndian.Uint32(attr[260:])
		size := int64(binary.LittleEndian.Uint32(attr[268:]))
		pos += isAttrLen
		name, err := dec.String(string(raw))
		if err != nil {
			name = string(raw)
		}
		name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
		if name == "." || name == "/" || name == "" {
			return out, errors.New("bad file name in InstallShield payload")
		}
		dst := filepath.Join(outDir, name)
		if err := writeISFile(dst, io.NewSectionReader(f, pos, size), raw, flags); err != nil {
			return out, err
		}
		pos += size
		out = append(out, dst)
	}
	return out, nil
}

func writeISFile(dst string, r io.Reader, name []byte, flags uint32) error {
	w, err := os.Create(dst)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(w, 1<<20)
	key := make([]byte, len(name))
	for i, c := range name {
		key[i] = c ^ []byte{0x13, 0x35, 0x86, 0x07}[i%4]
	}
	buf := make([]byte, 1024)
	var k int
	for {
		n, rerr := io.ReadFull(r, buf)
		if flags&2 != 0 && len(key) > 0 {
			if flags&4 != 0 {
				k = 0
			}
			for i := 0; i < n; i++ {
				b := buf[i]
				buf[i] = ^(key[k%len(key)] ^ (b<<4 | b>>4))
				k++
			}
		}
		bw.Write(buf[:n])
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			w.Close()
			return rerr
		}
	}
	if err := bw.Flush(); err != nil {
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

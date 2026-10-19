// Package regdiff reads, diffs and merges Wine's text registry files
// (system.reg / user.reg) without running Wine.
package regdiff

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Key is one registry key: its values in file order plus Wine's "#..." meta lines.
type Key struct {
	Name   string
	Meta   []string
	Names  []string          // value names in order (`@` or `"quoted"`)
	Values map[string]string // name -> raw Wine value text (e.g. `dword:00000001`)
}

// Hive is an ordered set of keys.
type Hive struct {
	Header []string // lines before the first key, kept verbatim on Apply
	Order  []string
	Keys   map[string]*Key
}

func newHive() *Hive { return &Hive{Keys: map[string]*Key{}} }

func (h *Hive) key(name string) *Key {
	if k, ok := h.Keys[name]; ok {
		return k
	}
	k := &Key{Name: name, Values: map[string]string{}}
	h.Keys[name] = k
	h.Order = append(h.Order, name)
	return k
}

func (k *Key) set(name, val string) {
	if _, ok := k.Values[name]; !ok {
		k.Names = append(k.Names, name)
	}
	k.Values[name] = val
}

var (
	keyRe = regexp.MustCompile(`^\[(.*)\](?: \d+)?$`)
	valRe = regexp.MustCompile(`(?s)^(@|"(?:[^"\\]|\\.)*")=(.*)$`)
)

// Parse reads a Wine registry file.
func Parse(path string) (*Hive, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	h := newHive()
	lines := strings.Split(string(data), "\n")
	var cur *Key
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		if m := keyRe.FindStringSubmatch(ln); m != nil {
			cur = h.key(m[1])
			continue
		}
		if cur == nil {
			if ln != "" || len(h.Header) > 0 {
				h.Header = append(h.Header, ln)
			}
			continue
		}
		switch {
		case strings.HasPrefix(ln, "#"):
			cur.Meta = append(cur.Meta, ln)
		case ln == "" || strings.HasPrefix(ln, ";"):
		default:
			for strings.HasSuffix(ln, `\`) && i+1 < len(lines) { // hex: values wrap with a trailing backslash
				i++
				ln += "\n" + lines[i]
			}
			if m := valRe.FindStringSubmatch(ln); m != nil {
				cur.set(m[1], m[2])
			}
		}
	}
	return h, nil
}

// noise lists keys that change on every install or belong to Wine itself.
var noise = compile(
	`^Software\\\\Wine\\\\(MSHTML|Mono|Debug)`, `\\\\Explorer\\\\(RecentDocs|UserAssist|ComDlg32|MountPoints2)`,
	`^System\\\\CurrentControlSet\\\\(Services\\\\Eventlog|Control\\\\DeviceClasses|Enum)`,
	`^Software\\\\Microsoft\\\\Windows\\\\CurrentVersion\\\\(Explorer\\\\Shell Folders|RunOnce)$`,
	`^Hardware\\\\`, `^Software\\\\Microsoft\\\\Cryptography\\\\RNG`, `\\\\Fonts$`,
	`^Volatile Environment`, `^Software\\\\Wine\\\\Fonts\\\\Cache`,
	`^Console$`, `\\\\TimeZoneInformation$`, `^Software\\\\Microsoft\\\\ActiveMovie\\\\devenum`, `\\\\MMDevices\\\\`,
	`^System\\\\ControlSet\d+\\\\Enum\\\\`, `^Software\\\\Wine\\\\(Drivers|MenuFiles|FileOpenAssociations)`,
	`^Software\\\\Microsoft\\\\(DirectX|Direct3D|DirectInput|DirectPlay|DirectDraw)\\\\Most ?Recent`,
	`^Software\\\\Microsoft\\\\Windows\\\\CurrentVersion\\\\Explorer\\\\(FileExts|Discardable)`,
)

func compile(ps ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(ps))
	for i, p := range ps {
		out[i] = regexp.MustCompile("(?i)" + p)
	}
	return out
}

func noisy(k string) bool {
	for _, r := range noise {
		if r.MatchString(k) {
			return true
		}
	}
	return false
}

// Diff returns the keys and values that are new or changed in b compared to a.
// New keys are kept even when empty, so that their existence is reproduced.
func Diff(a, b *Hive) *Hive {
	out := newHive()
	for _, name := range b.Order {
		if noisy(name) {
			continue
		}
		bk := b.Keys[name]
		ak, existed := a.Keys[name]
		var changed []string
		for _, n := range bk.Names {
			if !existed {
				changed = append(changed, n)
				continue
			}
			if old, ok := ak.Values[n]; !ok || old != bk.Values[n] {
				changed = append(changed, n)
			}
		}
		if !existed || len(changed) > 0 {
			k := out.key(name)
			for _, n := range changed {
				k.set(n, bk.Values[n])
			}
		}
	}
	return out
}

// Count returns the number of keys and values.
func (h *Hive) Count() (keys, values int) {
	for _, k := range h.Keys {
		values += len(k.Names)
	}
	return len(h.Keys), values
}

// WriteFragment writes h as a standalone fragment file.
func (h *Hive) WriteFragment(path string) error {
	var b strings.Builder
	b.WriteString("WINE REGISTRY Version 2\n;; vnbox registry fragment\n")
	h.writeKeys(&b, false)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func (h *Hive) writeKeys(b *strings.Builder, meta bool) {
	for _, name := range h.Order {
		k := h.Keys[name]
		fmt.Fprintf(b, "\n[%s] 0\n", name)
		if meta {
			for _, m := range k.Meta {
				b.WriteString(m + "\n")
			}
		}
		for _, n := range k.Names {
			fmt.Fprintf(b, "%s=%s\n", n, k.Values[n])
		}
	}
}

// Apply merges the fragment file into the target registry file in place.
// wineserver must not be running for the target prefix.
func Apply(fragPath, targetPath string) error {
	frag, err := Parse(fragPath)
	if err != nil {
		return err
	}
	t, err := Parse(targetPath)
	if err != nil {
		return err
	}
	for _, name := range frag.Order {
		dst := t.key(name)
		for _, n := range frag.Keys[name].Names {
			dst.set(n, frag.Keys[name].Values[n])
		}
	}
	var b strings.Builder
	for _, l := range t.Header {
		b.WriteString(l + "\n")
	}
	t.writeKeys(&b, true)
	tmp := targetPath + ".vnbox-tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, targetPath)
}

var escRe = regexp.MustCompile(`\\x([0-9a-fA-F]{1,4})`)

// Unescape turns Wine's \xNNNN escapes into characters for display.
func Unescape(s string) string {
	return escRe.ReplaceAllStringFunc(s, func(m string) string {
		v, _ := strconv.ParseUint(m[2:], 16, 32)
		return string(rune(v))
	})
}

// Show prints a fragment in a human-readable form.
func Show(path string, w *bufio.Writer) error {
	h, err := Parse(path)
	if err != nil {
		return err
	}
	for _, name := range h.Order {
		fmt.Fprintf(w, "[%s]\n", Unescape(name))
		k := h.Keys[name]
		for _, n := range k.Names {
			v := Unescape(k.Values[n])
			if len(v) > 120 {
				v = v[:120]
			}
			fmt.Fprintf(w, "   %s = %s\n", Unescape(n), v)
		}
	}
	return w.Flush()
}

// Package detect guesses the engine, installer type and runtime needs of a game
// from file names and byte signatures, and picks the executable to launch.
package detect

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

type fileSig struct {
	name string
	re   *regexp.Regexp
}

func fs(name, re string) fileSig { return fileSig{name, regexp.MustCompile(re)} }

var fileSigs = []fileSig{
	fs("NScripter", `^(nscript\.dat|nscr_sec\.dat|0?0\.txt|arc\d*\.nsa|\d+\.ns2|.*\.nsa)$`),
	fs("KiriKiri", `^(data\.xp3|.*\.xp3|krkr\.exe|tvpwin32\.exe)$`),
	fs("YU-RIS", `^(ysbin\.ypf|.*\.ypf|yu-ris\.exe|yscfg\.dat)$`),
	fs("SiglusEngine", `^(scene\.pck|gameexe\.dat|siglusengine.*\.exe)$`),
	fs("RealLive", `^(seen\.txt|reallive\.exe|gameexe\.ini)$`),
	fs("BGI/Ethornell", `^(bgi\.exe|bgi\.gdb|sysgrp\.arc|sysprg\.arc)$`),
	fs("CatSystem2", `^(cs2\.exe|cs2conf\.dll|.*\.int)$`),
	fs("Artemis", `^(.*\.pfs|.*\.pfs\.\d+)$`),
	fs("LiveMaker", `^(.*\.lsb|game\.dat|.*\.lpb|livemaker.*)$`),
	fs("Unity", `^(unityplayer\.dll|globalgamemanagers|.*_data)$`),
	fs("RenPy", `^(.*\.rpa|.*\.rpyc|renpy)$`),
	fs("NW.js/Tyrano", `^(nw\.dll|package\.nw|nw\.pak|tyrano.*)$`),
	fs("RPGMaker", `^(game\.rgss\w*|rgss\d+.*\.dll|rpg_core\.js|rmmz_core\.js)$`),
	fs("WolfRPG", `^(data\.wolf|game\.wolf)$`),
	fs("Majiro", `^(scenario\.arc|majiro.*)$`),
	fs("Softpal", `^(.*\.pac|pal\.dll)$`),
	fs("PONScripter/ONScripter", `^(pscript\.dat|pns\.cfg|ponscr.*|onscripter.*)$`),
	fs("AdvHD", `^(advhd\.exe|.*\.ws2)$`),
	fs("QLIE", `^(.*\.pack|qlie.*)$`),
	fs("Musica", `^(.*\.paz)$`),
	fs("ShiinaRio", `^(.*\.war)$`), fs("EntisGLS", `^(.*\.noa|.*\.nos)$`), fs("Nitroplus", `^(.*\.npa)$`),
	fs("Eushully AGE", `^(age\.exe|agerc\.dll|sys4ini\.bin|.*\.alf)$`),
	fs("AliceSoft System4", `^(.*\.ain|.*\.ald|.*\.afa|system40\.exe)$`), fs("FVP", `^(.*\.hcb)$`),
	fs("AdvWin32/WillPlus", `^(rio\.arc|advwin32\.exe)$`), fs("NeXAS", `^(.*\.pac)$`),
	fs("Runtime:SharpDX11", `^sharpdx\.direct3d11\.dll$`),
	fs("Runtime:Electron", `^(app\.asar|electron\.asar|default_app\.asar)$`),
	fs("Installer:InstallShield", `^(data1\.cab|data1\.hdr|.*\.hdr|ikernel\.ex_|setup\.inx|_setup\.dll)$`),
	fs("Installer:MSI", `^.*\.msi$`),
}

type binSig struct {
	name string
	sig  []byte
}

func bs(name, sig string) binSig { return binSig{name, []byte(sig)} }

// utf16 is the UTF-16LE form of an ASCII string (for wide string literals in executables).
func utf16(s string) string {
	b := make([]byte, 0, 2*len(s))
	for i := 0; i < len(s); i++ {
		b = append(b, s[i], 0)
	}
	return string(b)
}

var binSigs = []binSig{
	bs("KiriKiri", "TVP(KIRIKIRI)"), bs("KiriKiri", "XP3\r\n \n\x1a"),
	bs("NScripter", "NScripter"), bs("YU-RIS", "YU-RIS"), bs("SiglusEngine", "SiglusEngine"),
	bs("RealLive", "RealLive"), bs("BGI/Ethornell", "BURIKO ARC20"), bs("BGI/Ethornell", "PackFile    "),
	bs("CatSystem2", "CatSystem2"), bs("LiveMaker", "LiveMaker"), bs("LiveMaker", "vf\x00\x00\x00"),
	bs("Unity", "UnityPlayer"), bs("Artemis", "Artemis"), bs("PONScripter/ONScripter", "ONScripter"),
	bs("AdvHD", "AdvHD"), bs("QLIE", "QLIE"), bs("NeXAS", "NeXAS"), bs("ShiinaRio", "ShiinaRio"),
	bs("EntisGLS", "EntisGLS"), bs("Escude", "ESC-ARC"), bs("Malie", "MalieSystem"), bs("Malie", "malie"),
	bs("Silky's AI6WIN", "AI6WIN"), bs("Silky's AI5WIN", "AI5WIN"), bs("AliceSoft System4", "System40"),
	bs("CIRCUS", "CIRCUS"), bs("Kaguya", "KaGuYa"), bs("Lilim", "LILIM"), bs("RPGMaker", "RGSS"),
	bs("Majiro", "MajiroArcV"), bs("CatSystem2", "CS2CONF"),
	bs("Installer:InnoSetup", "Inno Setup Setup Data"), bs("Installer:NSIS", "Nullsoft Install System"),
	bs("Installer:NSIS", "NullsoftInst"), bs("Installer:InstallShield", "InstallShield"),
	bs("Installer:WiseInstaller", "Wise Installation"), bs("SFX:7z", "7z\xbc\xaf\x27\x1c"),
	bs("SFX:RAR", "Rar!\x1a\x07"), bs("SFX:WinRAR", "WinRAR SFX"), bs("SFX:CAB", "MSCF\x00\x00\x00\x00"),
	bs("SFX:LZH", "-lh5-"), bs("SFX:ZIP", "PK\x03\x04"),
	bs("DRM:SoftDenchi", "SoftDenchi"), bs("DRM:DMM", "DMMGamePlayer"), bs("DRM:Alpha-ROM", "ARCHIVE_ROM"),
	bs("DRM:StarForce", "StarForce"), bs("DRM:SecuROM", "SecuROM"), bs("DRM:PlayDRM", "PlayDRM"),
	bs("DRM:DMM", "dmmgameplayer://"), bs("DRM:SoftDenchi", "soft-denchi"),
	// protectors: PE section names in the header
	bs("Protect:Themida", ".themida"), bs("Protect:WinLicense", ".winlice"), bs("Protect:VMProtect", ".vmp0\x00"),
	bs("Protect:Enigma", ".enigma1"), bs("Protect:SafeDisc", "stxt774"), bs("Protect:StarForce", ".sforce"),
	bs("Protect:SecuROM", ".securom"),
	bs("Runtime:DirectShow", "quartz.dll"), bs("Runtime:VC71", "MSVCR71.dll"),
	bs("Runtime:VC80", "MSVCR80.dll"), bs("Runtime:VC90", "MSVCR90.dll"), bs("Runtime:VC100", "MSVCR100.dll"),
	bs("Runtime:XamlIslands", utf16("Windows.UI.Xaml.Hosting.DesktopWindowXamlSource")),
	bs("Runtime:VCRuntimeCheck", utf16(`SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes`)),
	bs("Runtime:MFC42", "MFC42.DLL"), bs("Runtime:VB6", "MSVBVM60.DLL"), bs("Runtime:D3DX9", "d3dx9_"),
	bs("Runtime:DotNet", "_CorExeMain"), bs("Runtime:WMP", "wmvcore.dll"), bs("Runtime:Flash", "Flash.ocx"),
}

// Exe describes one executable found in the game folder.
type Exe struct {
	Path   string   `json:"exe"` // relative to the scanned folder
	SizeMB float64  `json:"size_mb"`
	Arch   string   `json:"arch"`
	Hits   []string `json:"hits"`
}

// Result is the outcome of Detect.
type Result struct {
	Path   string         `json:"path"`
	Engine string         `json:"engine"`
	Scores map[string]int `json:"engine_scores"`
	// FileEngine is the engine guessed from data file names only (no executable signatures).
	// A launcher or installer exe can mention an engine without the game being present.
	FileEngine string   `json:"file_engine"`
	Tags       []string `json:"tags"` // Installer:*, SFX:*, DRM:*, Runtime:*
	Exes       []Exe    `json:"exes"`
}

// ScanBinary returns the signatures found in the first 64 MB and last 8 MB of a file.
func ScanBinary(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, _ := f.Stat()
	head := make([]byte, 64<<20)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	var tail []byte
	if st.Size() > int64(n) {
		off := st.Size() - 8<<20
		if off < int64(n) {
			off = int64(n)
		}
		tail = make([]byte, st.Size()-off)
		m, _ := f.ReadAt(tail, off)
		tail = tail[:m]
	}
	hits := map[string]bool{}
	for _, buf := range [][]byte{head, tail} {
		var low []byte
		for _, s := range binSigs {
			if bytes.Contains(buf, s.sig) {
				hits[s.name] = true
			} else if strings.HasPrefix(s.name, "Runtime:") {
				if low == nil {
					low = bytes.ToLower(buf)
				}
				if bytes.Contains(low, bytes.ToLower(s.sig)) {
					hits[s.name] = true
				}
			}
		}
	}
	return sortedKeys(hits)
}

// PEArch returns "x86", "x64", "arm64" or "" for non-PE files.
func PEArch(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	d := make([]byte, 4096)
	n, _ := io.ReadFull(f, d)
	d = d[:n]
	if n < 0x40 || string(d[:2]) != "MZ" {
		return ""
	}
	off := int(binary.LittleEndian.Uint32(d[0x3c:0x40]))
	if off+6 > n || string(d[off:off+4]) != "PE\x00\x00" {
		return "x86" // NE/MZ: treat as 32-bit
	}
	switch binary.LittleEndian.Uint16(d[off+4 : off+6]) {
	case 0x14c:
		return "x86"
	case 0x8664:
		return "x64"
	case 0xaa64:
		return "arm64"
	}
	return "x86"
}

// Detect scans a folder (up to 3 levels deep) or a single file.
func Detect(root string) Result { return DetectSkip(root, "") }

// DetectSkip is Detect ignoring the files directly inside skip (its subfolders are still scanned).
func DetectSkip(root, skip string) Result {
	res := Result{Path: root, Scores: map[string]int{}}
	fileScores := map[string]int{}
	var exes, dlls []string
	st, err := os.Stat(root)
	if err != nil {
		res.Engine = "unknown"
		return res
	}
	if !st.IsDir() {
		exes = []string{root}
	} else {
		filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || p == root {
				return nil
			}
			if skip != "" && !d.IsDir() && filepath.Dir(p) == skip {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			if strings.Count(rel, string(filepath.Separator)) > 3 {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			n := strings.ToLower(d.Name())
			for _, s := range fileSigs {
				if s.re.MatchString(n) {
					res.Scores[s.name]++
					if !strings.Contains(s.name, ":") {
						fileScores[s.name]++
					}
				}
			}
			if !d.IsDir() && strings.HasSuffix(n, ".exe") {
				exes = append(exes, p)
			}
			if !d.IsDir() && strings.HasSuffix(n, ".dll") && len(dlls) < 40 {
				dlls = append(dlls, p)
			}
			return nil
		})
	}
	size := func(p string) int64 {
		if st, err := os.Stat(p); err == nil {
			return st.Size()
		}
		return 0
	}
	sort.Slice(exes, func(i, j int) bool { return size(exes[i]) > size(exes[j]) })
	if len(exes) > 8 {
		exes = exes[:8]
	}
	for _, e := range exes {
		hits := ScanBinary(e)
		for _, h := range hits {
			res.Scores[h] += 3
		}
		rel := e
		if st.IsDir() {
			rel, _ = filepath.Rel(root, e)
		}
		res.Exes = append(res.Exes, Exe{Path: rel, SizeMB: float64(size(e)*10>>20) / 10, Arch: PEArch(e), Hits: hits})
	}
	// DRM runtimes and protectors often live in DLLs next to the game
	for _, d := range dlls {
		if size(d) > 32<<20 {
			continue
		}
		for _, h := range ScanBinary(d) {
			if strings.HasPrefix(h, "DRM:") || strings.HasPrefix(h, "Protect:") {
				res.Scores[h]++
			}
		}
	}
	best, bestScore := "unknown", 0
	for k, v := range res.Scores {
		if strings.Contains(k, ":") {
			res.Tags = append(res.Tags, k)
			continue
		}
		if v > bestScore || (v == bestScore && k < best) {
			best, bestScore = k, v
		}
	}
	for k := range res.Scores {
		if strings.Contains(k, ":") {
			delete(res.Scores, k)
		}
	}
	sort.Strings(res.Tags)
	res.Engine = best
	res.FileEngine, bestScore = "unknown", 0
	for k, v := range fileScores {
		if v > bestScore || (v == bestScore && k < res.FileEngine) {
			res.FileEngine, bestScore = k, v
		}
	}
	// An Electron app's exe is a 100+ MB Chromium build in which short engine signatures turn up
	// by chance; without engine data files, the engine is the Electron app itself.
	if res.FileEngine == "unknown" && slices.Contains(res.Tags, "Runtime:Electron") {
		res.Engine = "Electron"
	}
	return res
}

var notGame = regexp.MustCompile(`(?i)unins|setup|install|config|設定|launcher|crash|vc_?redist|dxsetup|directx|updater|readme|manual|support|インストール|getpin|pin\.exe|regist|認証|savefolder|セーブフォルダ|(^|/)start\.exe$`)

// launchers that must be started instead of the game exe (they write the launch settings)
var launcher = regexp.MustCompile(`(?i)(^|/)startup\.exe$`)

// MainExe picks the executable to launch: a known launcher if present, otherwise the
// largest exe that does not look like a tool.
func (r Result) MainExe() string {
	for _, e := range r.Exes {
		if launcher.MatchString(filepathToSlash(e.Path)) {
			return e.Path
		}
	}
	for _, e := range r.Exes {
		if !notGame.MatchString(e.Path) {
			return e.Path
		}
	}
	if len(r.Exes) > 0 {
		return r.Exes[0].Path
	}
	return ""
}

// HasTag reports whether any exe or file carries the tag prefix (e.g. "Installer:").
func (r Result) HasTag(prefix string) bool {
	for _, t := range r.Tags {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func filepathToSlash(p string) string { return strings.ReplaceAll(p, "\\", "/") }

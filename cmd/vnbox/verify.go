package main

// Dynamic checks: vnbox mkbase / probe / verify / ledger. A check starts the game through the
// same session code as `vnbox play`, so what passes here is what players get.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"vnbox/internal/base"
	"vnbox/internal/extract"
	"vnbox/internal/pkg"
	"vnbox/internal/probe"
	"vnbox/internal/wine"
)

func cmdMkbase(ctx context.Context, args []string) error {
	c := newFlags("mkbase")
	w64 := c.fs.Bool("win64", false, "classic Wine: 64-bit Windows 10 prefix (default 32-bit Windows 7). The vnbox Wine is always 64-bit")
	pos := c.parse(args)
	fl := c.Flavor()
	dir := fl.BasePrefix(*w64)
	if len(pos) > 0 {
		dir, _ = filepath.Abs(pos[0])
	}
	return base.Make(ctx, fl, *w64, dir, os.Stdout)
}

func cmdProbe(ctx context.Context, args []string) error {
	c := newFlags("probe")
	prefix := c.fs.String("prefix", "", "prefix to run in (default prefixes/run-<name>; prepared when new)")
	run := c.fs.String("run", "", "results/<run>/ for screenshots and logs (default <name>)")
	input := c.fs.String("input", "", "operations: click:x,y key:K sleep:N shot:NAME pass:N (default: click + Enter six times)")
	wait := c.fs.Duration("wait", 20*time.Second, "time before the first screenshot")
	pos := c.parse(args)
	if len(pos) < 1 {
		return errors.New("usage: vnbox probe <name> [-input ops] [-wait 30s] [-prefix dir] [-run name]")
	}
	r, err := probe.Run(ctx, pos[0], probe.Options{Flavor: c.Flavor(), Prefix: *prefix, Run: *run,
		Input: *input, Wait: *wait, Log: os.Stderr})
	if err != nil {
		return err
	}
	fmt.Printf("%s  %s\n", r.Verdict, r.Dir)
	return nil
}

// verifyOptions are the settings of one vnbox verify run.
type verifyOptions struct {
	out    string // result TSV
	input  string
	wait   time.Duration
	verbs  []string // added to winetricks= of every package
	clicks [][2]int // clicks on the installer's first screen
	keep   bool     // keep packages after the check
}

// cmdVerify checks a list of distributions: download → vnbox add → probe with the vnbox Wine,
// and with the classic Wine when that fails → contact sheet → one TSV line per title. The
// screenshots of every title are then reviewed by eye (contact sheets in results/batch/) and
// recorded with vnbox ledger record.
func cmdVerify(ctx context.Context, args []string) error {
	c := newFlags("verify")
	jobs := c.fs.Int("j", 3, "titles checked at the same time")
	out := c.fs.String("out", filepath.Join(wine.Root(), "results", "batch", "summary.tsv"), "result TSV (titles already in it are skipped)")
	input := c.fs.String("input", "shot:t40 pass:6 sleep:12 shot:t1 sleep:12 shot:t2 click:640,400 sleep:5 key:Return sleep:8 shot:t3 click:640,400 sleep:5 key:Return sleep:8 shot:c", "probe operations")
	wait := c.fs.Duration("wait", 45*time.Second, "time before the first screenshot")
	vb := c.fs.String("verbs", "", "verbs added to every package (vnbox-dxvk ...)")
	click := c.fs.String("click", "", "clicks on the installer's first screen (x,y;x,y)")
	keep := c.fs.Bool("keep", false, "keep the packages")
	pos := c.parse(args)
	if len(pos) < 1 {
		return errors.New("usage: vnbox verify <list.tsv> [-j 3] [-out results/batch/summary.tsv]\n" +
			"  list columns: id, url, title, source, retail DRM, MB, and optionally per title: operations,\n" +
			"  verbs (space separated), installer clicks (x,y;x,y), start exe, start arguments, wait seconds")
	}
	o := verifyOptions{out: *out, input: *input, wait: *wait, verbs: strings.Fields(*vb), clicks: parseClicks(*click), keep: *keep}
	rows, err := readTSV(pos[0])
	if err != nil {
		return err
	}
	done := map[string]bool{}
	if prev, err := readTSV(o.out); err == nil {
		for _, r := range prev {
			done[r[0]] = true
		}
	}
	os.MkdirAll(filepath.Dir(o.out), 0o755)
	os.MkdirAll(filepath.Join(wine.Root(), "results", "batch"), 0o755) // logs and contact sheets
	var mu sync.Mutex
	sem := make(chan struct{}, max(1, *jobs))
	var wg sync.WaitGroup
	for _, r := range rows {
		if len(r) < 2 || done[r[0]] || ctx.Err() != nil {
			continue
		}
		done[r[0]] = true // a title listed twice is checked once
		r = append(r, make([]string, len(listColumns))...)[:len(listColumns)]
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			line := verifyOne(ctx, r, o)
			if ctx.Err() != nil {
				return // interrupted: not a result, check it again next time
			}
			mu.Lock()
			defer mu.Unlock()
			f, err := os.OpenFile(o.out, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err == nil {
				fmt.Fprintln(f, strings.Join(line, "\t"))
				f.Close()
			}
			fmt.Println(strings.Join(line, "\t"))
		}()
	}
	wg.Wait()
	return ctx.Err()
}

func parseClicks(s string) [][2]int {
	var clicks [][2]int
	for _, pt := range strings.Split(s, ";") {
		var x, y int
		if _, err := fmt.Sscanf(strings.TrimSpace(pt), "%d,%d", &x, &y); err == nil {
			clicks = append(clicks, [2]int{x, y})
		}
	}
	return clicks
}

// listColumns are the columns of a verify list. The first six come from vnbox candidates; the
// rest are optional settings for titles that need them (they override -input / -wait and add
// to -verbs / -click).
var listColumns = []string{"id", "url", "title", "source", "drm", "mb", "input", "verbs", "clicks", "exe", "args", "wait"}

// verifyOne checks one title and returns its TSV line:
// id, Wine that worked (vnbox / classic), verdict, target, engine, source, DRM (retail), DRM traces.
func verifyOne(ctx context.Context, r []string, o verifyOptions) []string {
	id, url, title, src, drm := r[0], r[1], r[2], r[3], r[4]
	if r[6] != "" {
		o.input = r[6]
	}
	o.verbs = append(append([]string{}, o.verbs...), strings.Fields(r[7])...)
	o.clicks = append(append([][2]int{}, o.clicks...), parseClicks(r[8])...)
	if w, err := time.ParseDuration(r[11] + "s"); err == nil && r[11] != "" {
		o.wait = w
	}
	line := func(best, verdict, target, engine, marks string) []string {
		return []string{id, best, verdict, target, engine, src, drm, marks}
	}
	logf, _ := os.Create(filepath.Join(wine.Root(), "results", "batch", "v_"+id+".log"))
	defer logf.Close()
	waitDisk(ctx, 6<<30)
	dl := filepath.Join(wine.Root(), "downloads", "verify", id)
	defer os.RemoveAll(dl)
	// the url column may list patches after the distribution: they are unpacked over the game folder
	var files []string
	for i, u := range strings.Fields(url) {
		f := filepath.Join(dl, strconv.Itoa(i), filepath.Base(strings.SplitN(u, "?", 2)[0]))
		if err := fetch(ctx, u, f); err != nil {
			fmt.Fprintln(logf, "download:", err)
			return line("-", "DL_FAIL", "", "", "")
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return line("-", "DL_FAIL", "", "", "")
	}
	p, l, err := addPackage(ctx, id, files[0], addOptions{Flavor: wine.Vnbox, Exe: r[9], Title: title, Clicks: o.clicks, Log: logf})
	if err == nil {
		for _, f := range files[1:] {
			if err = applyPatch(f, filepath.Join(p.Dir, "files", filepath.Dir(filepath.FromSlash(l.Target)))); err != nil {
				err = fmt.Errorf("patch %s: %w", filepath.Base(f), err)
				break
			}
		}
	}
	os.RemoveAll(dl)
	if err != nil {
		fmt.Fprintln(logf, "add:", err)
		os.RemoveAll(pkg.Dir(id))
		return line("-", "NG:package("+probe.Short(err.Error())+")", "", "", "")
	}
	if !o.keep {
		defer os.RemoveAll(p.Dir)
	}
	for _, v := range o.verbs {
		if !slices.Contains(l.Winetricks, v) {
			l.Winetricks = append(l.Winetricks, v)
		}
	}
	if r[10] != "" {
		l.Args = r[10]
	}
	if err := pkg.WriteLaunch(p.Dir, l); err != nil {
		fmt.Fprintln(logf, "launch.ini:", err)
		return line("-", "NG:package("+probe.Short(err.Error())+")", l.Target, l.Engine, "")
	}
	marks := drmScan(filepath.Join(p.Dir, "files"))
	verdict, best := "NG", "-"
	var runs []string
	for _, fl := range []wine.Flavor{wine.Vnbox, wine.Classic} {
		run := "v_" + id + "_" + string(fl)
		res, err := probe.Run(ctx, id, probe.Options{Flavor: fl, Run: run, Input: o.input, Wait: o.wait, Log: logf})
		if err != nil {
			fmt.Fprintln(logf, "probe:", err)
			continue
		}
		runs = append(runs, run)
		verdict, best = res.Verdict, string(fl)
		if verdict == "OK" {
			break
		}
	}
	if err := contactSheet(ctx, filepath.Join(wine.Root(), "results", "batch", "v_"+id+".png"), runs); err != nil {
		fmt.Fprintln(logf, "sheet:", err)
	}
	return line(best, verdict, l.Target, l.Engine, marks)
}

// applyPatch unpacks a patch archive over the game folder. Folders that only wrap the patch
// (renran_patch/...) are skipped, so the files land next to the game's own.
func applyPatch(archive, gameDir string) error {
	tmp, err := os.MkdirTemp(filepath.Join(wine.Root(), "downloads"), "patch")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := extract.Extract(archive, tmp); err != nil {
		return err
	}
	root := tmp
	for {
		ents, _ := os.ReadDir(root)
		if len(ents) != 1 || !ents[0].IsDir() {
			break
		}
		if _, err := os.Stat(filepath.Join(gameDir, ents[0].Name())); err == nil {
			break // the game has a folder of that name: the patch mirrors the game's layout
		}
		root = filepath.Join(root, ents[0].Name())
	}
	if out, err := exec.Command("cp", "-a", "--remove-destination", root+"/.", gameDir+"/").CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

// waitDisk waits until the file system has at least min bytes free (titles are checked in
// parallel and a big installer can fill the disk).
func waitDisk(ctx context.Context, min uint64) {
	for ctx.Err() == nil {
		var st syscall.Statfs_t
		if syscall.Statfs(wine.Root(), &st) != nil || st.Bavail*uint64(st.Bsize) >= min {
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(30 * time.Second):
		}
	}
}

// fetch downloads url to path, trying three times.
func fetch(ctx context.Context, url, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var err error
	for i := 0; i < 3; i++ {
		if err = fetchOnce(ctx, url, path); err == nil || ctx.Err() != nil {
			return err
		}
		time.Sleep(time.Duration(i+1) * 5 * time.Second)
	}
	return err
}

func fetchOnce(ctx context.Context, url, path string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

var drmRe = regexp.MustCompile(`(?i)soft-?denchi|sdenchi|paltio|PlayDRM|dlsite ?drm|DMMGamePlayer|dmmgame|Software ?Defender|StarForce|SecuROM|Themida|WinLicense|Enigma ?protector|VMProtect|ProRing|Alpha-?ROM`)

// drmScan lists DRM / protector names found in the executables (the first 60 below 80 MB).
func drmScan(dir string) string {
	found := map[string]bool{}
	n := 0
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || n >= 60 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".exe" && ext != ".dll" {
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() >= 80<<20 {
			return nil
		}
		n++
		if b, err := os.ReadFile(p); err == nil {
			for _, m := range drmRe.FindAll(b, -1) {
				found[strings.ReplaceAll(strings.ToLower(string(m)), " ", "")] = true
			}
		}
		return nil
	})
	marks := make([]string, 0, len(found))
	for m := range found {
		marks = append(marks, m)
	}
	sort.Strings(marks)
	if len(marks) == 0 {
		return ""
	}
	return strings.Join(marks, ",") + ","
}

// contactSheet puts the last two screenshots of each run side by side, labelled with the run
// name, into one image (ImageMagick) for review by eye.
func contactSheet(ctx context.Context, out string, runs []string) error {
	tmp, err := os.MkdirTemp("", "sheet")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	var tiles []string
	for _, run := range runs {
		shots, _ := filepath.Glob(filepath.Join(wine.Root(), "results", run, "[0-9][0-9]_*.png"))
		if len(shots) == 0 {
			continue
		}
		a, b := shots[len(shots)-1], shots[max(0, len(shots)-2)]
		t := filepath.Join(tmp, fmt.Sprintf("%03d.png", len(tiles)))
		cell := func(img string) []string {
			return []string{"(", img, "-resize", "300x170", "-background", "#222", "-gravity", "center", "-extent", "300x170", ")"}
		}
		args := append(cell(b), cell(a)...)
		args = append(args, "+append", "-gravity", "south", "-background", "#000", "-fill", "yellow", "-pointsize", "15",
			"-splice", "0x20", "-annotate", "+0+2", run, t)
		if o, err := exec.CommandContext(ctx, "convert", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("convert: %v: %s", err, o)
		}
		tiles = append(tiles, t)
	}
	if len(tiles) == 0 {
		return errors.New("no screenshots")
	}
	args := append(tiles, "-tile", "2x", "-geometry", "+3+3", "-background", "#111", out)
	if o, err := exec.CommandContext(ctx, "montage", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("montage: %v: %s", err, o)
	}
	return nil
}

func readTSV(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows [][]string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "#") {
			continue
		}
		rows = append(rows, strings.Split(l, "\t"))
	}
	return rows, sc.Err()
}

// cmdStopAll stops every game (the wineserver of each prefix) and every headless X server.
func cmdStopAll() error {
	prefixes, _ := filepath.Glob(filepath.Join(wine.Root(), "prefixes", "*"))
	for _, p := range prefixes {
		for _, fl := range []wine.Flavor{wine.Vnbox, wine.Classic} {
			wine.Env{Flavor: fl, Prefix: p}.Kill()
		}
	}
	exec.Command("pkill", "-x", "Xvfb").Run()
	fmt.Println("stopped")
	return nil
}

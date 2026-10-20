package main

// vnbox ledger: the record of checked titles. results/verified.tsv (id, title, engine, note;
// a note starting with "弱:" is a partial check) and results/excluded.tsv (id, reason), from
// which the tallies and the title list of VERIFICATION.md are generated.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"vnbox/internal/wine"
)

func ledgerPath(name string) string { return filepath.Join(wine.Root(), "results", name) }

func cmdLedger(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: vnbox ledger record [-list L] [-summary S] [-note N] [-title T] [-replace] <id>... | ledger md")
	}
	switch args[0] {
	case "record":
		return ledgerRecord(args[1:])
	case "md":
		return ledgerMD()
	}
	return fmt.Errorf("unknown ledger command %q", args[0])
}

// windowSkip are window titles that are not the game's (launchers, settings, dialogs).
var windowSkip = regexp.MustCompile(`スタートアップ|設定|インストール|Exception|名前の変更|^Information$|確認|Default IME|^$`)

// ledgerRecord appends titles reviewed by eye to verified.tsv. The title is the list's (the
// store's product name) or else the game window's; the engine is vnbox add's; the note says
// how far the check got (default タイトル) plus the classic Wine, the retail DRM and the DRM
// traces when there are any.
func ledgerRecord(args []string) error {
	c := newFlags("ledger record")
	list := c.fs.String("list", filepath.Join(wine.Root(), "downloads", "batch6.tsv"), "candidate list given to vnbox verify")
	summary := c.fs.String("summary", ledgerPath(filepath.Join("batch", "summary6.tsv")), "result TSV of vnbox verify")
	note := c.fs.String("note", "タイトル", "how far it was checked")
	title := c.fs.String("title", "", "title (overrides the list / window title)")
	replace := c.fs.Bool("replace", false, "replace existing entries of these ids")
	ids := c.parse(args)
	cand, err := readTSV(*list)
	if err != nil {
		return err
	}
	sum, err := readTSV(*summary)
	if err != nil {
		return err
	}
	byID := func(rows [][]string) map[string][]string {
		m := map[string][]string{}
		for _, r := range rows {
			m[r[0]] = append(r, make([]string, 8)...)
		}
		return m
	}
	lst, sm := byID(cand), byID(sum)
	path := ledgerPath("verified.tsv")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var lines []string
	have := map[string]bool{}
	for _, l := range strings.SplitAfter(string(b), "\n") {
		if l == "" {
			continue
		}
		id, _, _ := strings.Cut(l, "\t")
		if *replace && slices.Contains(ids, id) {
			continue
		}
		have[id] = true
		if !strings.HasSuffix(l, "\n") { // a last line without a newline
			l += "\n"
		}
		lines = append(lines, l)
	}
	for _, id := range ids {
		if have[id] {
			continue
		}
		cr, s := lst[id], sm[id]
		if cr == nil || s == nil {
			fmt.Println("unknown id", id)
			continue
		}
		t := *title
		if t == "" {
			t = cr[2]
		}
		if t == "" {
			t = windowTitle(id, s[1])
		}
		if t == "" {
			t = id
		}
		engine := s[4]
		if engine == "" {
			engine = "-"
		}
		var extra []string
		if s[1] == "classic" && s[2] == "OK" {
			extra = append(extra, "従来 Wine")
		}
		if d := cr[4]; d != "" && d != "なし" && d != "-" {
			extra = append(extra, "製品版 DRM: "+d)
		}
		if m := strings.TrimRight(s[7], ","); m != "" {
			extra = append(extra, "痕跡: "+m)
		}
		n := *note
		if len(extra) > 0 {
			n += " (" + strings.Join(extra, ", ") + ")"
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s\n", id, t, engine, n))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0o644); err != nil {
		return err
	}
	strict, _ := ledgerRows()
	fmt.Println("total strict", len(strict))
	return nil
}

// windowTitle is the first game-like window title the check of id saw.
func windowTitle(id, flavor string) string {
	for _, run := range []string{"v_" + id + "_" + flavor, "b6_" + id + "_" + flavor, "r6_" + id + "_" + flavor} {
		b, err := os.ReadFile(filepath.Join(wine.Root(), "results", run, "summary.txt"))
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			if w, ok := strings.CutPrefix(l, "windows="); ok {
				for _, t := range strings.Split(w, "|") {
					if t = strings.TrimSpace(t); !windowSkip.MatchString(t) {
						return t
					}
				}
				return ""
			}
		}
	}
	return ""
}

// ledgerRows returns verified.tsv split into full checks and partial ("弱:") ones.
func ledgerRows() (strict, weak [][]string) {
	rows, _ := readTSV(ledgerPath("verified.tsv"))
	for _, r := range rows {
		r = append(r, make([]string, 4)...)[:4]
		if strings.HasPrefix(r[3], "弱:") {
			weak = append(weak, r)
		} else {
			strict = append(strict, r)
		}
	}
	return
}

// ledgerMD regenerates the tallies (STATS) and the title list (TITLES) of VERIFICATION.md.
func ledgerMD() error {
	path := filepath.Join(wine.Root(), "VERIFICATION.md")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(b)
	strict, weak := ledgerRows()
	if s, err = replaceBetween(s, "<!-- STATS:BEGIN -->", "<!-- STATS:END -->", statsMD(strict, weak)); err != nil {
		return err
	}
	if s, err = replaceBetween(s, "<!-- TITLES:BEGIN -->", "<!-- TITLES:END -->", titlesMD(strict, weak)); err != nil {
		return err
	}
	fmt.Printf("VERIFICATION.md: %d titles (%d partial)\n", len(strict), len(weak))
	return os.WriteFile(path, []byte(s), 0o644)
}

func replaceBetween(s, begin, end, body string) (string, error) {
	a, b := strings.Index(s, begin), strings.Index(s, end)
	if a < 0 || b < a {
		return s, fmt.Errorf("markers %s / %s not found", begin, end)
	}
	return s[:a+len(begin)] + "\n" + body + "\n" + s[b:], nil
}

type counter map[string]int

func (c counter) sorted() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		if c[keys[i]] != c[keys[j]] {
			return c[keys[i]] > c[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}

// statsMD is the tally section: totals per round, exclusions, and for round 6 (the vnbox add
// batch of downloads/batch6.tsv) the sources, retail DRM and engines.
func statsMD(strict, weak [][]string) string {
	excluded, _ := readTSV(ledgerPath("excluded.tsv"))
	b6 := map[string][]string{}
	var b6ids []string
	if rows, err := readTSV(filepath.Join(wine.Root(), "downloads", "batch6.tsv")); err == nil {
		for _, r := range rows {
			b6[r[0]] = append(r, make([]string, 6)...)
			b6ids = append(b6ids, r[0])
		}
	}
	known := map[string]bool{}
	for _, r := range append(append([][]string{}, strict...), weak...) {
		known[r[0]] = true
	}
	for _, e := range excluded {
		known[e[0]] = true
	}
	var ok6, weak6 [][]string
	for _, r := range strict {
		if b6[r[0]] != nil {
			ok6 = append(ok6, r)
		}
	}
	for _, r := range weak {
		if b6[r[0]] != nil {
			weak6 = append(weak6, r)
		}
	}
	ex6, left6 := 0, []string{}
	for _, e := range excluded {
		if b6[e[0]] != nil {
			ex6++
		}
	}
	for _, id := range b6ids {
		if !known[id] {
			left6 = append(left6, id)
		}
	}
	src, drm, eng := counter{}, counter{}, counter{}
	sz, dl := 0, 0
	for _, c := range b6 {
		if c[3] == "suezou" {
			sz++
		} else {
			dl++
		}
	}
	for _, r := range ok6 {
		c := b6[r[0]]
		if c[3] == "suezou" {
			src["suezou"]++
		} else {
			src["DLsite"]++
		}
		if strings.HasPrefix(c[3], "dlsite") {
			d := c[4]
			if d == "" {
				d = "なし"
			}
			drm[d]++
		}
		e := r[2]
		if e == "" || e == "-" {
			e = "判定外 (独自エンジン等)"
		}
		eng[e]++
	}
	var w []string
	for _, r := range weak {
		w = append(w, fmt.Sprintf("`%s` %s", r[0], strings.TrimSpace(strings.TrimPrefix(r[3], "弱:"))))
	}
	out := []string{
		fmt.Sprintf("**タイトル画面以降まで目視で確認できたもの: %d 本** (一覧は下の「起動確認済みタイトル一覧」)。", len(strict)),
		"- 第1〜3弾: 102 本 (第1弾 22 本 + suezou ミラー 80 本)",
		"- 第4弾 (2026-10-19): suezou ミラーから新たに 108 本、ムジナ 1 本、未解決分の解消 59 本",
		fmt.Sprintf("- 第6弾 (2026-10-19〜20、Go 版 `vnbox add` でバッチ): 候補 %d 本 (suezou %d / DLsite %d) から **%d 本** (suezou %d / DLsite %d)。対象外 %d 本、弱い確認 %d 本、未確定 %d 本",
			len(b6), sz, dl, len(ok6), src["suezou"], src["DLsite"], ex6, len(weak6), len(left6)),
		"",
		fmt.Sprintf("対象外 (`results/excluded.tsv`、計 %d 本) は理由付き: 配布物が紹介動画・音声のみ、配布ファイルの破損、VR 専用、GPU 必須 (D3D12 SM6 専用の UE 5.8)。", len(excluded)),
		fmt.Sprintf("弱い確認 (%d 本): %s", len(weak), strings.Join(w, "; ")),
		"",
	}
	if len(left6) > 0 {
		var l []string
		for _, id := range left6 {
			l = append(l, "`"+id+"`")
		}
		out = append(out, "第6弾で未確定: "+strings.Join(l, ", "), "")
	}
	var d []string
	for _, k := range drm.sorted() {
		d = append(d, fmt.Sprintf("%s %d", k, drm[k]))
	}
	out = append(out, "第6弾の DLsite 分の製品版 DRM (作品ページの表記): "+strings.Join(d, " / ")+" (体験版にはどれも入っていない)", "",
		"第6弾のエンジン別 (`vnbox add` の判定):", "", "| エンジン | 本数 |", "|---|---|")
	for _, k := range eng.sorted() {
		out = append(out, fmt.Sprintf("| %s | %d |", k, eng[k]))
	}
	return strings.Join(out, "\n")
}

var mirrorID = regexp.MustCompile(`^[bs](\d{4})_`)

// titlesMD is the list of every checked title, by year.
func titlesMD(strict, weak [][]string) string {
	dl := map[string]string{} // DLsite work number → registration year
	files, _ := filepath.Glob(filepath.Join(wine.Root(), "downloads", "dlsite", "*.tsv"))
	for _, f := range files {
		rows, _ := readTSV(f)
		for _, c := range rows {
			if len(c) > 7 && len(c[7]) >= 4 {
				dl[c[0]] = c[7][:4]
			}
		}
	}
	year := func(id string) string {
		if m := mirrorID.FindStringSubmatch(id); m != nil {
			return m[1]
		}
		if w, ok := strings.CutPrefix(id, "dl_"); ok {
			if y, ok := dl[w]; ok {
				return y
			}
		}
		return "-"
	}
	source := func(id string) string {
		switch {
		case strings.HasPrefix(id, "dl_RJ"):
			return "DLsite 同人"
		case strings.HasPrefix(id, "dl_"):
			return "DLsite"
		case mirrorID.MatchString(id):
			return "suezou"
		}
		return "手動"
	}
	cell := func(s string) string { return strings.TrimSpace(strings.ReplaceAll(s, "|", "／")) }
	order := func(rows [][]string) {
		sort.SliceStable(rows, func(i, j int) bool {
			yi, yj := year(rows[i][0]), year(rows[j][0])
			if (yi == "-") != (yj == "-") {
				return yj == "-"
			}
			if yi != yj {
				return yi < yj
			}
			return rows[i][0] < rows[j][0]
		})
	}
	order(strict)
	order(weak)
	out := []string{
		fmt.Sprintf("タイトル画面以降まで目視で確認: **%d 本** (ロゴ・注意画面止まりの %d 本は下の折りたたみに別記)。", len(strict), len(weak)),
		"年 = 配布ミラーの掲載年度 / DLsite の登録年 (「-」は第1弾の手動テスト分)。エンジンの「-」は自動判定できなかったもの。", "",
		"| # | 年 | 出典 | タイトル | エンジン | 確認できたところ | ID |", "|---|---|---|---|---|---|---|",
	}
	for i, r := range strict {
		out = append(out, fmt.Sprintf("| %d | %s | %s | %s | %s | %s | `%s` |", i+1, year(r[0]), source(r[0]), cell(r[1]), cell(r[2]), cell(r[3]), r[0]))
	}
	out = append(out, "", fmt.Sprintf("<details><summary>弱い確認 (ロゴ・注意画面・起動ダイアログまで) %d 本</summary>", len(weak)), "",
		"| 年 | タイトル | 止まったところ | ID |", "|---|---|---|---|")
	for _, r := range weak {
		out = append(out, fmt.Sprintf("| %s | %s | %s | `%s` |", year(r[0]), cell(r[1]), cell(strings.TrimPrefix(r[3], "弱:")), r[0]))
	}
	out = append(out, "", "</details>")
	return strings.Join(out, "\n")
}

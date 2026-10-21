package main

// vnbox candidates: lists of trial versions to check with vnbox verify.
//
//	candidates suezou <out.tsv>                 every archive linked from the suezou mirror (year, brand, url, page)
//	candidates dlsite <pro|maniax> <pages> <out.tsv>   DLsite PC games with a trial, with the retail DRM
//	candidates pick <out.tsv> [-suezou N -pro N -maniax N]   a verify list from the two (titles not tried yet)

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/a-kaibu/vnbox/internal/wine"
)

func cmdCandidates(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return errors.New("usage: vnbox candidates suezou <out> | dlsite <pro|maniax> <pages> <out> | pick <out>")
	}
	switch args[0] {
	case "suezou":
		if len(args) < 2 {
			return errors.New("usage: vnbox candidates suezou <out.tsv>")
		}
		return suezouLinks(ctx, args[1])
	case "dlsite":
		if len(args) < 4 {
			return errors.New("usage: vnbox candidates dlsite <pro|maniax> <pages> <out.tsv>")
		}
		pages, _ := strconv.Atoi(args[2])
		return dlsiteList(ctx, args[1], pages, args[3])
	case "pick":
		return pickCandidates(ctx, args[1:])
	}
	return fmt.Errorf("unknown candidates command %q", args[0])
}

// httpGet fetches a page (three tries), with the age confirmation cookie DLsite wants.
func httpGet(ctx context.Context, u string) (string, error) {
	var err error
	for i := 0; i < 3; i++ {
		var b []byte
		if b, err = httpGetOnce(ctx, u); err == nil {
			return string(b), nil
		}
		time.Sleep(3 * time.Second)
	}
	return "", err
}

func httpGetOnce(ctx context.Context, u string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Cookie", "adultchecked=1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

const suezouBase = "http://suezou.dyndns.org/"

var (
	suezouPageRe = regexp.MustCompile(`(?i)href="([a-z0-9_\-]+\.html)"`)
	suezouLinkRe = regexp.MustCompile(`(?i)href="([^"]+\.(?:zip|exe|7z|rar|lzh|iso|cab))"`)
	suezouDirRe  = regexp.MustCompile(`/dl(\d{4})[a-z0-9]*/([^/]+)/`)
)

// suezouLinks collects the archive links of every page of the suezou mirror.
func suezouLinks(ctx context.Context, out string) error {
	top, err := httpGet(ctx, suezouBase+"top.html")
	if err != nil {
		return err
	}
	pageSet := map[string]bool{}
	for _, m := range suezouPageRe.FindAllStringSubmatch(top, -1) {
		pageSet[m[1]] = true
	}
	var pages []string
	for p := range pageSet {
		pages = append(pages, p)
	}
	sort.Strings(pages)
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	seen := map[string]bool{}
	base, _ := url.Parse(suezouBase)
	for i, p := range pages {
		html, _ := httpGet(ctx, suezouBase+p)
		time.Sleep(200 * time.Millisecond)
		pu := base.ResolveReference(&url.URL{Path: p})
		for _, m := range suezouLinkRe.FindAllStringSubmatch(html, -1) {
			ref, err := url.Parse(m[1])
			if err != nil {
				continue
			}
			u := pu.ResolveReference(ref).String()
			if seen[u] {
				continue
			}
			seen[u] = true
			y, brand := "?", "?"
			if d := suezouDirRe.FindStringSubmatch(u); d != nil {
				y, brand = d[1], d[2]
			}
			fmt.Fprintf(f, "%s\t%s\t%s\t%s\n", y, brand, u, p)
		}
		if i%40 == 0 {
			fmt.Fprintf(os.Stderr, "%d/%d pages, %d links\n", i, len(pages), len(seen))
		}
	}
	fmt.Printf("done: %d pages, %d links\n", len(pages), len(seen))
	return nil
}

var dlsiteWorkRe = regexp.MustCompile(`product_id/([RVB]J\d{6,8})`)

// dlsiteList lists DLsite PC games with a trial (newest first): workno, name, maker, type,
// options, trial URL, size, release date, retail DRM (from the product page).
func dlsiteList(ctx context.Context, site string, pages int, out string) error {
	cat := map[string]string{"pro": "work_category%5B0%5D/pc", "maniax": "work_category%5B0%5D/doujin/work_type_category%5B0%5D/game"}[site]
	if cat == "" {
		return fmt.Errorf("site must be pro or maniax, not %q", site)
	}
	var ids []string
	seen := map[string]bool{}
	for p := 1; p <= pages; p++ {
		html, err := httpGet(ctx, fmt.Sprintf("https://www.dlsite.com/%s/fsr/=/language/jp/sex_category%%5B0%%5D/male/%s/options%%5B0%%5D/TRI/order/release_d/per_page/100/page/%d", site, cat, p))
		if err != nil {
			return err
		}
		for _, m := range dlsiteWorkRe.FindAllStringSubmatch(html, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				ids = append(ids, m[1])
			}
		}
		time.Sleep(time.Second)
	}
	lines := make([]string, len(ids))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			body, err := httpGet(ctx, fmt.Sprintf("https://www.dlsite.com/%s/api/=/product.json?workno=%s", site, id))
			if err != nil {
				return
			}
			var raw []map[string]json.RawMessage
			if json.Unmarshal([]byte(body), &raw) != nil || len(raw) == 0 {
				return
			}
			str := func(k string) string {
				var s string
				json.Unmarshal(raw[0][k], &s)
				return strings.ReplaceAll(s, "\t", " ")
			}
			var pc bool
			json.Unmarshal(raw[0]["is_pc_work"], &pc)
			var trials []struct {
				URL      string      `json:"url"`
				FileSize json.Number `json:"file_size"`
			}
			json.Unmarshal(raw[0]["trials"], &trials)
			if !pc || len(trials) == 0 || trials[0].URL == "" {
				return
			}
			size := trials[0].FileSize.String()
			if size == "" {
				size = "0"
			}
			date := str("regist_date")
			if len(date) > 10 {
				date = date[:10]
			}
			lines[i] = strings.Join([]string{str("workno"), str("work_name"), str("maker_name"), str("work_type"),
				str("options"), "https:" + trials[0].URL, size, date, retailDRM(ctx, site, id)}, "\t")
		}()
	}
	wg.Wait()
	var keep []string
	for _, l := range lines {
		if l != "" {
			keep = append(keep, l+"\n")
		}
	}
	fmt.Printf("works %d, with a PC trial %d\n", len(ids), len(keep))
	return os.WriteFile(out, []byte(strings.Join(keep, "")), 0o644)
}

// retailDRM reads the DRM of the retail version from the DLsite product page.
func retailDRM(ctx context.Context, site, id string) string {
	h, err := httpGet(ctx, fmt.Sprintf("https://www.dlsite.com/%s/work/=/product_id/%s.html", site, id))
	if err != nil {
		return "?"
	}
	var kinds []string
	for _, k := range []struct{ re, name string }{
		{`PlayDRM型`, "PlayDRM"}, {`ソフト電池`, "ソフト電池"}, {`シリアル番号発行型|シリアル番号を発行`, "シリアル番号"}, {`DLsite Play専用|DLsite Playのみ`, "DLsitePlay"},
	} {
		if regexp.MustCompile(k.re).MatchString(h) {
			kinds = append(kinds, k.name)
		}
	}
	if len(kinds) == 0 {
		return "なし"
	}
	return strings.Join(kinds, "+")
}

var (
	trialName = regexp.MustCompile(`(?i)trial|taiken|_try|try\.|_t\.zip|_tr\.`)
	notTrial  = regexp.MustCompile(`(?i)patch|update|upd|wall|kabe|sabun|bgm|voice|movie|_pv|icon|banner|bnr|ss\d|cm\d|chara|tokuten|omake|_op|op_|manual|font|gekkan|countdown|wp_|screensaver|saver|demo`)
	nonAlnum  = regexp.MustCompile(`[^A-Za-z0-9]`)
)

// pickCandidates builds a vnbox verify list: suezou trials whose work folder was not tried yet
// (years taken in turn, at most two per year and brand, 30 MB - 1.5 GB) and DLsite trials (at
// most two per maker, under 1.6 GB), shuffled so that the sources mix.
func pickCandidates(ctx context.Context, args []string) error {
	c := newFlags("candidates pick")
	nS := c.fs.Int("suezou", 240, "suezou titles")
	nPro := c.fs.Int("pro", 110, "DLsite pro titles")
	nMan := c.fs.Int("maniax", 110, "DLsite maniax (doujin) titles")
	seed := c.fs.Int64("seed", 6, "random seed")
	pos := c.parse(args)
	if len(pos) < 1 {
		return errors.New("usage: vnbox candidates pick <out.tsv>")
	}
	rnd := rand.New(rand.NewSource(*seed))
	dl := filepath.Join(wine.Root(), "downloads")
	slug := func(u string) string { return u[:max(0, strings.LastIndex(u, "/"))] } // the work folder of a URL
	// work folders tried before: every URL in the other lists of downloads/suezou and in the verify lists
	used := map[string]bool{}
	lists, _ := filepath.Glob(filepath.Join(dl, "suezou", "*.tsv"))
	more, _ := filepath.Glob(filepath.Join(dl, "batch*.tsv"))
	for _, f := range append(lists, more...) {
		if filepath.Base(f) == "links.tsv" {
			continue
		}
		rows, _ := readTSV(f)
		for _, r := range rows {
			for _, x := range r {
				if strings.HasPrefix(x, "http") {
					used[slug(x)] = true
				}
			}
		}
	}
	var out [][]string
	links, err := readTSV(filepath.Join(dl, "suezou", "links.tsv"))
	if err != nil {
		return err
	}
	cand := map[string][][]string{}
	for _, r := range links {
		if len(r) < 3 {
			continue
		}
		fn := r[2][strings.LastIndex(r[2], "/")+1:]
		if trialName.MatchString(fn) && !notTrial.MatchString(fn) && !used[slug(r[2])] && r[0] != "?" {
			cand[r[0]] = append(cand[r[0]], r)
		}
	}
	var years []string
	for y := range cand {
		years = append(years, y)
		rnd.Shuffle(len(cand[y]), func(i, j int) { cand[y][i], cand[y][j] = cand[y][j], cand[y][i] })
	}
	sort.Strings(years)
	var order [][]string
	for left := true; left; {
		left = false
		for _, y := range years {
			if n := len(cand[y]); n > 0 {
				order = append(order, cand[y][n-1])
				cand[y] = cand[y][:n-1]
				left = true
			}
		}
	}
	per, seen, n := map[string]int{}, map[string]bool{}, 0
	for _, r := range order {
		if n >= *nS || ctx.Err() != nil {
			break
		}
		y, brand, u := r[0], strings.ReplaceAll(r[1], "-", "_"), r[2]
		if per[y+"/"+brand] >= 2 {
			continue
		}
		size := headSize(ctx, u)
		if size <= 30<<20 || size >= 1500<<20 {
			continue
		}
		work := nonAlnum.ReplaceAllString(filepath.Base(slug(u)), "_")
		if len(work) > 20 {
			work = work[:20]
		}
		id := fmt.Sprintf("s%s_%s_%s", y, brand, work)
		if seen[id] {
			continue
		}
		per[y+"/"+brand]++
		seen[id] = true
		n++
		out = append(out, []string{id, u, "", "suezou", "", strconv.FormatInt(size>>20, 10)})
	}
	for _, s := range []struct {
		site string
		max  int
	}{{"pro", *nPro}, {"maniax", *nMan}} {
		rows, err := readTSV(filepath.Join(dl, "dlsite", s.site+".tsv"))
		if err != nil {
			return err
		}
		rnd.Shuffle(len(rows), func(i, j int) { rows[i], rows[j] = rows[j], rows[i] })
		perMaker, k := map[string]int{}, 0
		for _, r := range rows {
			if len(r) < 9 || k >= s.max {
				continue
			}
			sz, _ := strconv.ParseInt(r[6], 10, 64)
			if perMaker[r[2]] >= 2 || sz <= 0 || sz >= 1600<<20 {
				continue
			}
			perMaker[r[2]]++
			k++
			out = append(out, []string{"dl_" + r[0], r[5], r[1], "dlsite-" + s.site + "/" + r[3], r[8], strconv.FormatInt(sz>>20, 10)})
		}
	}
	rnd.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	var sb strings.Builder
	for _, r := range out {
		sb.WriteString(strings.Join(r, "\t") + "\n")
	}
	fmt.Printf("%d candidates\n", len(out))
	return os.WriteFile(pos[0], []byte(sb.String()), 0o644)
}

// headSize is the Content-Length of url (0 when unknown).
func headSize(ctx context.Context, u string) int64 {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "HEAD", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	return resp.ContentLength
}

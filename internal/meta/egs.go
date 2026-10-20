package meta

import (
	"context"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EGS queries ErogameScape through its public SQL form.
type EGS struct {
	Endpoint string
	Client   *http.Client
	Interval time.Duration // minimum time between requests (politeness)

	mu   sync.Mutex
	next time.Time
}

// NewEGS returns a client for erogamescape.dyndns.org.
func NewEGS() *EGS {
	return &EGS{
		Endpoint: "https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/sql_for_erogamer_form.php",
		Client:   &http.Client{Timeout: 20 * time.Second},
		Interval: 2500 * time.Millisecond,
	}
}

const egsGamePage = "https://erogamescape.dyndns.org/~ap2/ero/toukei_kaiseki/game.php?game="

// Query runs one SQL statement and returns the rows keyed by column name.
func (e *EGS) Query(ctx context.Context, sql string) ([]map[string]string, error) {
	e.mu.Lock()
	wait := time.Until(e.next)
	if wait < 0 {
		wait = 0
	}
	e.next = time.Now().Add(wait + e.Interval)
	e.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(wait):
	}
	req, err := http.NewRequestWithContext(ctx, "POST", e.Endpoint, strings.NewReader(url.Values{"sql": {sql}}.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	resp, err := e.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return parseTable(string(b)), nil
}

var (
	tableRe = regexp.MustCompile(`(?is)<table[^>]*>(.*?)</table>`)
	rowRe   = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	cellRe  = regexp.MustCompile(`(?is)<t[hd][^>]*>(.*?)</t[hd]>`)
	tagRe   = regexp.MustCompile(`(?s)<[^>]*>`)
)

// parseTable reads the first HTML table: the first row holds the column names.
func parseTable(page string) []map[string]string {
	m := tableRe.FindStringSubmatch(page)
	if m == nil {
		return nil
	}
	var cols []string
	var out []map[string]string
	for i, r := range rowRe.FindAllStringSubmatch(m[1], -1) {
		var cells []string
		for _, c := range cellRe.FindAllStringSubmatch(r[1], -1) {
			cells = append(cells, strings.TrimSpace(html.UnescapeString(tagRe.ReplaceAllString(c[1], ""))))
		}
		if i == 0 {
			cols = cells
			continue
		}
		row := map[string]string{}
		for j, v := range cells {
			if j < len(cols) {
				row[cols[j]] = v
			}
		}
		out = append(out, row)
	}
	return out
}

// literal makes an SQL string literal. Non-ASCII text goes through decode(hex) because the
// form rejects non-ASCII bytes in quoted strings.
func literal(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return "convert_from(decode('" + hex.EncodeToString([]byte(s)) + "','hex'),'UTF8')"
}

// Search finds games whose name contains keyword, most voted first.
func (e *EGS) Search(ctx context.Context, keyword string) ([]Candidate, error) {
	if keyword == "" {
		return nil, nil
	}
	esc := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(keyword)
	rows, err := e.Query(ctx, `SELECT g.id, g.gamename, g.sellday, g.count2, b.brandname
FROM gamelist g LEFT JOIN brandlist b ON g.brandname = b.id
WHERE g.gamename LIKE '%' || `+literal(esc)+` || '%' ESCAPE '!'
ORDER BY g.count2 DESC NULLS LAST LIMIT 30`)
	if err != nil {
		return nil, err
	}
	var out []Candidate
	for _, r := range rows {
		v, _ := strconv.Atoi(r["count2"])
		out = append(out, Candidate{Source: "erogamescape", ID: r["id"], Title: r["gamename"], Brand: r["brandname"], Release: r["sellday"], Votes: v})
	}
	return out, nil
}

// povGroups are the POV groups shown as tags (genre, setting, tone).
var povGroups = map[string]bool{"ジャンル": true, "背景": true, "傾向": true}

var povSuffix = regexp.MustCompile(`(仕立てのゲーム|系のゲーム|系ゲーム|なゲーム|のゲーム|な作品|ゲーム|ゲー)$`)

// Get reads one game with its brand, POV tags and series.
func (e *EGS) Get(ctx context.Context, id int) (*Info, error) {
	rows, err := e.Query(ctx, fmt.Sprintf(`SELECT g.id, g.gamename, g.furigana, g.sellday, g.median, g.count2,
g.dmm, g.dmm_subsc, g.genre, g.shoukai, g.dlsite_id, g.dlsite_domain, g.erogame, b.brandname, b.url
FROM gamelist g LEFT JOIN brandlist b ON g.brandname = b.id WHERE g.id = %d`, id))
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	r := rows[0]
	in := &Info{Source: "erogamescape", EGSID: id, Title: r["gamename"], Furigana: r["furigana"], Brand: r["brandname"],
		BrandURL: r["url"], Adult: r["erogame"] == "t"}
	if d := r["sellday"]; len(d) >= 4 && d[:4] < "2030" { // 2030+ means "not yet decided"
		in.Release = d
	}
	in.Score, _ = strconv.Atoi(r["median"])
	in.Votes, _ = strconv.Atoi(r["count2"])
	in.Links = append(in.Links, Link{"批評空間", egsGamePage + strconv.Itoa(id)})
	if s := r["shoukai"]; strings.HasPrefix(s, "http") {
		in.Links = append(in.Links, Link{"公式", s})
	} else if s != "" {
		in.Description = s
	}
	if r["url"] != "" {
		in.Links = append(in.Links, Link{"ブランド", r["url"]})
	}
	if x := firstNonEmpty(r["dmm_subsc"], r["dmm"]); x != "" {
		in.CoverURL = "https://pics.dmm.co.jp/digital/pcgame/" + x + "/" + x + "pl.jpg"
		in.Links = append(in.Links, Link{"DMM", "https://dlsoft.dmm.co.jp/detail/" + x + "/"})
	}
	if x := r["dlsite_id"]; x != "" {
		in.Links = append(in.Links, Link{"DLsite", "https://www.dlsite.com/" + firstNonEmpty(r["dlsite_domain"], "pro") + "/work/=/product_id/" + x + ".html"})
	}
	if g := r["genre"]; g != "" {
		in.Tags = append(in.Tags, g)
	}
	// POV tags (voted by users) and the series
	more, err := e.Query(ctx, fmt.Sprintf(`(SELECT 'tag' AS src, p.title AS val, p.system_group AS grp, pt.count AS cnt
FROM povgroups_toukei pt JOIN povlist p ON pt.pov = p.id WHERE pt.game = %d ORDER BY pt.count DESC)
UNION ALL (SELECT 'series', g.name, '', 0 FROM belong_to_gamegroup_list b JOIN gamegrouplist g ON b.gamegroup = g.id WHERE b.game = %d LIMIT 1)`, id, id))
	if err == nil {
		for _, m := range more {
			switch m["src"] {
			case "tag":
				if n, _ := strconv.Atoi(m["cnt"]); n >= 2 && povGroups[m["grp"]] && len(in.Tags) < 12 {
					in.Tags = appendUnique(in.Tags, povSuffix.ReplaceAllString(m["val"], ""))
				}
			case "series":
				in.Series = m["val"]
			}
		}
	}
	return in, nil
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func appendUnique(list []string, v string) []string {
	if v == "" {
		return list
	}
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

package meta

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSearchKeyAndBest(t *testing.T) {
	for in, want := range map[string]string{
		"Hello,good-bye 体験版":        "Hello,good-bye",
		"彼岸花の咲く夜に お試し読み版":           "彼岸花の咲く夜に",
		"姫狩りダンジョンマイスター 体験版":         "姫狩りダンジョンマイスター",
		"ＲＥＡＬ ＴＲＩＡＬ ＶＥＲＳＩＯＮ":        "REAL",
		"淫落痴漢電車～恥辱姦状線～ 体験版":         "淫落痴漢電車~恥辱姦状線",
		"クダンノフォークロア Web体験版 ver1.01": "クダンノフォークロア",
	} {
		if got := SearchKey(in); got != want {
			t.Errorf("SearchKey(%q) = %q, want %q", in, got, want)
		}
	}
	cands := []Candidate{
		{ID: "1", Title: "Hello,good-bye ファンディスク", Votes: 50},
		{ID: "2", Title: "Hello, good-bye", Votes: 300},
		{ID: "3", Title: "まったく別のゲーム", Votes: 900},
	}
	if c, ok := Best("Hello,good-bye 体験版", cands); !ok || c.ID != "2" {
		t.Errorf("best = %+v %v", c, ok)
	}
	if _, ok := Best("存在しないタイトル", cands); ok {
		t.Error("unrelated title matched")
	}
}

func TestLiteral(t *testing.T) {
	if got := literal("it's"); got != "'it''s'" {
		t.Error(got)
	}
	if got := literal("姫"); got != "convert_from(decode('e5a7ab','hex'),'UTF8')" {
		t.Error(got)
	}
}

// fakeEGS answers the SQL form like ErogameScape does: an HTML table of the result.
func fakeEGS(t *testing.T) *EGS {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		v, _ := url.ParseQuery(string(b))
		sql := v.Get("sql")
		table := func(rows ...string) string {
			return `<html><body><p>result</p><table border=1>` + strings.Join(rows, "") + `</table></body></html>`
		}
		switch {
		case strings.Contains(sql, "LIKE"):
			if !strings.Contains(sql, "decode('") {
				t.Errorf("japanese keyword not hex encoded: %s", sql)
			}
			io.WriteString(w, table(`<tr><th>id</th><th>gamename</th><th>sellday</th><th>count2</th><th>brandname</th></tr>`,
				`<tr><td>777</td><td>姫狩りダンジョンマイスター</td><td>2009-04-24</td><td>1200</td><td>エウシュリー</td></tr>`,
				`<tr><td>778</td><td>姫狩りダンジョンマイスター 遺されし神の血族</td><td>2010-01-01</td><td>40</td><td>エウシュリー</td></tr>`))
		case strings.Contains(sql, "povgroups_toukei"):
			io.WriteString(w, table(`<tr><th>src</th><th>val</th><th>grp</th><th>cnt</th></tr>`,
				`<tr><td>tag</td><td>ファンタジーなゲーム</td><td>背景</td><td>30</td></tr>`,
				`<tr><td>tag</td><td>泣きゲー</td><td>傾向</td><td>1</td></tr>`,
				`<tr><td>series</td><td>ダンジョンマイスター</td><td></td><td>0</td></tr>`))
		default:
			io.WriteString(w, table(`<tr><th>id</th><th>gamename</th><th>furigana</th><th>sellday</th><th>median</th><th>count2</th><th>dmm</th><th>dmm_subsc</th><th>genre</th><th>shoukai</th><th>dlsite_id</th><th>dlsite_domain</th><th>erogame</th><th>brandname</th><th>url</th></tr>`,
				`<tr><td>777</td><td>姫狩りダンジョンマイスター</td><td>ひめがり</td><td>2009-04-24</td><td>78</td><td>1200</td><td>views_0001</td><td></td><td>ダンジョン経営SLG</td><td>http://www.eushully.com/hime/</td><td></td><td></td><td>t</td><td>エウシュリー</td><td>http://www.eushully.com/</td></tr>`))
		}
	}))
	t.Cleanup(srv.Close)
	e := NewEGS()
	e.Endpoint, e.Interval = srv.URL, 0
	return e
}

func TestEGSLookup(t *testing.T) {
	f := &Fetcher{EGS: fakeEGS(t), VNDB: &VNDB{Endpoint: "http://127.0.0.1:1", Client: http.DefaultClient}, Log: io.Discard}
	in, err := f.Lookup(context.Background(), "姫狩りダンジョンマイスター 体験版", "")
	if err != nil {
		t.Fatal(err)
	}
	if in.EGSID != 777 || in.Brand != "エウシュリー" || in.Score != 78 || in.Release != "2009-04-24" || !in.Adult || in.Series != "ダンジョンマイスター" {
		t.Fatalf("info: %+v", in)
	}
	if strings.Join(in.Tags, ",") != "ダンジョン経営SLG,ファンタジー" {
		t.Errorf("tags: %v", in.Tags)
	}
	if in.CoverURL != "https://pics.dmm.co.jp/digital/pcgame/views_0001/views_0001pl.jpg" {
		t.Errorf("cover: %s", in.CoverURL)
	}
}

func TestVNDBFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Filters []any `json:"filters"`
		}
		json.NewDecoder(r.Body).Decode(&q)
		vn := map[string]any{"id": "v1195", "title": "Himegari Dungeon Meister", "alttitle": "姫狩りダンジョンマイスター",
			"released": "2009-04-24", "votecount": 500, "rating": 76.2, "description": "Story [url=http://x]source[/url]",
			"developers": []any{map[string]any{"name": "Eushully", "original": "エウシュリー"}},
			"image":      map[string]any{"url": "https://t.vndb.org/cv/51/28451.jpg", "sexual": 1.6, "violence": 0},
			"tags": []any{map[string]any{"name": "Fantasy", "rating": 2.4, "spoiler": 0, "category": "cont"},
				map[string]any{"name": "Spoiler", "rating": 2.9, "spoiler": 2, "category": "cont"}}}
		json.NewEncoder(w).Encode(map[string]any{"results": []any{vn}})
	}))
	defer srv.Close()
	egs := NewEGS()
	egs.Endpoint, egs.Interval = "http://127.0.0.1:1", 0 // unreachable, like from outside Japan
	f := &Fetcher{EGS: egs, VNDB: &VNDB{Endpoint: srv.URL, Client: srv.Client()}, Log: io.Discard}
	in, err := f.Lookup(context.Background(), "姫狩りダンジョンマイスター 体験版", "")
	if err != nil {
		t.Fatal(err)
	}
	if in.Source != "vndb" || in.Title != "姫狩りダンジョンマイスター" || in.Brand != "エウシュリー" || in.Score != 76 ||
		in.Description != "Story source" || in.CoverURL == "" || strings.Join(in.Tags, ",") != "Fantasy" {
		t.Fatalf("info: %+v", in)
	}
}

func TestSearchKeys(t *testing.T) {
	got := SearchKeys("淫落痴漢電車～恥辱姦状線～ 体験版")
	if len(got) != 2 || got[1] != "淫落痴漢電車" {
		t.Errorf("keys: %q", got)
	}
	if got := SearchKeys("Hello,good-bye 体験版"); len(got) != 1 {
		t.Errorf("keys: %q", got)
	}
}

package meta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"sort"
	"time"
)

// VNDB queries the VNDB Kana API (https://api.vndb.org/kana).
type VNDB struct {
	Endpoint string
	Client   *http.Client
}

// NewVNDB returns a client for api.vndb.org.
func NewVNDB() *VNDB {
	return &VNDB{Endpoint: "https://api.vndb.org/kana/vn", Client: &http.Client{Timeout: 20 * time.Second}}
}

type vndbVN struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	AltTitle   string  `json:"alttitle"`
	Released   string  `json:"released"`
	VoteCount  int     `json:"votecount"`
	Rating     float64 `json:"rating"`
	Desc       string  `json:"description"`
	Developers []struct {
		Name     string `json:"name"`
		Original string `json:"original"`
	} `json:"developers"`
	Image *struct {
		URL      string  `json:"url"`
		Sexual   float64 `json:"sexual"`
		Violence float64 `json:"violence"`
	} `json:"image"`
	Tags []struct {
		Name     string  `json:"name"`
		Rating   float64 `json:"rating"`
		Spoiler  float64 `json:"spoiler"`
		Category string  `json:"category"`
	} `json:"tags"`
	Extlinks []struct {
		URL   string `json:"url"`
		Label string `json:"label"`
	} `json:"extlinks"`
}

func (v *VNDB) query(ctx context.Context, filters any, fields string, n int) ([]vndbVN, error) {
	body, _ := json.Marshal(map[string]any{"filters": filters, "fields": fields, "results": n})
	req, err := http.NewRequestWithContext(ctx, "POST", v.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(b))
	}
	var out struct {
		Results []vndbVN `json:"results"`
	}
	return out.Results, json.Unmarshal(b, &out)
}

// name returns the original-script title when there is one.
func (vn vndbVN) name() string {
	if vn.AltTitle != "" {
		return vn.AltTitle
	}
	return vn.Title
}

func (vn vndbVN) brand() string {
	if len(vn.Developers) == 0 {
		return ""
	}
	if d := vn.Developers[0]; d.Original != "" {
		return d.Original
	}
	return vn.Developers[0].Name
}

// Search finds visual novels by title.
func (v *VNDB) Search(ctx context.Context, keyword string) ([]Candidate, error) {
	if keyword == "" {
		return nil, nil
	}
	res, err := v.query(ctx, []any{"search", "=", keyword}, "title,alttitle,released,votecount,developers{name,original}", 15)
	if err != nil {
		return nil, err
	}
	var out []Candidate
	for _, vn := range res {
		out = append(out, Candidate{Source: "vndb", ID: vn.ID, Title: vn.name(), Brand: vn.brand(), Release: vn.Released, Votes: vn.VoteCount})
		if vn.AltTitle != "" { // let the romanised title match too
			out = append(out, Candidate{Source: "vndb", ID: vn.ID, Title: vn.Title, Brand: vn.brand(), Release: vn.Released, Votes: vn.VoteCount})
		}
	}
	return out, nil
}

var bbcodeRe = regexp.MustCompile(`\[/?[a-z]+(=[^\]]+)?\]`)

// Get reads one visual novel.
func (v *VNDB) Get(ctx context.Context, id string) (*Info, error) {
	res, err := v.query(ctx, []any{"id", "=", id},
		"title,alttitle,released,votecount,rating,description,developers{name,original},image{url,sexual,violence},tags{name,rating,spoiler,category},extlinks{url,label}", 1)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, ErrNotFound
	}
	vn := res[0]
	in := &Info{Source: "vndb", VNDBID: vn.ID, Title: vn.name(), Brand: vn.brand(), Release: vn.Released,
		Score: int(math.Round(vn.Rating)), Votes: vn.VoteCount, Adult: true,
		Description: bbcodeRe.ReplaceAllString(vn.Desc, "")}
	in.Links = append(in.Links, Link{"VNDB", "https://vndb.org/" + vn.ID})
	for _, l := range vn.Extlinks {
		in.Links = append(in.Links, Link{l.Label, l.URL})
	}
	// eroge package art is usually rated "suggestive" (1-1.9); skip explicit (2) or violent covers
	if vn.Image != nil && vn.Image.Sexual < 2 && vn.Image.Violence < 2 {
		in.CoverURL = vn.Image.URL
	}
	tags := vn.Tags
	sort.Slice(tags, func(i, j int) bool { return tags[i].Rating > tags[j].Rating })
	for _, t := range tags {
		if t.Spoiler == 0 && t.Category != "ero" && t.Rating >= 2 && len(in.Tags) < 10 {
			in.Tags = append(in.Tags, t.Name)
		}
	}
	return in, nil
}

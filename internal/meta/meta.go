// Package meta fetches game metadata (title, brand, release date, score, tags, story and
// cover) for the gallery from ErogameScape (批評空間) and, as a fallback or when it cannot
// be reached, from VNDB. The result is stored in the package as meta.json + cover.jpg.
//
// The ErogameScape queries follow minuspiral/playnite-with-erogamescape (MIT License,
// Copyright (c) 2025 minuspiral): one SQL form endpoint, hex-encoded UTF-8 literals for
// Japanese keywords, and at least 2.5 s between requests.
package meta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// File names inside a package directory.
const (
	InfoFile  = "meta.json"
	CoverFile = "cover.jpg"
)

// Info is what the gallery shows about a game.
type Info struct {
	Source      string   `json:"source"` // "erogamescape" or "vndb"
	EGSID       int      `json:"egs_id,omitempty"`
	VNDBID      string   `json:"vndb_id,omitempty"`
	Title       string   `json:"title"`
	Furigana    string   `json:"furigana,omitempty"`
	Brand       string   `json:"brand,omitempty"`
	BrandURL    string   `json:"brand_url,omitempty"`
	Release     string   `json:"release,omitempty"` // yyyy-mm-dd
	Score       int      `json:"score,omitempty"`   // 0-100: ErogameScape median or VNDB rating
	Votes       int      `json:"votes,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Series      string   `json:"series,omitempty"`
	Description string   `json:"description,omitempty"`
	Adult       bool     `json:"adult,omitempty"`
	Links       []Link   `json:"links,omitempty"`
	CoverURL    string   `json:"cover_url,omitempty"`
	Fetched     string   `json:"fetched"`
}

// Link is an external page about the game.
type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Candidate is a search hit.
type Candidate struct {
	Source  string
	ID      string // ErogameScape game id or VNDB id
	Title   string
	Brand   string
	Release string
	Votes   int
}

// Fetcher looks games up. The zero value uses both sources.
type Fetcher struct {
	EGS  *EGS
	VNDB *VNDB
	Log  io.Writer

	egsDown bool // ErogameScape did not answer; do not wait for it again in this run
}

// NewFetcher returns a fetcher with default clients.
func NewFetcher(log io.Writer) *Fetcher {
	if log == nil {
		log = io.Discard
	}
	return &Fetcher{EGS: NewEGS(), VNDB: NewVNDB(), Log: log}
}

// ErrNotFound means no source had a game matching the title.
var ErrNotFound = errors.New("no matching game found")

// Lookup finds the game best matching title (a package title such as "○○ 体験版").
// id may pin the entry: "egs:12345" or "vndb:v1195".
func (f *Fetcher) Lookup(ctx context.Context, title, id string) (*Info, error) {
	switch {
	case strings.HasPrefix(id, "egs:"):
		var n int
		if _, err := fmt.Sscanf(id, "egs:%d", &n); err != nil {
			return nil, fmt.Errorf("bad id %q", id)
		}
		return f.EGS.Get(ctx, n)
	case strings.HasPrefix(id, "vndb:"):
		return f.VNDB.Get(ctx, strings.TrimPrefix(id, "vndb:"))
	case id != "":
		return nil, fmt.Errorf("bad id %q (egs:<n> or vndb:v<n>)", id)
	}
	for _, key := range SearchKeys(title) {
		if f.egsDown {
			break
		}
		cands, err := f.EGS.Search(ctx, key)
		if err != nil {
			fmt.Fprintf(f.Log, "erogamescape: %v (falling back to VNDB)\n", err)
			f.egsDown = true
			break
		}
		if c, ok := Best(title, cands); ok {
			var n int
			fmt.Sscanf(c.ID, "%d", &n)
			return f.EGS.Get(ctx, n)
		}
	}
	var lastErr error = ErrNotFound
	for _, key := range SearchKeys(title) {
		cands, err := f.VNDB.Search(ctx, key)
		if err != nil {
			lastErr = fmt.Errorf("vndb: %w", err)
			continue
		}
		if c, ok := Best(title, cands); ok {
			return f.VNDB.Get(ctx, c.ID)
		}
	}
	return nil, lastErr
}

// Search lists candidates from every reachable source (for choosing an id by hand).
func (f *Fetcher) Search(ctx context.Context, keyword string) ([]Candidate, error) {
	var all []Candidate
	var errs []error
	if c, err := f.EGS.Search(ctx, keyword); err == nil {
		all = append(all, c...)
	} else {
		errs = append(errs, fmt.Errorf("erogamescape: %w", err))
	}
	if c, err := f.VNDB.Search(ctx, keyword); err == nil {
		all = append(all, c...)
	} else {
		errs = append(errs, fmt.Errorf("vndb: %w", err))
	}
	if len(all) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return all, nil
}

// Save writes meta.json into dir and downloads the cover as cover.jpg (max 600 px wide).
func Save(ctx context.Context, dir string, in *Info) error {
	in.Fetched = time.Now().UTC().Format(time.RFC3339)
	b, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, InfoFile), b, 0o644); err != nil {
		return err
	}
	if in.CoverURL == "" {
		return nil
	}
	return download(ctx, in.CoverURL, filepath.Join(dir, CoverFile))
}

// Load reads meta.json from dir.
func Load(dir string) (*Info, error) {
	b, err := os.ReadFile(filepath.Join(dir, InfoFile))
	if err != nil {
		return nil, err
	}
	var in Info
	return &in, json.Unmarshal(b, &in)
}

func download(ctx context.Context, url, dst string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		return fmt.Errorf("cover %s: %s %s", url, resp.Status, resp.Header.Get("Content-Type"))
	}
	tmp := dst + ".src"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, 20<<20))
	f.Close()
	defer os.Remove(tmp)
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-y", "-i", tmp, "-frames:v", "1",
		"-vf", "scale='min(600,iw)':-2", "-q:v", "3", dst).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %v: %s", err, out)
	}
	return nil
}

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

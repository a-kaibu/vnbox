// Package store keeps packages and save data in S3 or an S3-compatible service
// (Cloudflare R2, MinIO, Wasabi ...).
//
//	<root>/packages/<name>.tar.zst   package (tar of packages/<name>/, zstd)
//	<root>/packages/<name>.ini       its launch.ini, for listing
//	<root>/saves/<user>/<name>/<UTC time>.tar.zst   every save, kept as history
//	<root>/saves/<user>/<name>/latest.tar.zst       the newest save
//
// A save holds the files under drive_c that changed after the prefix was prepared,
// plus the registry differences, so it works without knowing where a game saves.
package store

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/klauspost/compress/zstd"

	"github.com/a-kaibu/vnbox/internal/pkg"
	"github.com/a-kaibu/vnbox/internal/regdiff"
	"github.com/a-kaibu/vnbox/internal/wine"
)

// Config is read from the environment, falling back to config/store.conf
// (KEY=VALUE lines).
type Config struct {
	URL      string // s3://bucket/prefix  (VNBOX_S3)
	Endpoint string // S3_ENDPOINT, for S3-compatible services
	User     string // VNBOX_USER, owner of the saves
}

// LoadConfig returns the configuration; URL is empty when storage is not configured.
func LoadConfig() Config {
	file := map[string]string{}
	if f, err := os.Open(filepath.Join(wine.Root(), "config", "store.conf")); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
			if ok {
				file[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
			}
		}
		f.Close()
	}
	get := func(k string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return file[k]
	}
	c := Config{URL: get("VNBOX_S3"), Endpoint: get("S3_ENDPOINT"), User: get("VNBOX_USER")}
	if c.User == "" {
		c.User = "default"
	}
	return c
}

// Store is a connected S3 location.
type Store struct {
	cfg    Config
	bucket string
	root   string // key prefix without trailing slash
	s3     *s3.Client
}

// Open connects using the standard AWS credential chain (env vars, ~/.aws, ...).
func Open(ctx context.Context, cfg Config) (*Store, error) {
	if !strings.HasPrefix(cfg.URL, "s3://") {
		return nil, errors.New("VNBOX_S3=s3://bucket/prefix is not set")
	}
	bucket, root, _ := strings.Cut(strings.TrimPrefix(cfg.URL, "s3://"), "/")
	ac, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	if ac.Region == "" {
		ac.Region = "auto"
		if cfg.Endpoint == "" {
			ac.Region = "us-east-1"
		}
	}
	cl := s3.NewFromConfig(ac, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true // MinIO and most self-hosted services need path-style URLs
		}
	})
	return &Store{cfg: cfg, bucket: bucket, root: strings.TrimSuffix(root, "/"), s3: cl}, nil
}

func (s *Store) key(parts ...string) string {
	return path.Join(append([]string{s.root}, parts...)...)
}

func (s *Store) put(ctx context.Context, key string, r io.Reader) error {
	_, err := manager.NewUploader(s.s3).Upload(ctx, &s3.PutObjectInput{Bucket: &s.bucket, Key: &key, Body: r})
	return err
}

func (s *Store) get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

// IsNotFound reports whether err means the object does not exist.
func IsNotFound(err error) bool {
	var nk *types.NoSuchKey
	return errors.As(err, &nk) || strings.Contains(fmt.Sprint(err), "NotFound") || strings.Contains(fmt.Sprint(err), "NoSuchKey")
}

// PushPackage uploads packages/<name>/ as one tar.zst object (streamed, multipart).
func (s *Store) PushPackage(ctx context.Context, p *pkg.Package) error {
	pr, pw := io.Pipe()
	go func() {
		zw, _ := zstd.NewWriter(pw, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
		err := writeTar(zw, filepath.Dir(p.Dir), p.Name)
		if cerr := zw.Close(); err == nil {
			err = cerr
		}
		pw.CloseWithError(err)
	}()
	if err := s.put(ctx, s.key("packages", p.Name+".tar.zst"), pr); err != nil {
		return err
	}
	ini, err := os.ReadFile(filepath.Join(p.Dir, "launch.ini"))
	if err != nil {
		return err
	}
	if err := s.put(ctx, s.key("packages", p.Name+".ini"), bytes.NewReader(ini)); err != nil {
		return err
	}
	return s.PushThumb(ctx, p)
}

// PushThumb uploads the gallery side objects of a package (thumbnail as packages/<name>.jpg,
// meta.json as .json, cover.jpg as .cover.jpg) so the gallery can show games that are only in
// the store. Missing files are skipped.
func (s *Store) PushThumb(ctx context.Context, p *pkg.Package) error {
	for file, ext := range map[string]string{pkg.ThumbFile: "jpg", "meta.json": "json", "cover.jpg": "cover.jpg"} {
		b, err := os.ReadFile(filepath.Join(p.Dir, file))
		if err != nil {
			continue
		}
		if err := s.PutPackageFile(ctx, p.Name, ext, b); err != nil {
			return err
		}
	}
	return nil
}

// PutPackageFile writes the side object packages/<name>.<ext>.
func (s *Store) PutPackageFile(ctx context.Context, name, ext string, b []byte) error {
	return s.put(ctx, s.key("packages", name+"."+ext), bytes.NewReader(b))
}

// PackageFile reads the small side object packages/<name>.<ext> ("ini" or "jpg").
func (s *Store) PackageFile(ctx context.Context, name, ext string) ([]byte, error) {
	body, err := s.get(ctx, s.key("packages", name+"."+ext))
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(io.LimitReader(body, 8<<20))
}

// PullPackage downloads a package into packages/<name>/ unless it is already there.
func (s *Store) PullPackage(ctx context.Context, name string) (*pkg.Package, error) {
	if p, err := pkg.Open(pkg.Dir(name)); err == nil {
		return p, nil
	}
	body, err := s.get(ctx, s.key("packages", name+".tar.zst"))
	if err != nil {
		return nil, err
	}
	defer body.Close()
	zr, err := zstd.NewReader(body)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	tmp := pkg.Dir(name) + ".partial"
	os.RemoveAll(tmp)
	if err := readTar(zr, tmp); err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	// the archive holds <name>/...; move it into place atomically
	if err := os.Rename(filepath.Join(tmp, name), pkg.Dir(name)); err != nil {
		return nil, err
	}
	os.RemoveAll(tmp)
	return pkg.Open(pkg.Dir(name))
}

// ListResult is the content of the store.
type ListResult struct {
	Packages []Object
	Saves    []Object // latest save per game of the configured user
}

// Object is one listed entry.
type Object struct {
	Name     string
	Size     int64
	Modified time.Time
}

// List returns packages and the user's latest saves.
func (s *Store) List(ctx context.Context) (ListResult, error) {
	var r ListResult
	err := s.walk(ctx, s.key("packages")+"/", func(o types.Object) {
		if k := path.Base(*o.Key); strings.HasSuffix(k, ".tar.zst") {
			r.Packages = append(r.Packages, Object{strings.TrimSuffix(k, ".tar.zst"), *o.Size, *o.LastModified})
		}
	})
	if err != nil {
		return r, err
	}
	err = s.walk(ctx, s.key("saves", s.cfg.User)+"/", func(o types.Object) {
		if path.Base(*o.Key) == "latest.tar.zst" {
			r.Saves = append(r.Saves, Object{path.Base(path.Dir(*o.Key)), *o.Size, *o.LastModified})
		}
	})
	return r, err
}

// SaveHistory lists the timestamps of all saves of a game, oldest first.
func (s *Store) SaveHistory(ctx context.Context, name string) ([]string, error) {
	var out []string
	err := s.walk(ctx, s.key("saves", s.cfg.User, name)+"/", func(o types.Object) {
		if k, ok := strings.CutSuffix(path.Base(*o.Key), ".tar.zst"); ok && k != "latest" { // not lock.json
			out = append(out, k)
		}
	})
	sort.Strings(out)
	return out, err
}

func (s *Store) walk(ctx context.Context, prefix string, fn func(types.Object)) error {
	p := s3.NewListObjectsV2Paginator(s.s3, &s3.ListObjectsV2Input{Bucket: &s.bucket, Prefix: &prefix})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, o := range page.Contents {
			fn(o)
		}
	}
	return nil
}

const (
	savedList = ".vnbox-saved-files" // files restored from a save (their mtimes predate the marker)
	saveHash  = ".vnbox-save-hash"   // hash of the last uploaded save
)

// SaveFiles returns the drive_c-relative paths that belong in a save.
func SaveFiles(prefix string) ([]string, error) {
	marker, err := os.Stat(filepath.Join(prefix, pkg.AppliedMarker))
	if err != nil {
		return nil, fmt.Errorf("prefix is not prepared: %s", prefix)
	}
	driveC := filepath.Join(prefix, "drive_c")
	set := map[string]bool{}
	filepath.WalkDir(driveC, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(driveC, p)
		rel = filepath.ToSlash(rel)
		low := strings.ToLower(rel)
		if d.IsDir() {
			if low == "windows" || strings.HasSuffix(low, "/temp") || low == "temp" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || strings.HasSuffix(low, ".log") {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(marker.ModTime()) {
			set[rel] = true
		}
		return nil
	})
	if b, err := os.ReadFile(filepath.Join(prefix, savedList)); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if l != "" {
				if st, err := os.Stat(filepath.Join(driveC, filepath.FromSlash(l))); err == nil && st.Mode().IsRegular() {
					set[l] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// SaveResult describes a SavePush.
type SaveResult struct {
	Files     int
	Bytes     int
	Timestamp string
	Unchanged bool
}

// SavePush uploads the save of a prepared prefix. Wine should be stopped (or at least
// have flushed its registry) for the registry part to be current.
func (s *Store) SavePush(ctx context.Context, name, prefix string) (SaveResult, error) {
	files, err := SaveFiles(prefix)
	if err != nil {
		return SaveResult{}, err
	}
	frags := map[string][]byte{}
	for _, r := range []string{"user", "system"} {
		base := filepath.Join(prefix, pkg.RegSnapshot, r+".reg")
		if _, err := os.Stat(base); err != nil {
			continue
		}
		a, err := regdiff.Parse(base)
		if err != nil {
			return SaveResult{}, err
		}
		b, err := regdiff.Parse(filepath.Join(prefix, r+".reg"))
		if err != nil {
			return SaveResult{}, err
		}
		tmp, _ := os.CreateTemp("", "frag")
		tmp.Close()
		regdiff.Diff(a, b).WriteFragment(tmp.Name())
		frags[r+".reg.frag"], _ = os.ReadFile(tmp.Name())
		os.Remove(tmp.Name())
	}
	var raw bytes.Buffer
	h := sha256.New()
	tw := tar.NewWriter(&raw)
	list := strings.Join(files, "\n") + "\n"
	io.WriteString(h, list)
	driveC := filepath.Join(prefix, "drive_c")
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(driveC, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		st, _ := os.Stat(filepath.Join(driveC, filepath.FromSlash(f)))
		h.Write(b)
		tw.WriteHeader(&tar.Header{Name: "drive_c/" + f, Mode: 0o644, Size: int64(len(b)), ModTime: st.ModTime(), Format: tar.FormatPAX})
		tw.Write(b)
	}
	addMem(tw, "files.txt", []byte(list))
	for _, n := range []string{"user.reg.frag", "system.reg.frag"} {
		if b, ok := frags[n]; ok {
			h.Write(b)
			addMem(tw, n, b)
		}
	}
	tw.Close()
	if len(files) == 0 && regEmpty(frags) {
		return SaveResult{Unchanged: true}, nil // nothing written yet: do not upload an empty save
	}
	sum := hex.EncodeToString(h.Sum(nil))[:16]
	if old, _ := os.ReadFile(filepath.Join(prefix, saveHash)); string(old) == sum {
		return SaveResult{Files: len(files), Unchanged: true}, nil
	}
	var z bytes.Buffer
	zw, _ := zstd.NewWriter(&z, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	zw.Write(raw.Bytes())
	zw.Close()
	ts := time.Now().UTC().Format("20060102T150405Z")
	for _, k := range []string{ts, "latest"} {
		if err := s.put(ctx, s.key("saves", s.cfg.User, name, k+".tar.zst"), bytes.NewReader(z.Bytes())); err != nil {
			return SaveResult{}, err
		}
	}
	os.WriteFile(filepath.Join(prefix, saveHash), []byte(sum), 0o644)
	return SaveResult{Files: len(files), Bytes: z.Len(), Timestamp: ts}, nil
}

// SavePull restores a save ("latest" or a timestamp) into a prepared prefix.
// It returns found=false when there is no save. Wine must not be running.
func (s *Store) SavePull(ctx context.Context, name, prefix, ts string) (found bool, files int, err error) {
	if ts == "" {
		ts = "latest"
	}
	body, err := s.get(ctx, s.key("saves", s.cfg.User, name, ts+".tar.zst"))
	if err != nil {
		if IsNotFound(err) {
			return false, 0, nil
		}
		return false, 0, err
	}
	defer body.Close()
	zr, err := zstd.NewReader(body)
	if err != nil {
		return true, 0, err
	}
	defer zr.Close()
	tmp, err := os.MkdirTemp("", "vnbox-save")
	if err != nil {
		return true, 0, err
	}
	defer os.RemoveAll(tmp)
	if err := readTar(zr, tmp); err != nil {
		return true, 0, err
	}
	driveC := filepath.Join(prefix, "drive_c")
	list, _ := os.ReadFile(filepath.Join(tmp, "files.txt"))
	var restored []string
	for _, f := range strings.Split(string(list), "\n") {
		if f == "" {
			continue
		}
		src := filepath.Join(tmp, "drive_c", filepath.FromSlash(f))
		b, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		dst := filepath.Join(driveC, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(dst), 0o755)
		os.Remove(dst) // never write through a hard link shared with the base prefix
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return true, 0, err
		}
		if st, err := os.Stat(src); err == nil {
			os.Chtimes(dst, st.ModTime(), st.ModTime())
		}
		restored = append(restored, f)
	}
	for _, r := range []string{"user", "system"} {
		frag := filepath.Join(tmp, r+".reg.frag")
		if st, err := os.Stat(frag); err == nil && st.Size() > 0 {
			if err := regdiff.Apply(frag, filepath.Join(prefix, r+".reg")); err != nil {
				return true, 0, err
			}
		}
	}
	// remember restored files: their old mtimes would otherwise drop them from the next save
	set := map[string]bool{}
	for _, f := range restored {
		set[f] = true
	}
	if b, err := os.ReadFile(filepath.Join(prefix, savedList)); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if l != "" {
				set[l] = true
			}
		}
	}
	all := make([]string, 0, len(set))
	for k := range set {
		all = append(all, k)
	}
	sort.Strings(all)
	os.WriteFile(filepath.Join(prefix, savedList), []byte(strings.Join(all, "\n")+"\n"), 0o644)
	return true, len(restored), nil
}

func regEmpty(frags map[string][]byte) bool {
	for _, b := range frags {
		if bytes.Contains(b, []byte("\n[")) {
			return false
		}
	}
	return true
}

func addMem(tw *tar.Writer, name string, b []byte) {
	tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(b)), ModTime: time.Now(), Format: tar.FormatPAX})
	tw.Write(b)
}

// writeTar writes dir/<name> (recursively) with paths relative to dir.
func writeTar(w io.Writer, dir, name string) error {
	tw := tar.NewWriter(w)
	err := filepath.WalkDir(filepath.Join(dir, name), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, _ = os.Readlink(p)
		}
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if d.IsDir() {
			hdr.Name += "/"
		}
		hdr.Format = tar.FormatPAX
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, f)
			f.Close()
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

// readTar extracts a tar stream into dst, refusing paths that escape it.
func readTar(r io.Reader, dst string) error {
	root, _ := filepath.Abs(dst)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p := filepath.Join(root, filepath.FromSlash(h.Name))
		if p != root && !strings.HasPrefix(p, root+string(filepath.Separator)) {
			return fmt.Errorf("unsafe path in archive: %q", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(p), 0o755)
			f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o777|0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
			os.Chtimes(p, h.ModTime, h.ModTime)
		case tar.TypeSymlink:
			// only relative links that stay inside the extracted tree
			if filepath.IsAbs(h.Linkname) || strings.HasPrefix(filepath.Clean(filepath.Join(filepath.Dir(h.Name), h.Linkname)), "..") {
				continue
			}
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.Symlink(h.Linkname, p)
		}
	}
}

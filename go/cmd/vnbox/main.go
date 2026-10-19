// Command vnbox runs Windows visual novels under Wine and streams each game window
// to a browser. See `vnbox help`.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"vnbox/internal/add"
	"vnbox/internal/capture"
	"vnbox/internal/detect"
	"vnbox/internal/extract"
	"vnbox/internal/meta"
	"vnbox/internal/pkg"
	"vnbox/internal/portal"
	"vnbox/internal/regdiff"
	"vnbox/internal/session"
	"vnbox/internal/store"
	"vnbox/internal/stream"
	"vnbox/internal/wine"
)

const usage = `vnbox — Windows ノベルゲームを Wine で動かしてブラウザ配信する

  vnbox add <name> <配布物> [-exe 相対パス] [-click x,y;...] [-no-upload]   zip/lzh/7z/rar/SFX/インストーラ/フォルダからパッケージを作る
  vnbox meta <name> [-title T] [-engine E] [-thumb 画像]  ギャラリー表示用のタイトル・エンジン・サムネイルを設定
  vnbox meta <name> -fetch [-id egs:N|vndb:vN] | meta -all   批評空間 (届かなければ VNDB) からブランド・発売日・点数・タグ・あらすじ・カバーを取得
  vnbox meta-search <キーワード>                        批評空間/VNDB の候補 ID を一覧
  vnbox serve [-addr :8080] [-token T]                ポータル: ギャラリーから選んで起動・配信 (複数同時)
  vnbox play <name> [-addr :8080] [-token T]          1 本だけ起動して配信
  vnbox prepare <name>                                実行用 prefix を準備してパスを表示
  vnbox store list | history <name>
  vnbox store push-package <name> | pull-package <name>
  vnbox store save-push <name> | save-pull <name> [時刻]
  vnbox capture begin <name> / capture end <name>     手動インストールの差分をパッケージ化
  vnbox detect <フォルダ|exe>                          エンジン・インストーラ・依存の判定 (JSON)
  vnbox extract <アーカイブ> <出力先>
  vnbox regdiff diff <before> <after> <out> | apply <frag> <target> | show <frag>

共通: -wine vnbox|classic (既定 vnbox。環境変数 VNBOX_FLAVOR)、データの置き場所は $VNBOX (既定 /home/user/vnbox)
S3: VNBOX_S3=s3://bucket/prefix, S3_ENDPOINT (R2/MinIO 等), VNBOX_USER, 認証は AWS 標準の環境変数/プロファイル
    (scripts/store.conf にも書ける)。トークンは -token か VNBOX_TOKEN。
`

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 || os.Args[1] == "help" || os.Args[1] == "-h" {
		fmt.Print(usage)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "add":
		err = cmdAdd(ctx, args)
	case "meta":
		err = cmdMeta(ctx, args)
	case "meta-search":
		err = cmdMetaSearch(ctx, args)
	case "serve":
		err = cmdServe(ctx, args)
	case "play":
		err = cmdPlay(ctx, args)
	case "prepare":
		err = cmdPrepare(ctx, args)
	case "store":
		err = cmdStore(ctx, args)
	case "capture":
		err = cmdCapture(args)
	case "detect":
		err = need(args, 1, func() error {
			b, _ := json.MarshalIndent(detect.Detect(args[0]), "", " ")
			fmt.Println(string(b))
			return nil
		})
	case "extract":
		err = need(args, 2, func() error { return extract.Extract(args[0], args[1]) })
	case "regdiff":
		err = cmdRegdiff(args)
	default:
		err = fmt.Errorf("unknown command %q (vnbox help)", cmd)
	}
	if err != nil {
		log.Fatal("vnbox: ", err)
	}
}

func need(args []string, n int, f func() error) error {
	if len(args) < n {
		return errors.New("missing arguments (vnbox help)")
	}
	return f()
}

// common flags
type common struct {
	fs     *flag.FlagSet
	flavor *string
}

func newFlags(name string) common {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	def := os.Getenv("VNBOX_FLAVOR")
	if def == "" {
		def = "vnbox"
	}
	return common{fs: fs, flavor: fs.String("wine", def, "vnbox | classic")}
}

func (c common) Flavor() wine.Flavor { return wine.Flavor(*c.flavor) }

// parse allows flags after positional arguments (vnbox add name file -exe x).
func (c common) parse(args []string) []string {
	var pos []string
	for len(args) > 0 {
		c.fs.Parse(args)
		args = c.fs.Args()
		if len(args) > 0 {
			pos = append(pos, args[0])
			args = args[1:]
		}
	}
	return pos
}

func openStore(ctx context.Context) (*store.Store, error) {
	cfg := store.LoadConfig()
	if cfg.URL == "" {
		return nil, nil
	}
	return store.Open(ctx, cfg)
}

func cmdAdd(ctx context.Context, args []string) error {
	c := newFlags("add")
	exe := c.fs.String("exe", "", "起動 exe (ゲームフォルダからの相対パス)")
	clickFlag := c.fs.String("click", "", "インストーラの最初の画面でクリックする座標 (例: 695,203;400,300)")
	up := c.fs.Bool("upload", false, "S3 にも上げる (VNBOX_S3 が設定されていれば既定で上げる)")
	noUp := c.fs.Bool("no-upload", false, "S3 に上げない")
	title := c.fs.String("title", "", "ギャラリーに出すタイトル (既定はパッケージ名)")
	pos := c.parse(args)
	if len(pos) < 2 {
		return errors.New("usage: vnbox add <name> <配布物>")
	}
	var clicks [][2]int
	for _, pt := range strings.Split(*clickFlag, ";") {
		var x, y int
		if _, err := fmt.Sscanf(strings.TrimSpace(pt), "%d,%d", &x, &y); err == nil {
			clicks = append(clicks, [2]int{x, y})
		}
	}
	p, err := add.Add(ctx, pos[0], pos[1], add.Options{Flavor: c.Flavor(), Exe: *exe, Clicks: clicks, Log: os.Stderr})
	if err != nil {
		return err
	}
	l, _ := p.Launch()
	// gallery metadata: the title given and the engine of the installed game folder
	l.Title = *title
	gdir := filepath.Join(p.Dir, "files", filepath.Dir(filepath.FromSlash(l.Target)))
	det := detect.Detect(gdir)
	if e := det.Engine; l.Engine == "" && e != "" && e != "unknown" {
		l.Engine = e
	}
	var drm []string
	for _, t := range det.Tags {
		// Electron (TyranoScript v5 等): GPU プロセスが Wine + GPU なしで起動できず WebGL が使えないので、
		// GPU 処理を本体プロセスに入れ、ソフトウェア描画 (SwiftShader) を使わせる
		if t == "Runtime:Electron" && l.Args == "" {
			l.Args = electronArgs
		}
		// XAML Islands (C++/WinRT の Windows.UI.Xaml): Wine に実装が無いので何もしない代用品を入れる
		if t == "Runtime:XamlIslands" && !slices.Contains(l.Winetricks, "vnbox-xamlstub") {
			l.Winetricks = append(l.Winetricks, "vnbox-xamlstub")
		}
		if strings.HasPrefix(t, "DRM:") || strings.HasPrefix(t, "Protect:") {
			drm = append(drm, t)
		}
	}
	if len(drm) > 0 {
		l.DRM = strings.Join(drm, ",")
		fmt.Fprintf(os.Stderr, "warning: DRM / protection found: %s (purchased copies may need online activation)\n", l.DRM)
	}
	if err := pkg.WriteLaunch(p.Dir, l); err != nil {
		return err
	}
	fmt.Printf("package: %s\ntarget:  %s\nengine:  %s\n", p.Dir, l.Target, l.Engine)
	if l.DRM != "" {
		fmt.Printf("drm:     %s\n", l.DRM)
	}
	st, err := openStore(ctx)
	if err != nil {
		return err
	}
	if st != nil && !*noUp || *up {
		if st == nil {
			return errors.New("VNBOX_S3 is not set")
		}
		if err := st.PushPackage(ctx, p); err != nil {
			return err
		}
		fmt.Println("uploaded to", store.LoadConfig().URL)
	}
	return nil
}

func sessionOptions(ctx context.Context, c common, fps int) (session.Options, error) {
	st, err := openStore(ctx)
	if err != nil {
		return session.Options{}, err
	}
	iv := 5 * time.Minute
	if v := os.Getenv("SAVE_INTERVAL"); v != "" {
		var sec int
		fmt.Sscan(v, &sec)
		if sec > 0 {
			iv = time.Duration(sec) * time.Second
		}
	}
	return session.Options{Flavor: c.Flavor(), Store: st, SaveInterval: iv, FPS: fps, Log: os.Stderr}, nil
}

func token(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return os.Getenv("VNBOX_TOKEN")
}

func listen(ctx context.Context, addr string, h http.Handler, onStop func()) error {
	srv := &http.Server{Addr: addr, Handler: h}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Print("shutting down (saving)...")
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(sctx)
	onStop()
	return nil
}

// rtcFlags adds the WebRTC network flags; the returned function applies them after parsing.
func rtcFlags(c common) func() error {
	port := c.fs.Int("rtc-port", 0, "WebRTC: 1 つの UDP+TCP ポートに映像・音声をまとめる (ポート開放/転送する場合。0 = 任意の UDP)")
	pub := c.fs.String("public-ip", os.Getenv("VNBOX_PUBLIC_IP"), "WebRTC: 外から見えるアドレス (NAT 越し。カンマ区切り)")
	ice := c.fs.String("ice", os.Getenv("VNBOX_ICE"), "WebRTC: STUN/TURN (例 stun:stun.l.google.com:19302,turn:user:pass@turn.example.com:3478)")
	kbps := c.fs.Int("bitrate", 3000, "WebRTC: 映像の上限 kbit/s")
	return func() error {
		cfg := stream.RTCConfig{Port: *port, Bitrate: *kbps}
		for _, s := range strings.Split(*pub, ",") {
			if s = strings.TrimSpace(s); s != "" {
				cfg.PublicIPs = append(cfg.PublicIPs, s)
			}
		}
		for _, s := range strings.Split(*ice, ",") {
			if s = strings.TrimSpace(s); s != "" {
				srv, err := stream.ParseICE(s)
				if err != nil {
					return err
				}
				cfg.ICEServers = append(cfg.ICEServers, srv)
			}
		}
		return stream.ConfigureRTC(cfg)
	}
}

func cmdServe(ctx context.Context, args []string) error {
	c := newFlags("serve")
	addr := c.fs.String("addr", ":8080", "listen address")
	tok := c.fs.String("token", "", "access token (or VNBOX_TOKEN)")
	fps := c.fs.Int("fps", 15, "video frame rate of the MJPEG fallback (WebRTC uses at least 30)")
	rtc := rtcFlags(c)
	c.parse(args)
	if err := rtc(); err != nil {
		return err
	}
	opt, err := sessionOptions(ctx, c, *fps)
	if err != nil {
		return err
	}
	t := token(*tok)
	if t == "" {
		log.Print("warning: no token; anyone who can reach this port can play and control games")
	}
	p := portal.New(t, opt)
	log.Printf("portal on http://%s/%s", hostAddr(*addr), tokenQuery(t))
	return listen(ctx, *addr, p.Handler(), p.Shutdown)
}

func cmdPlay(ctx context.Context, args []string) error {
	c := newFlags("play")
	addr := c.fs.String("addr", ":8080", "listen address")
	tok := c.fs.String("token", "", "access token (or VNBOX_TOKEN)")
	fps := c.fs.Int("fps", 15, "video frame rate of the MJPEG fallback (WebRTC uses at least 30)")
	rtc := rtcFlags(c)
	pos := c.parse(args)
	if len(pos) < 1 {
		return errors.New("usage: vnbox play <name>")
	}
	if err := rtc(); err != nil {
		return err
	}
	opt, err := sessionOptions(ctx, c, *fps)
	if err != nil {
		return err
	}
	s, err := session.Start(ctx, pos[0], opt)
	if err != nil {
		return err
	}
	t := token(*tok)
	p := portal.New(t, opt)
	log.Printf("playing %s on %s -> http://%s/g/%s/%s", pos[0], s.Display, hostAddr(*addr), pos[0], tokenQuery(t))
	p.Adopt(s)
	pctx, cancel := context.WithCancel(ctx)
	go func() { <-s.Done(); cancel() }() // quitting the game ends the command
	return listen(pctx, *addr, p.Handler(), p.Shutdown)
}

func hostAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		h, _ := os.Hostname()
		return h + addr
	}
	return addr
}

func tokenQuery(t string) string {
	if t == "" {
		return ""
	}
	return "?token=" + t
}

func cmdPrepare(ctx context.Context, args []string) error {
	c := newFlags("prepare")
	pos := c.parse(args)
	if len(pos) < 1 {
		return errors.New("usage: vnbox prepare <name>")
	}
	p, err := pkg.Open(pkg.Dir(pos[0]))
	if err != nil {
		return err
	}
	prefix := session.Prefix(pos[0])
	l, err := p.Launch()
	if err != nil {
		return err
	}
	if err := p.Prepare(ctx, l.WineFlavor(c.Flavor()), prefix); err != nil {
		return err
	}
	fmt.Println(prefix)
	return nil
}

func cmdStore(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return errors.New("usage: vnbox store <list|history|push-package|pull-package|save-push|save-pull> ...")
	}
	st, err := openStore(ctx)
	if err != nil {
		return err
	}
	if st == nil {
		return errors.New("VNBOX_S3=s3://bucket/prefix is not set")
	}
	sub, a := args[0], args[1:]
	switch sub {
	case "list":
		l, err := st.List(ctx)
		if err != nil {
			return err
		}
		fmt.Println("== packages")
		for _, o := range l.Packages {
			fmt.Printf("  %-40s %s  %d\n", o.Name, o.Modified.Local().Format("2006-01-02 15:04"), o.Size)
		}
		fmt.Printf("== saves (%s)\n", store.LoadConfig().User)
		for _, o := range l.Saves {
			fmt.Printf("  %-40s %s\n", o.Name, o.Modified.Local().Format("2006-01-02 15:04"))
		}
		return nil
	case "history":
		return need(a, 1, func() error {
			h, err := st.SaveHistory(ctx, a[0])
			for _, t := range h {
				fmt.Println(t)
			}
			return err
		})
	case "push-package":
		return need(a, 1, func() error {
			p, err := pkg.Open(pkgPath(a[0]))
			if err != nil {
				return err
			}
			if err := st.PushPackage(ctx, p); err != nil {
				return err
			}
			fmt.Println("uploaded", p.Name)
			return nil
		})
	case "pull-package":
		return need(a, 1, func() error {
			p, err := st.PullPackage(ctx, a[0])
			if err == nil {
				fmt.Println(p.Dir)
			}
			return err
		})
	case "save-push":
		return need(a, 1, func() error {
			prefix := session.Prefix(a[0])
			wine.Env{Flavor: wine.Vnbox, Prefix: prefix}.Wait()
			r, err := st.SavePush(ctx, a[0], prefix)
			if err != nil {
				return err
			}
			if r.Unchanged {
				fmt.Println("save unchanged")
			} else {
				fmt.Printf("saved %d files (%d bytes) %s\n", r.Files, r.Bytes, r.Timestamp)
			}
			return nil
		})
	case "save-pull":
		return need(a, 1, func() error {
			ts := ""
			if len(a) > 1 {
				ts = a[1]
			}
			prefix := session.Prefix(a[0])
			if !pkg.Prepared(prefix) {
				return fmt.Errorf("prefix not prepared (vnbox prepare %s)", a[0])
			}
			wine.Env{Flavor: wine.Vnbox, Prefix: prefix}.Kill()
			found, n, err := st.SavePull(ctx, a[0], prefix, ts)
			if err == nil {
				if found {
					fmt.Printf("restored %d files\n", n)
				} else {
					fmt.Println("no save")
				}
			}
			return err
		})
	}
	return fmt.Errorf("unknown store command %q", sub)
}

func pkgPath(nameOrDir string) string {
	if strings.Contains(nameOrDir, "/") {
		return nameOrDir
	}
	return pkg.Dir(nameOrDir)
}

func cmdCapture(args []string) error {
	c := newFlags("capture")
	pos := c.parse(args)
	if len(pos) < 2 {
		return errors.New("usage: vnbox capture begin|end <name>")
	}
	switch pos[0] {
	case "begin":
		s, err := capture.Begin(pos[1], c.Flavor())
		if err != nil {
			return err
		}
		fmt.Printf("snapshot taken. install with:\n  WINEPREFIX=%s %s/wine <setup.exe>\nthen: vnbox capture end %s\n", s.Prefix, c.Flavor().Bin(), pos[1])
	case "end":
		s, err := capture.Resume(pos[1], c.Flavor())
		if err != nil {
			return err
		}
		r, err := s.End()
		if err != nil {
			return err
		}
		fmt.Printf("package: %s (%d files)\ncandidates:\n  %s\n", r.Package, r.Files, strings.Join(r.Candidates, "\n  "))
	default:
		return errors.New("usage: vnbox capture begin|end <name>")
	}
	return nil
}

func cmdRegdiff(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: vnbox regdiff diff|apply|show ...")
	}
	switch args[0] {
	case "diff":
		return need(args, 4, func() error {
			a, err := regdiff.Parse(args[1])
			if err != nil {
				return err
			}
			b, err := regdiff.Parse(args[2])
			if err != nil {
				return err
			}
			d := regdiff.Diff(a, b)
			k, v := d.Count()
			fmt.Printf("%d keys, %d values\n", k, v)
			return d.WriteFragment(args[3])
		})
	case "apply":
		return need(args, 3, func() error { return regdiff.Apply(args[1], args[2]) })
	case "show":
		return regdiff.Show(args[1], bufio.NewWriter(os.Stdout))
	}
	return fmt.Errorf("unknown regdiff command %q", args[0])
}

const electronArgs = "--in-process-gpu --use-angle=swiftshader --enable-unsafe-swiftshader --ignore-gpu-blocklist"

// cmdMeta edits what the gallery shows for a package; with VNBOX_S3 the side objects are updated too.
func cmdMeta(ctx context.Context, args []string) error {
	c := newFlags("meta")
	title := c.fs.String("title", "", "タイトル")
	engine := c.fs.String("engine", "", "エンジン名")
	thumb := c.fs.String("thumb", "", "サムネイルにする画像 (png/jpg 等。480px 幅の JPEG に変換)")
	fetch := c.fs.Bool("fetch", false, "批評空間 (届かなければ VNDB) からメタデータとカバー画像を取得")
	id := c.fs.String("id", "", "取得するエントリを指定: egs:<ゲームID> / vndb:v<ID> (-fetch を含む)")
	all := c.fs.Bool("all", false, "-fetch を全パッケージ (meta.json が無いもの) に対して行う")
	pos := c.parse(args)
	if *all {
		return fetchAllMeta(ctx)
	}
	if len(pos) < 1 {
		return errors.New("usage: vnbox meta <name> [-title T] [-engine E] [-thumb 画像] [-fetch] [-id egs:N|vndb:vN] | vnbox meta -all")
	}
	p, err := pkg.Open(pkg.Dir(pos[0]))
	if err != nil {
		return err
	}
	if *fetch || *id != "" {
		if err := fetchMeta(ctx, meta.NewFetcher(os.Stderr), p, *id); err != nil {
			return err
		}
	}
	l, err := p.Launch()
	if err != nil {
		return err
	}
	if *title != "" {
		l.Title = *title
	}
	if *engine != "" {
		l.Engine = *engine
	}
	if err := pkg.WriteLaunch(p.Dir, l); err != nil {
		return err
	}
	if *thumb != "" {
		out, err := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-y", "-i", *thumb, "-frames:v", "1",
			"-vf", "scale='min(480,iw)':-2", "-q:v", "4", p.Thumb()).CombinedOutput()
		if err != nil {
			return fmt.Errorf("ffmpeg: %v: %s", err, out)
		}
	}
	fmt.Printf("title=%s engine=%s thumb=%v\n", l.Title, l.Engine, fileExists(p.Thumb()))
	st, err := openStore(ctx)
	if err != nil || st == nil {
		return err
	}
	if ini, err := os.ReadFile(filepath.Join(p.Dir, "launch.ini")); err == nil {
		if err := st.PutPackageFile(ctx, p.Name, "ini", ini); err != nil {
			return err
		}
	}
	return st.PushThumb(ctx, p)
}

// fetchMeta looks the package up by its gallery title (or pinned id) and stores meta.json + cover.jpg.
func fetchMeta(ctx context.Context, f *meta.Fetcher, p *pkg.Package, id string) error {
	l, err := p.Launch()
	if err != nil {
		return err
	}
	title := l.Title
	if title == "" {
		title = p.Name
	}
	in, err := f.Lookup(ctx, title, id)
	if err != nil {
		return fmt.Errorf("%s (%s): %w", p.Name, title, err)
	}
	if err := meta.Save(ctx, p.Dir, in); err != nil {
		fmt.Fprintf(os.Stderr, "%s: cover: %v\n", p.Name, err)
	}
	fmt.Printf("%s: %s / %s / %s / score %d (%s)\n", p.Name, in.Title, in.Brand, in.Release, in.Score, in.Source)
	return nil
}

func fetchAllMeta(ctx context.Context) error {
	ents, err := os.ReadDir(filepath.Join(wine.Root(), "packages"))
	if err != nil {
		return err
	}
	f := meta.NewFetcher(io.Discard)
	st, _ := openStore(ctx)
	for _, e := range ents {
		p, err := pkg.Open(pkg.Dir(e.Name()))
		if err != nil || fileExists(filepath.Join(p.Dir, meta.InfoFile)) {
			continue
		}
		if err := fetchMeta(ctx, f, p, ""); err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		if st != nil {
			st.PushThumb(ctx, p)
		}
	}
	return nil
}

// cmdMetaSearch lists database entries for a keyword, to pick an id for `vnbox meta -id`.
func cmdMetaSearch(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return errors.New("usage: vnbox meta-search <キーワード>")
	}
	cands, err := meta.NewFetcher(os.Stderr).Search(ctx, strings.Join(args, " "))
	if err != nil {
		return err
	}
	for _, c := range cands {
		pre := "egs:"
		if c.Source == "vndb" {
			pre = "vndb:"
		}
		fmt.Printf("%-14s %-10s %5d票  %s  (%s)\n", pre+c.ID, c.Release, c.Votes, c.Title, c.Brand)
	}
	return nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

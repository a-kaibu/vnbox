// Command vnbox runs Windows visual novels under Wine and streams each game window
// to a browser. See `vnbox help`.
package main

import (
	"bufio"
	"bytes"
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
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/a-kaibu/vnbox/internal/add"
	"github.com/a-kaibu/vnbox/internal/base"
	"github.com/a-kaibu/vnbox/internal/capture"
	"github.com/a-kaibu/vnbox/internal/detect"
	"github.com/a-kaibu/vnbox/internal/extract"
	"github.com/a-kaibu/vnbox/internal/meta"
	"github.com/a-kaibu/vnbox/internal/pkg"
	"github.com/a-kaibu/vnbox/internal/portal"
	"github.com/a-kaibu/vnbox/internal/regdiff"
	"github.com/a-kaibu/vnbox/internal/session"
	"github.com/a-kaibu/vnbox/internal/store"
	"github.com/a-kaibu/vnbox/internal/stream"
	"github.com/a-kaibu/vnbox/internal/webplay"
	"github.com/a-kaibu/vnbox/internal/wine"
	"github.com/a-kaibu/vnbox/internal/winebuild"
)

const usage = `vnbox — Windows ノベルゲームを Wine で動かしてブラウザ配信する

  vnbox install-wine                                  自前 Wine (パッチ入り Wine) のビルド済みを GitHub の Releases から <データ>/wine に入れる (add / play が初回に自動で行う)
  vnbox build-wine [-j N] [-prefix dir] [-tag]        自前 Wine を手元でビルドする (Releases にまだ無いパッチのとき)
  vnbox add <name> <配布物> [-exe 相対パス] [-click x,y;...] [-no-upload]   zip/lzh/7z/rar/SFX/インストーラ/フォルダからパッケージを作る
  vnbox meta <name> [-title T] [-engine E] [-thumb 画像]  ギャラリー表示用のタイトル・エンジン・サムネイルを設定
  vnbox meta <name> -fetch [-id egs:N|vndb:vN] | meta -all   批評空間 (届かなければ VNDB) からブランド・発売日・点数・タグ・あらすじ・カバーを取得
  vnbox meta-search <キーワード>                        批評空間/VNDB の候補 ID を一覧
  vnbox serve [-addr :8080] [-token T] [-player dir]  ポータル: ギャラリーから選んで起動・配信 (複数同時)。-player は vnrun の dist/
  vnbox play <name> [-addr :8080] [-token T]          1 本だけ起動して配信
  vnbox browser [-all | <name>...]                    vnrun が動かせる作品をブラウザでも遊べるようにする (launch.ini に runner=vnrun)
  vnbox mkbase [-win64] [dir]                         ベース prefix を作る (最初に一度。-wine classic なら 32bit / -win64 で 64bit)
  vnbox prepare <name> [-prefix dir]                  実行用 prefix を準備してパスを表示
  vnbox probe <name> [-input 操作列] [-wait 30s]      play と同じ起動で動かし、操作列を流してスクショと判定 (results/<name>/)
  vnbox verify <list.tsv> [-j 3] [-out tsv]           配布物の一覧を DL → add → probe (vnbox → classic) → 一覧画像 → 結果 TSV
  vnbox ledger record [-note N] <id>... | ledger md   目視で確かめたものを台帳に記録 / VERIFICATION.md を作り直す
  vnbox candidates suezou <out> | dlsite <pro|maniax> <ページ数> <out> | pick <out>   verify に渡す候補の一覧を作る
  vnbox stop-all                                      動いているゲームと X をすべて止める
  vnbox store list | history <name>
  vnbox store push-package <name> | pull-package <name>
  vnbox store save-push <name> | save-pull <name> [時刻]
  vnbox capture begin <name> / capture end <name>     手動インストールの差分をパッケージ化
  vnbox detect <フォルダ|exe>                          エンジン・インストーラ・依存の判定 (JSON)
  vnbox extract <アーカイブ> <出力先>
  vnbox regdiff diff <before> <after> <out> | apply <frag> <target> | show <frag>

共通: -wine vnbox|classic (既定 vnbox。環境変数 VNBOX_FLAVOR)、データの置き場所は $VNBOX (既定 ~/.local/share/vnbox)
S3: VNBOX_S3=s3://bucket/prefix, S3_ENDPOINT (R2/MinIO 等), VNBOX_USER, 認証は AWS 標準の環境変数/プロファイル
    (config/store.conf にも書ける)。トークンは -token か VNBOX_TOKEN。
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
	case "browser":
		err = cmdBrowser(args)
	case "prepare":
		err = cmdPrepare(ctx, args)
	case "build-wine":
		err = cmdBuildWine(ctx, args)
	case "install-wine":
		err = cmdInstallWine(ctx)
	case "mkbase":
		err = cmdMkbase(ctx, args)
	case "probe":
		err = cmdProbe(ctx, args)
	case "verify":
		err = cmdVerify(ctx, args)
	case "ledger":
		err = cmdLedger(args)
	case "candidates":
		err = cmdCandidates(ctx, args)
	case "stop-all":
		err = cmdStopAll()
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

// ready makes sure the Wine to use is there (the patched one is downloaded from the
// releases the first time) and makes its base prefix if needed.
func (c common) ready(ctx context.Context) error {
	if c.Flavor() == wine.Vnbox && c.Flavor().Installed() != nil && os.Getenv("VNBOX_WINE") == "" {
		fmt.Fprintf(os.Stderr, "downloading the patched Wine (%s, first time only)\n", winebuild.Tag())
		if err := winebuild.Install(ctx, os.Stderr); err != nil {
			return err
		}
	}
	if err := c.Flavor().Installed(); err != nil {
		return err
	}
	if c.Flavor() == wine.Vnbox && winebuild.Outdated() {
		log.Printf("note: a newer patched Wine is out (%s): vnbox install-wine", winebuild.Tag())
	}
	return base.Ensure(ctx, c.Flavor(), os.Stderr)
}

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
	if err := c.ready(ctx); err != nil {
		return err
	}
	var clicks [][2]int
	for _, pt := range strings.Split(*clickFlag, ";") {
		var x, y int
		if _, err := fmt.Sscanf(strings.TrimSpace(pt), "%d,%d", &x, &y); err == nil {
			clicks = append(clicks, [2]int{x, y})
		}
	}
	p, l, err := addPackage(ctx, pos[0], pos[1], addOptions{Flavor: c.Flavor(), Exe: *exe, Title: *title, Clicks: clicks, Log: os.Stderr})
	if err != nil {
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

// addOptions are the settings of addPackage.
type addOptions struct {
	Flavor wine.Flavor
	Exe    string   // start exe relative to the game folder ("" = guess)
	Title  string   // gallery title
	Clicks [][2]int // clicks on the installer's first screen
	Log    io.Writer
}

// addPackage turns a distribution file into packages/<name> (vnbox add without the upload):
// add.Add, then the launch fixes that depend on the engine (start the game behind BISHOP's
// launcher, DXVK for Unity 6, Chromium flags for NW.js / Electron, ...) and the DRM traces.
func addPackage(ctx context.Context, name, src string, o addOptions) (*pkg.Package, pkg.Launch, error) {
	p, err := add.Add(ctx, name, src, add.Options{Flavor: o.Flavor, Exe: o.Exe, Clicks: o.Clicks, Log: o.Log})
	if err != nil {
		return nil, pkg.Launch{}, err
	}
	l, _ := p.Launch()
	// BISHOP の起動ランチャー (startup.exe) は作品によって「ゲーム開始」で C++ 例外を出して終わる
	// (隷従の制服 体験版)。startup.ini に書かれたゲーム本体を直接起動する
	if o.Exe == "" {
		if t := bishopGame(filepath.Join(p.Dir, "files", filepath.FromSlash(l.Target))); t != "" {
			l.Target = path.Join(path.Dir(l.Target), t)
		}
	}
	// gallery metadata: the title given and the engine of the installed game folder
	l.Title = o.Title
	gdir := filepath.Join(p.Dir, "files", filepath.Dir(filepath.FromSlash(l.Target)))
	det := detect.Detect(gdir)
	if e := det.Engine; l.Engine == "" && e != "" && e != "unknown" {
		l.Engine = e
	}
	// .NET 5+ のアプリ (apphost): ランタイムを prefix に入れる
	if v := dotnetVerb(filepath.Join(p.Dir, "files", filepath.FromSlash(l.Target))); v != "" && !slices.Contains(l.Winetricks, v) {
		l.Winetricks = append(l.Winetricks, v)
	}
	// Unity 6 (6000.x) の Direct3D 11 描画は wined3d では黒画面のまま。DXVK (Vulkan) で描ける
	if unity6(filepath.Join(p.Dir, "files", filepath.FromSlash(l.Target))) && !slices.Contains(l.Winetricks, "vnbox-dxvk") {
		l.Winetricks = append(l.Winetricks, "vnbox-dxvk")
	}
	// Unreal Engine 4 の Direct3D 11 描画は wined3d だとロック待ちで止まる (真依と秘密のマッサージ屋さん、
	// 小さな侵入者、Bondage Sandbox)。DXVK だと描ける。UE5 は wined3d のままで動いた (新人アイドル、枕営業します。)
	if unreal4(filepath.Join(p.Dir, "files", filepath.FromSlash(l.Target))) && !slices.Contains(l.Winetricks, "vnbox-dxvk") {
		l.Winetricks = append(l.Winetricks, "vnbox-dxvk")
	}
	// SakanaGL: WM (openbox) がウィンドウを管理すると全面が黒いまま。Wine 自身に管理させると描ける
	// (でか乳巫女と巨乳シスター)
	if l.Engine == "SakanaGL" {
		l.Unmanaged = true
	}
	// Godot 4: プロジェクトが Direct3D 12 を指定していると、Wine 内蔵の vkd3d が DXIL を読めずに落ちる
	// (魔王なのにスライムしか作れない 体験版)。Vulkan (lavapipe) で描かせる
	if l.Args == "" && godot4(filepath.Join(p.Dir, "files", filepath.FromSlash(l.Target))) {
		l.Args = "--rendering-driver vulkan"
	}
	// NW.js (RPG ツクール MV/MZ 等) も Chromium なので同じ: 一部の作品は GPU プロセス無しでは真っ黒のまま
	// (淫らに染めて 体験版、RPG ツクール MZ)
	if l.Args == "" {
		if _, err := os.Stat(filepath.Join(gdir, "nw.dll")); err == nil {
			l.Args = electronArgs
		}
	}
	var drm []string
	for _, t := range det.Tags {
		// Electron (TyranoScript v5 等): GPU プロセスが Wine + GPU なしで起動できず WebGL が使えないので、
		// GPU 処理を本体プロセスに入れ、ソフトウェア描画 (SwiftShader) を使わせる
		if t == "Runtime:Electron" && l.Args == "" {
			l.Args = electronArgs
		}
		// XAML Islands (C++/WinRT の Windows.UI.Xaml): Wine に実装が無いので何もしない代用品を入れる
		// SharpDX の Direct3D 11 (MonoGame WindowsDX 等): wined3d では画面が単色のまま。DXVK (Vulkan) で描ける
		if t == "Runtime:SharpDX11" && !slices.Contains(l.Winetricks, "vnbox-dxvk") {
			l.Winetricks = append(l.Winetricks, "vnbox-dxvk")
		}
		// VC\Runtimes の版と msvcp140_2.dll の版を確かめてから起動するもの (Unreal Engine 5 等): 最新の VC++ ランタイムを入れる
		if t == "Runtime:VCRuntimeCheck" && !slices.Contains(l.Winetricks, "vnbox-vcrun") {
			l.Winetricks = append(l.Winetricks, "vnbox-vcrun")
		}
		if t == "Runtime:XamlIslands" && !slices.Contains(l.Winetricks, "vnbox-xamlstub") {
			l.Winetricks = append(l.Winetricks, "vnbox-xamlstub")
		}
		// MFC 4.2 (VC++ 6) を使うのに同梱していないもの (魔女恋にっき DMM 版): winetricks で入れる
		if t == "Runtime:MFC42" && !slices.Contains(l.Winetricks, "mfc42") && !hasFile(gdir, "mfc42.dll") {
			l.Winetricks = append(l.Winetricks, "mfc42")
		}
		// ソフト電池: ランタイム (同梱) を入れ、オンライン認証のためネットワークを許す
		if t == "DRM:SoftDenchi" && !slices.Contains(l.Winetricks, "vnbox-sdrt") {
			l.Winetricks = append(l.Winetricks, "vnbox-sdrt")
			l.Network = true
		}
		if strings.HasPrefix(t, "DRM:") || strings.HasPrefix(t, "Protect:") {
			drm = append(drm, t)
		}
	}
	if len(drm) > 0 {
		l.DRM = strings.Join(drm, ",")
		fmt.Fprintf(o.Log, "warning: DRM / protection found: %s (purchased copies may need online activation)\n", l.DRM)
	}
	// vnrun が動かせるエンジンで DRM が無ければ、ブラウザでも遊べるようにする (Wine でも今まで通り動く)。
	// DRM 付きの作品は Wine で正規の認証を通して遊ぶ
	if l.DRM == "" && webplay.Playable(l.Engine, gdir) {
		l.Runner = pkg.RunnerVnrun
		l.Root = path.Dir(l.Target)
		fmt.Fprintf(o.Log, "[%s] %s: can also run in the browser (vnrun)\n", name, l.Engine)
	}
	if err := pkg.WriteLaunch(p.Dir, l); err != nil {
		return nil, pkg.Launch{}, err
	}
	return p, l, nil
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
	kbps := c.fs.Int("bitrate", 3000, "WebRTC: 映像の上限 kbit/s (視聴者の回線に合わせてここから下げる)")
	enc := c.fs.String("encoder", envOr("VNBOX_ENCODER", "auto"), "H.264 エンコーダ: auto (GPU が使えれば VAAPI / NVENC) / vaapi / nvenc / x264")
	return func() error {
		e, ok := stream.ParseEncoder(*enc)
		if !ok {
			return fmt.Errorf("-encoder: %q is not auto, vaapi, nvenc or x264", *enc)
		}
		cfg := stream.RTCConfig{Port: *port, Bitrate: *kbps, Encoder: e}
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

// playerFlag adds -player, the built vnrun player that runs runner=vnrun packages in the browser.
func playerFlag(c common) *string {
	return c.fs.String("player", envOr("VNBOX_PLAYER", filepath.Join(wine.Root(), "player")),
		"vnrun のプレイヤー (vnrun の dist/)。runner=vnrun のパッケージをブラウザで動かす (環境変数 VNBOX_PLAYER)")
}

// cmdBrowser marks already added packages that vnrun can run (engine supported, no DRM) as
// playable in the browser. New packages get this from add.
func cmdBrowser(args []string) error {
	c := newFlags("browser")
	all := c.fs.Bool("all", false, "every package")
	names := c.parse(args)
	if *all {
		ents, err := os.ReadDir(filepath.Join(wine.Root(), "packages"))
		if err != nil {
			return err
		}
		names = nil
		for _, e := range ents {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return errors.New("usage: vnbox browser [-all | <name>...]")
	}
	for _, n := range names {
		p, err := pkg.Open(pkg.Dir(n))
		if err != nil {
			continue
		}
		l, err := p.Launch()
		if err != nil || l.InBrowser() || l.DRM != "" || l.Target == "" {
			continue
		}
		if !webplay.Playable(l.Engine, filepath.Join(p.Dir, "files", filepath.Dir(filepath.FromSlash(l.Target)))) {
			continue
		}
		l.Runner, l.Root = pkg.RunnerVnrun, path.Dir(l.Target)
		if err := pkg.WriteLaunch(p.Dir, l); err != nil {
			return err
		}
		fmt.Printf("%s: %s, playable in the browser\n", n, l.Engine)
	}
	return nil
}

func cmdServe(ctx context.Context, args []string) error {
	c := newFlags("serve")
	addr := c.fs.String("addr", ":8080", "listen address")
	tok := c.fs.String("token", "", "access token (or VNBOX_TOKEN)")
	fps := c.fs.Int("fps", 30, "video frame rate (below 24 means 30)")
	player := playerFlag(c)
	rtc := rtcFlags(c)
	c.parse(args)
	if err := rtc(); err != nil {
		return err
	}
	if err := c.ready(ctx); err != nil {
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
	p.Web = webplay.New(webplay.Options{PlayerDir: *player, Store: opt.Store})
	if !p.Web.Ready() {
		log.Printf("browser play (vnrun) is off: no player in %s", *player)
	}
	log.Printf("portal on http://%s/%s", hostAddr(*addr), tokenQuery(t))
	return listen(ctx, *addr, p.Handler(), p.Shutdown)
}

func cmdPlay(ctx context.Context, args []string) error {
	c := newFlags("play")
	addr := c.fs.String("addr", ":8080", "listen address")
	tok := c.fs.String("token", "", "access token (or VNBOX_TOKEN)")
	fps := c.fs.Int("fps", 30, "video frame rate (below 24 means 30)")
	rtc := rtcFlags(c)
	pos := c.parse(args)
	if len(pos) < 1 {
		return errors.New("usage: vnbox play <name>")
	}
	if err := c.ready(ctx); err != nil {
		return err
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

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
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
	dir := c.fs.String("prefix", "", "prefix to prepare (default prefixes/run-<name>)")
	pos := c.parse(args)
	if len(pos) < 1 {
		return errors.New("usage: vnbox prepare <name> [-prefix dir]")
	}
	p, err := pkg.Open(pkg.Dir(pos[0]))
	if err != nil {
		return err
	}
	prefix := session.Prefix(pos[0])
	if *dir != "" {
		if prefix, err = filepath.Abs(*dir); err != nil {
			return err
		}
	}
	l, err := p.Launch()
	if err != nil {
		return err
	}
	display := wine.FreeDisplay("prepare-" + pos[0])
	defer wine.StopX(display)
	if err := p.Prepare(ctx, l.WineFlavor(c.Flavor()), prefix, display); err != nil {
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

// dotnetVerb reads <exe>.runtimeconfig.json and returns "vnbox-dotnet:<major>:<arch>[:desktop]", or "".
// bishopGame returns the game exe named by the BISHOP launcher's startup.ini ([EXE] Boot= or
// Demo=) when exe is that launcher and the file exists, else "".
func bishopGame(exe string) string {
	if !strings.EqualFold(filepath.Base(exe), "startup.exe") {
		return ""
	}
	var b []byte
	ents, _ := os.ReadDir(filepath.Dir(exe))
	for _, e := range ents {
		if strings.EqualFold(e.Name(), "startup.ini") { // StartUp.ini in some titles
			b, _ = os.ReadFile(filepath.Join(filepath.Dir(exe), e.Name()))
		}
	}
	if b == nil {
		return ""
	}
	sect := ""
	var found []string
	for _, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "[") {
			sect = strings.ToUpper(strings.Trim(ln, "[]"))
			continue
		}
		k, v, ok := strings.Cut(ln, "=")
		if !ok || sect != "EXE" || strings.HasPrefix(ln, ";") {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(v), ".\\"), "./")
		if (k == "boot" || k == "demo") && v != "" && !strings.ContainsAny(v, "\\/") {
			for _, e := range ents {
				if strings.EqualFold(e.Name(), v) {
					found = append(found, e.Name())
				}
			}
		}
	}
	// the game exits at once without the settings file the launcher writes on ゲーム開始
	// ([StartUp] ConfigName=, e.g. bsxx.ini): keep the launcher if the trial does not ship one
	if cfg := iniValue(string(b), "StartUp", "ConfigName"); cfg != "" {
		have := false
		for _, e := range ents {
			have = have || strings.EqualFold(e.Name(), cfg)
		}
		if !have {
			return ""
		}
	}
	if len(found) > 0 {
		return found[0]
	}
	return ""
}

// iniValue returns key in [section] of an ini text ("" if absent; ; comments are skipped).
func iniValue(text, section, key string) string {
	sect := ""
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "[") {
			sect = strings.Trim(ln, "[]")
			continue
		}
		if k, v, ok := strings.Cut(ln, "="); ok && !strings.HasPrefix(ln, ";") && strings.EqualFold(sect, section) && strings.EqualFold(strings.TrimSpace(k), key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// godot4 reports whether exe is a Godot 4 export (it knows the --rendering-driver option).
func godot4(exe string) bool {
	b, err := os.ReadFile(exe)
	return err == nil && bytes.Contains(b, []byte("Godot Engine")) && bytes.Contains(b, []byte("--rendering-driver"))
}

// unreal4 reports whether exe is an Unreal Engine 4 game: the <project>-Win64-Shipping.exe
// itself, or the small launcher next to <project>/Binaries/Win64/<project>-Win64-Shipping.exe.
func unreal4(exe string) bool {
	ship := []string{exe}
	if !strings.HasSuffix(strings.ToLower(exe), "-shipping.exe") {
		ship, _ = filepath.Glob(filepath.Join(filepath.Dir(exe), "*", "Binaries", "Win64", "*-Shipping.exe"))
	}
	for _, f := range ship {
		if b, err := os.ReadFile(f); err == nil && bytes.Contains(b, []byte("++UE4+Release-4.")) {
			return true
		}
	}
	return false
}

// unity6 reports whether the UnityPlayer.dll next to exe is from Unity 6 (version 6000.x).
func unity6(exe string) bool {
	ents, _ := os.ReadDir(filepath.Dir(exe))
	for _, e := range ents {
		if strings.EqualFold(e.Name(), "UnityPlayer.dll") { // some installers write UNITYPLAYER.DLL
			b, err := os.ReadFile(filepath.Join(filepath.Dir(exe), e.Name()))
			return err == nil && unityVersionRe.Match(b)
		}
	}
	return false
}

var unityVersionRe = regexp.MustCompile(`6000\.\d+\.\d+[abfp]\d+`)

func dotnetVerb(exe string) string {
	b, err := os.ReadFile(strings.TrimSuffix(exe, filepath.Ext(exe)) + ".runtimeconfig.json")
	if err != nil {
		return ""
	}
	var rc struct {
		RuntimeOptions struct {
			Tfm        string `json:"tfm"`
			Framework  struct{ Name, Version string }
			Frameworks []struct{ Name, Version string }
		} `json:"runtimeOptions"`
	}
	if json.Unmarshal(b, &rc) != nil {
		return ""
	}
	fws := append(rc.RuntimeOptions.Frameworks, rc.RuntimeOptions.Framework)
	major, desktop := "", false
	for _, f := range fws {
		if f.Version != "" && major == "" {
			major, _, _ = strings.Cut(f.Version, ".")
		}
		desktop = desktop || f.Name == "Microsoft.WindowsDesktop.App"
	}
	if major == "" {
		major = strings.TrimPrefix(strings.SplitN(rc.RuntimeOptions.Tfm, ".", 2)[0], "net")
	}
	if major == "" {
		return ""
	}
	arch := "x86"
	if detect.PEArch(exe) == "x64" {
		arch = "x64"
	}
	v := "vnbox-dotnet:" + major + ":" + arch
	if desktop {
		v += ":desktop"
	}
	return v
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

func cmdBuildWine(ctx context.Context, args []string) error {
	c := newFlags("build-wine")
	jobs := c.fs.Int("j", 0, "make -j (default: number of CPUs)")
	prefix := c.fs.String("prefix", "", "install there instead of <data>/wine (for packaging)")
	tag := c.fs.Bool("tag", false, "only print the release tag and file name of this Wine")
	c.parse(args)
	if *tag {
		fmt.Println(winebuild.Tag(), winebuild.Asset())
		return nil
	}
	if err := winebuild.Build(ctx, winebuild.Options{Jobs: *jobs, Prefix: *prefix, Log: os.Stderr}); err != nil {
		return err
	}
	if *prefix != "" {
		fmt.Println("installed:", *prefix)
	} else {
		fmt.Println("installed:", wine.Vnbox.Bin())
	}
	return nil
}

func cmdInstallWine(ctx context.Context) error {
	if err := winebuild.Install(ctx, os.Stderr); err != nil {
		return err
	}
	fmt.Println("installed:", wine.Vnbox.Bin(), "("+winebuild.Tag()+")")
	return nil
}

// hasFile reports whether dir (or a folder below it) holds a file of this name (case ignored).
func hasFile(dir, name string) bool {
	found := false
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.EqualFold(d.Name(), name) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

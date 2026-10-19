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
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"vnbox/internal/add"
	"vnbox/internal/capture"
	"vnbox/internal/detect"
	"vnbox/internal/extract"
	"vnbox/internal/pkg"
	"vnbox/internal/portal"
	"vnbox/internal/regdiff"
	"vnbox/internal/session"
	"vnbox/internal/store"
	"vnbox/internal/wine"
)

const usage = `vnbox — Windows ノベルゲームを Wine で動かしてブラウザ配信する

  vnbox add <name> <配布物> [-exe 相対パス] [-upload]   zip/lzh/7z/rar/SFX/インストーラ/フォルダからパッケージを作る
  vnbox serve [-addr :8080] [-token T]                ポータル: 一覧から選んで起動・配信 (複数同時)
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
	up := c.fs.Bool("upload", false, "S3 にも上げる (VNBOX_S3 が設定されていれば既定で上げる)")
	noUp := c.fs.Bool("no-upload", false, "S3 に上げない")
	pos := c.parse(args)
	if len(pos) < 2 {
		return errors.New("usage: vnbox add <name> <配布物>")
	}
	p, err := add.Add(ctx, pos[0], pos[1], add.Options{Flavor: c.Flavor(), Exe: *exe, Log: os.Stderr})
	if err != nil {
		return err
	}
	l, _ := p.Launch()
	fmt.Printf("package: %s\ntarget:  %s\n", p.Dir, l.Target)
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

func cmdServe(ctx context.Context, args []string) error {
	c := newFlags("serve")
	addr := c.fs.String("addr", ":8080", "listen address")
	tok := c.fs.String("token", "", "access token (or VNBOX_TOKEN)")
	fps := c.fs.Int("fps", 15, "video frame rate")
	c.parse(args)
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
	fps := c.fs.Int("fps", 15, "video frame rate")
	pos := c.parse(args)
	if len(pos) < 1 {
		return errors.New("usage: vnbox play <name>")
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
	if err := p.Prepare(ctx, c.Flavor(), prefix); err != nil {
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

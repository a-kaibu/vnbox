package session

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/a-kaibu/vnbox/internal/wine"
	"github.com/a-kaibu/vnbox/internal/x11"
)

// Login screens of DRM runtimes that vnbox fills in (window title -> account in
// <data>/config/accounts.conf).
var loginWindows = map[string]string{
	"ユーザー認証": "dmm", // ソフト電池 (DMM 版): DMM のユーザー ID / メールアドレスとパスワード
}

// account reads <data>/config/accounts.conf:
//
//	dmm.id=you@example.com
//	dmm.password=...
//
// The file stays on this machine (never in the repository or the S3 store).
func account(name string) (id, password string) {
	f, err := os.Open(filepath.Join(wine.Root(), "config", "accounts.conf"))
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		switch strings.TrimSpace(k) {
		case name + ".id":
			id = strings.TrimSpace(v)
		case name + ".password":
			password = strings.TrimSpace(v)
		}
	}
	return id, password
}

// autoLogin watches the first minutes of the game for a DRM login screen and fills in the
// account from accounts.conf (tools/fillauth). It tries each screen twice at most, so a
// wrong password ends with the screen left for the player.
func (s *Session) autoLogin(ctx context.Context) {
	tries := map[string]int{}
	end := time.Now().Add(5 * time.Minute)
	for time.Now().Before(end) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
		for _, w := range x11.Display(s.Display).Windows() {
			acct, ok := loginWindows[w.Name]
			if !ok || tries[w.Name] >= 2 {
				continue
			}
			id, pass := account(acct)
			if id == "" || pass == "" {
				return
			}
			tries[w.Name]++
			exe, err := wine.Tool("fillauth")
			if err != nil {
				fmt.Fprintf(s.opt.Log, "[%s] auto login: %v\n", s.Name, err)
				return
			}
			out, err := s.env.Command(ctx, "", exe, w.Name, id, pass).Output()
			fmt.Fprintf(s.opt.Log, "[%s] auto login (%s): %s %v\n", s.Name, acct, strings.TrimSpace(string(out)), err)
			time.Sleep(10 * time.Second) // the online check
		}
	}
}

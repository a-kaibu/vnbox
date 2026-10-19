package store

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"vnbox/internal/pkg"
)

// startMoto runs a local S3-compatible server (moto) for the test, or skips.
func startMoto(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("MOTO_SERVER")
	if bin == "" {
		bin = "/opt/s3tools/bin/moto_server"
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skip("moto_server not available")
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	cmd := exec.Command(bin, "-p", fmt.Sprint(port))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	for i := 0; i < 100; i++ {
		if c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			c.Close()
			return url
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("moto did not start")
	return ""
}

func setup(t *testing.T) (*Store, string) {
	endpoint := startMoto(t)
	root := t.TempDir()
	t.Setenv("VNBOX", root)
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_REGION", "us-east-1")
	ctx := context.Background()
	st, err := Open(ctx, Config{URL: "s3://bkt/vnbox", Endpoint: endpoint, User: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.s3.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &st.bucket}); err != nil {
		t.Fatal(err)
	}
	return st, root
}

func TestPackageRoundTrip(t *testing.T) {
	st, root := setup(t)
	ctx := context.Background()
	dir := filepath.Join(root, "packages", "demo")
	os.MkdirAll(filepath.Join(dir, "files", "Games", "demo"), 0o755)
	pkg.WriteLaunch(dir, pkg.Launch{Target: "Games/demo/game.exe", Title: "デモ 体験版", Engine: "BGI"})
	os.WriteFile(filepath.Join(dir, pkg.ThumbFile), []byte("\xff\xd8jpeg\xff\xd9"), 0o644)
	big := strings.Repeat("ノベル", 400000) // > 5 MB to exercise multipart upload
	os.WriteFile(filepath.Join(dir, "files", "Games", "demo", "game.exe"), []byte(big), 0o644)
	p, _ := pkg.Open(dir)
	if err := st.PushPackage(ctx, p); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(dir)
	p2, err := st.PullPackage(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(p2.Dir, "files", "Games", "demo", "game.exe"))
	if string(b) != big {
		t.Fatal("package content differs after round trip")
	}
	l, _ := st.List(ctx)
	if len(l.Packages) != 1 || l.Packages[0].Name != "demo" {
		t.Fatalf("list: %+v", l)
	}
	ini, err := st.PackageFile(ctx, "demo", "ini")
	if err != nil {
		t.Fatal(err)
	}
	if ln, _ := pkg.ParseLaunch(bytes.NewReader(ini)); ln.Title != "デモ 体験版" || ln.Engine != "BGI" {
		t.Fatalf("remote ini: %+v", ln)
	}
	if j, err := st.PackageFile(ctx, "demo", "jpg"); err != nil || string(j) != "\xff\xd8jpeg\xff\xd9" {
		t.Fatalf("thumb: %q %v", j, err)
	}
}

// fakePrefix builds a prepared prefix without running Wine.
func fakePrefix(t *testing.T, root string) string {
	p := filepath.Join(root, "prefixes", "run-demo")
	game := filepath.Join(p, "drive_c", "Games", "demo")
	os.MkdirAll(game, 0o755)
	os.MkdirAll(filepath.Join(p, "drive_c", "windows"), 0o755)
	reg := "WINE REGISTRY Version 2\n\n[Software\\\\Wine] 1\n\"Version\"=\"win10\"\n"
	os.WriteFile(filepath.Join(p, "user.reg"), []byte(reg), 0o644)
	os.WriteFile(filepath.Join(p, "system.reg"), []byte(reg), 0o644)
	os.WriteFile(filepath.Join(game, "game.exe"), []byte("MZ"), 0o644)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(game, "game.exe"), old, old) // package files predate the marker
	if err := pkg.Snapshot(p); err != nil {
		t.Fatal(err)
	}
	marker := time.Now().Add(-time.Minute)
	os.Chtimes(filepath.Join(p, pkg.AppliedMarker), marker, marker)
	return p
}

func TestSaveCycle(t *testing.T) {
	st, root := setup(t)
	ctx := context.Background()
	p := fakePrefix(t, root)
	game := filepath.Join(p, "drive_c", "Games", "demo")
	// the game writes a save, a config under AppData and a log; Windows files are ignored
	os.WriteFile(filepath.Join(game, "save01.dat"), []byte("slot1"), 0o644)
	os.MkdirAll(filepath.Join(p, "drive_c", "users", "u", "AppData"), 0o755)
	os.WriteFile(filepath.Join(p, "drive_c", "users", "u", "AppData", "cfg.ini"), []byte("vol=7"), 0o644)
	os.WriteFile(filepath.Join(game, "debug.log"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(p, "drive_c", "windows", "win.ini"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(p, "user.reg"), []byte("WINE REGISTRY Version 2\n\n[Software\\\\Wine] 1\n\"Version\"=\"win10\"\n\n[Software\\\\Brand\\\\Demo] 2\n\"Volume\"=dword:0000004d\n"), 0o644)

	files, _ := SaveFiles(p)
	if strings.Join(files, ",") != "Games/demo/save01.dat,users/u/AppData/cfg.ini" {
		t.Fatalf("save files: %v", files)
	}
	r, err := st.SavePush(ctx, "demo", p)
	if err != nil || r.Unchanged || r.Files != 2 {
		t.Fatalf("push: %+v %v", r, err)
	}
	first := r.Timestamp
	if r, _ := st.SavePush(ctx, "demo", p); !r.Unchanged {
		t.Error("an unchanged save must not be uploaded again")
	}

	// a brand-new prefix gets the save back, including the registry change
	os.RemoveAll(p)
	p = fakePrefix(t, root)
	game = filepath.Join(p, "drive_c", "Games", "demo")
	found, n, err := st.SavePull(ctx, "demo", p, "")
	if err != nil || !found || n != 2 {
		t.Fatalf("pull: %v %v %v", found, n, err)
	}
	if b, _ := os.ReadFile(filepath.Join(game, "save01.dat")); string(b) != "slot1" {
		t.Fatal("save file not restored")
	}
	if b, _ := os.ReadFile(filepath.Join(p, "user.reg")); !strings.Contains(string(b), "dword:0000004d") {
		t.Fatal("registry not restored")
	}

	// restored files keep their old mtime but must stay in the next save
	time.Sleep(1100 * time.Millisecond)
	os.WriteFile(filepath.Join(game, "save01.dat"), []byte("slot2"), 0o644)
	r, err = st.SavePush(ctx, "demo", p)
	if err != nil || r.Files != 2 {
		t.Fatalf("second push: %+v %v", r, err)
	}
	h, _ := st.SaveHistory(ctx, "demo")
	if len(h) != 2 || h[0] != first {
		t.Fatalf("history: %v", h)
	}
	// roll back to the first save
	if _, _, err := st.SavePull(ctx, "demo", p, first); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(game, "save01.dat")); string(b) != "slot1" {
		t.Fatal("rollback failed")
	}
	if found, _, _ := st.SavePull(ctx, "nothing", p, ""); found {
		t.Error("missing save reported as found")
	}
}

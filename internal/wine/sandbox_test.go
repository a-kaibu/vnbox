package wine

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestSandbox(t *testing.T) {
	if !SandboxAvailable() {
		t.Skip("bubblewrap not installed")
	}
	prefix := t.TempDir()
	t.Setenv("AWS_SECRET_ACCESS_KEY", "do-not-leak")
	e := Env{Prefix: prefix, Sandbox: true}
	run := func(script string) string {
		t.Helper()
		out, err := e.command(context.Background(), prefix, "/bin/sh", "-c", script).CombinedOutput()
		if err != nil {
			if _, ok := err.(*exec.ExitError); !ok {
				t.Fatalf("%s: %v", script, err)
			}
		}
		return strings.TrimSpace(string(out))
	}
	// /home only holds the empty path down to the folders bound in (runtime/tools)
	if got := run("ls -A /root; ls -A " + Root() + " 2>/dev/null"); got != "runtime" && got != "" {
		t.Errorf("/root or the vnbox data visible in the sandbox: %q", got)
	}
	if got := run("ls -A " + Root() + "/runtime 2>/dev/null"); got != "tools" && got != "" {
		t.Errorf("runtime/ visible in the sandbox: %q", got)
	}
	if got := run("env | grep AWS_ || true"); got != "" {
		t.Errorf("credentials leaked: %q", got)
	}
	if got := run("echo x > " + prefix + "/w && cat " + prefix + "/w"); got != "x" {
		t.Errorf("prefix not writable: %q", got)
	}
	if got := run("(echo x > /etc/vnbox-test) 2>/dev/null && echo written || echo denied"); got != "denied" {
		t.Errorf("system writable: %q", got)
		os.Remove("/etc/vnbox-test")
	}
	if got := run("cat /proc/net/dev | tail -n +3 | cut -d: -f1 | tr -d ' '"); got != "lo" {
		t.Errorf("network interfaces in the sandbox: %q", got)
	}
	e.Network = true
	if got := run("cat /proc/net/dev | tail -n +3 | wc -l"); got == "1" {
		t.Errorf("Network: only loopback")
	}
}

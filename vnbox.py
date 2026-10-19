#!/usr/bin/env python3
"""vnbox: Windows ノベルゲームを Wine で動かし、ウィンドウ単位でブラウザ配信するためのツール群の入口

  vnbox.py list                         レシピ一覧と状態
  vnbox.py detect <dir|exe>             エンジン / インストーラ / DRM 判定
  vnbox.py test <recipe> [...]          レシピの操作列で自動起動し、スクショを results/<recipe>/ に保存
  vnbox.py test --all                   全レシピを回帰テスト (status=broken は除く)
  vnbox.py play <recipe> [--port N]     ゲームを起動してブラウザ配信 (映像+音声+入力)
  vnbox.py capture-begin <name>         インストーラ差分キャプチャ開始 (その後 WINEPREFIX=prefixes/<name> wine setup.exe)
  vnbox.py capture-end <name>           差分を packages/<name>/ に書き出す
"""
import configparser, os, subprocess, sys
ROOT = os.path.dirname(os.path.abspath(__file__)); S = os.path.join(ROOT, "scripts")

def recipe(name):
    c = configparser.ConfigParser(interpolation=None); c.optionxform = str
    c.read(os.path.join(ROOT, "recipes", name + ".ini"), encoding="utf-8")
    return c

def target(c):
    """(作業ディレクトリ, exe, args, パッケージか) を返す"""
    L = c["launch"]; local = os.path.join(ROOT, L.get("local", ""))
    if local.rstrip("/").split("/")[-2] == "packages" or os.path.exists(os.path.join(local, "launch.ini")):
        return local, None, L.get("args", ""), True
    return os.path.join(local, L.get("dir", "")), L["exe"], L.get("args", ""), False

def prepare(name, c):
    """レシピの prefix 設定 (winver / renderer / verbs) を適用した prefix を用意する"""
    d, exe, args, is_pkg = target(c)
    P = c["prefix"] if c.has_section("prefix") else {}
    env = dict(os.environ)
    if is_pkg:
        env["PREPARE_ONLY"] = "1"
        prefix = subprocess.run([os.path.join(S, "launch.sh"), d], env=env, capture_output=True, text=True).stdout.strip()
        t = open(os.path.join(d, "launch.ini"), encoding="utf-8").read().split("target=")[1].split("\n")[0]
        d = os.path.join(prefix, "drive_c", os.path.dirname(t)); exe = os.path.basename(t)
    else:
        prefix = os.path.join(ROOT, "prefixes", name)
    return d, exe, args, prefix, P

def apply_prefix_opts(prefix, P, arch_hint):
    if not os.path.isdir(prefix): return
    env = dict(os.environ, WINEPREFIX=prefix, WINEARCH="win64" if "#arch=win64" in open(os.path.join(prefix, "system.reg")).read(2000) else "win32", WINEDEBUG="-all")
    def reg(*a): subprocess.run(["wine", "reg", "add", *a, "/f"], env=env, capture_output=True)
    if P.get("winver"): reg("HKCU\\Software\\Wine", "/v", "Version", "/d", P["winver"])
    if P.get("renderer"): reg("HKCU\\Software\\Wine\\Direct3D", "/v", "renderer", "/d", P["renderer"])
    for v in P.get("verbs", "").split():
        sh = os.path.join(S, "verbs", v + ".sh")
        subprocess.run([sh, prefix] if os.path.exists(sh) else ["winetricks", "-q", v], env=env)
    subprocess.run(["wineserver", "-w"], env=env)

def cmd_test(name):
    c = recipe(name); d, exe, args, prefix, P = prepare(name, c)
    if not os.path.isdir(prefix):  # run.sh に複製させてから設定を当てる
        subprocess.run([os.path.join(S, "run.sh"), name, d, exe, "1"], env=dict(os.environ, INPUT="sleep:0", ARGS=args, WINEPREFIX_OVERRIDE=prefix), capture_output=True)
    apply_prefix_opts(prefix, P, None)
    inp = c.get("test", "input", fallback="shot:t20 sleep:15 shot:t35")
    env = dict(os.environ, INPUT=inp, ARGS=args, WINEPREFIX_OVERRIDE=prefix)
    if P.get("xvfb"): env["XVFB_SIZE"] = P["xvfb"]   # 画面サイズに合わせて自動拡大するゲームはサイズ固定
    out = subprocess.run([os.path.join(S, "run.sh"), name, d, exe, c.get("test", "wait", fallback="25")], env=env, capture_output=True, text=True).stdout
    alive = "alive_after=yes" in out
    # 最後のスクショが単色 (真っ黒/真っ白/固まった画面) でないこと
    shots = sorted(f for f in os.listdir(os.path.join(ROOT, "results", name)) if f.endswith(".png") and f[:2].isdigit())
    sd = 0.0
    if shots:
        r = subprocess.run(["convert", os.path.join(ROOT, "results", name, shots[-1]), "-colorspace", "gray", "-format", "%[fx:standard_deviation]", "info:"], capture_output=True, text=True)
        try: sd = float(r.stdout.strip() or 0)
        except ValueError: sd = 0.0
    ok = alive and sd > 0.03
    print(f"{'OK ' if ok else 'NG '} {name:22s} alive={'y' if alive else 'n'} contrast={sd:.3f} last={shots[-1] if shots else '-'}  [{c.get('result','status',fallback='?')}]", flush=True)
    return ok

def main():
    if len(sys.argv) < 2: print(__doc__); return
    cmd, a = sys.argv[1], sys.argv[2:]
    if cmd == "list":
        for f in sorted(os.listdir(os.path.join(ROOT, "recipes"))):
            c = recipe(f[:-4]); print(f"{f[:-4]:22s} {c.get('game','engine',fallback=''):42.42s} {c.get('result','status',fallback='')}")
    elif cmd == "detect":
        subprocess.run([sys.executable, "-I", os.path.join(S, "detect.py"), a[0]])
    elif cmd == "test":
        names = [f[:-4] for f in sorted(os.listdir(os.path.join(ROOT, "recipes")))] if a == ["--all"] else a
        res = [cmd_test(n) for n in names if not recipe(n).get("result", "status", fallback="").startswith("broken")]
        print(f"{sum(res)}/{len(res)} alive")
    elif cmd == "play":
        name = a[0]; port = a[a.index("--port") + 1] if "--port" in a else "8080"
        c = recipe(name); d, exe, args, is_pkg = target(c)
        if c.has_section("prefix") and c["prefix"].get("xvfb"): os.environ["XVFB_SIZE"] = c["prefix"]["xvfb"]
        if is_pkg: os.execv(os.path.join(S, "play.sh"), ["play.sh", "--package", d, port])
        os.environ["ARGS"] = args
        os.execv(os.path.join(S, "play.sh"), ["play.sh", name, d, exe, port])
    elif cmd == "capture-begin": subprocess.run([os.path.join(S, "capture.sh"), "begin", a[0]])
    elif cmd == "capture-end": subprocess.run([os.path.join(S, "capture.sh"), "end", a[0]])
    else: print(__doc__)

if __name__ == "__main__":
    main()

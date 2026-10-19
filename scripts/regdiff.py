#!/usr/bin/env python3
"""Wine の system.reg / user.reg を直接 diff / merge する。
  regdiff.py diff  <before.reg> <after.reg> <out.reg>   追加・変更されたキー/値だけを Wine 形式で書き出す
  regdiff.py apply <fragment.reg> <target.reg>          fragment を target にマージ (wineserver 停止中に実行)
  regdiff.py show  <fragment.reg>                       人が読める形で表示
"""
import re, sys
from collections import OrderedDict

HDR = "WINE REGISTRY Version 2\n"
NOISE = [  # インストール前後で必ず変わる/関係ないキー
    r"^Software\\\\Wine\\\\(MSHTML|Mono|Debug)", r"\\\\Explorer\\\\(RecentDocs|UserAssist|ComDlg32|MountPoints2)",
    r"^System\\\\CurrentControlSet\\\\(Services\\\\Eventlog|Control\\\\DeviceClasses|Enum)",
    r"^Software\\\\Microsoft\\\\Windows\\\\CurrentVersion\\\\(Explorer\\\\Shell Folders|RunOnce)$",
    r"^Hardware\\\\", r"^Software\\\\Microsoft\\\\Cryptography\\\\RNG", r"\\\\Fonts$",
    r"^Volatile Environment", r"^Software\\\\Wine\\\\Fonts\\\\Cache",
    r"^Console$", r"\\\\TimeZoneInformation$", r"^Software\\\\Microsoft\\\\ActiveMovie\\\\devenum", r"\\\\MMDevices\\\\", r"^System\\\\ControlSet\d+\\\\Enum\\\\", r"^Software\\\\Wine\\\\(Drivers|MenuFiles|FileOpenAssociations)",
    r"^Software\\\\Microsoft\\\\(DirectX|Direct3D|DirectInput|DirectPlay|DirectDraw)\\\\Most ?Recent",
    r"^Software\\\\Microsoft\\\\Windows\\\\CurrentVersion\\\\Explorer\\\\(FileExts|Discardable)",
]

def parse(path):
    keys = OrderedDict(); cur = None
    with open(path, encoding="utf-8", errors="surrogateescape") as f:
        lines = f.read().split("\n")
    i = 0
    while i < len(lines):
        ln = lines[i]
        m = re.match(r"^\[(.*)\](?: \d+)?$", ln)
        if m:
            cur = m.group(1); keys.setdefault(cur, OrderedDict())
        elif cur is not None and ln and not ln.startswith(("#", ";")):
            while ln.endswith("\\") and i + 1 < len(lines):  # 継続行 (hex: の折返し)
                i += 1; ln = ln + "\n" + lines[i]
            vm = re.match(r'^(@|"(?:[^"\\]|\\.)*")=(.*)$', ln, re.S)
            if vm:
                keys[cur][vm.group(1)] = vm.group(2)
        elif cur is not None and ln.startswith("#"):
            keys[cur].setdefault("#meta", []).append(ln)
        i += 1
    return keys

def noisy(k):
    return any(re.search(p, k, re.I) for p in NOISE)

def diff(a, b):
    out = OrderedDict()
    for k, vals in b.items():
        if noisy(k): continue
        old = a.get(k)
        changed = OrderedDict((n, v) for n, v in vals.items()
                              if n != "#meta" and (old is None or old.get(n) != v))
        if old is None or changed:
            out[k] = changed
    return out

def write(keys, path):
    with open(path, "w", encoding="utf-8", errors="surrogateescape") as f:
        f.write(HDR + ";; vnbox registry fragment\n")
        for k, vals in keys.items():
            f.write(f"\n[{k}] 0\n")
            for n, v in vals.items():
                if n != "#meta": f.write(f"{n}={v}\n")

def apply(frag, target):
    t = parse(target); fr = parse(frag)
    for k, vals in fr.items():
        dst = t.setdefault(k, OrderedDict())
        for n, v in vals.items():
            if n != "#meta": dst[n] = v
    # 元ファイルのヘッダ行 (;; All keys relative to ... / #arch) を保持して書き戻す
    head = []
    with open(target, encoding="utf-8", errors="surrogateescape") as f:
        for ln in f:
            if ln.startswith("["): break
            head.append(ln)
    with open(target, "w", encoding="utf-8", errors="surrogateescape") as f:
        f.write("".join(head))
        for k, vals in t.items():
            f.write(f"\n[{k}] 0\n")
            for m in vals.get("#meta", []): f.write(m + "\n")
            for n, v in vals.items():
                if n != "#meta": f.write(f"{n}={v}\n")

def unescape(s):
    return re.sub(r"\\x([0-9a-fA-F]{1,4})", lambda m: chr(int(m.group(1), 16)), s)

if __name__ == "__main__":
    cmd = sys.argv[1]
    if cmd == "diff":
        d = diff(parse(sys.argv[2]), parse(sys.argv[3])); write(d, sys.argv[4])
        print(f"{len(d)} keys, {sum(len(v) for v in d.values())} values")
    elif cmd == "apply":
        apply(sys.argv[2], sys.argv[3])
    elif cmd == "show":
        for k, vals in parse(sys.argv[2]).items():
            print(f"[{unescape(k)}]")
            for n, v in vals.items():
                if n != "#meta": print(f"   {unescape(n)} = {unescape(v)[:120]}")

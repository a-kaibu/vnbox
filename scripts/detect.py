#!/usr/bin/env python3
"""ゲームフォルダ or exe からエンジン / インストーラ種別を推定する。
使い方: detect.py <dir or file>   -> JSON を出力"""
import json, os, re, sys

FILE_SIGS = [  # (engine, ファイル名の正規表現)
    ("NScripter", r"^(nscript\.dat|nscr_sec\.dat|0?0\.txt|arc\d*\.nsa|\d+\.ns2|.*\.nsa)$"),
    ("KiriKiri", r"^(data\.xp3|.*\.xp3|krkr\.exe|tvpwin32\.exe)$"),
    ("YU-RIS", r"^(ysbin\.ypf|.*\.ypf|yu-ris\.exe|yscfg\.dat)$"),
    ("SiglusEngine", r"^(scene\.pck|gameexe\.dat|siglusengine.*\.exe)$"),
    ("RealLive", r"^(seen\.txt|reallive\.exe|gameexe\.ini)$"),
    ("BGI/Ethornell", r"^(bgi\.exe|bgi\.gdb|sysgrp\.arc|sysprg\.arc)$"),
    ("CatSystem2", r"^(cs2\.exe|cs2conf\.dll|.*\.int)$"),
    ("Artemis", r"^(.*\.pfs|.*\.pfs\.\d+)$"),
    ("LiveMaker", r"^(.*\.lsb|game\.dat|.*\.lpb|livemaker.*)$"),
    ("Unity", r"^(unityplayer\.dll|globalgamemanagers|.*_data)$"),
    ("RenPy", r"^(.*\.rpa|.*\.rpyc|renpy)$"),
    ("NW.js/Tyrano", r"^(nw\.dll|package\.nw|nw\.pak|tyrano.*)$"),
    ("RPGMaker", r"^(game\.rgss\w*|rgss\d+.*\.dll|rpg_core\.js|rmmz_core\.js)$"),
    ("WolfRPG", r"^(data\.wolf|game\.wolf)$"),
    ("Majiro", r"^(scenario\.arc|majiro.*)$"),
    ("Softpal", r"^(.*\.pac|pal\.dll)$"),
    ("PONScripter/ONScripter", r"^(pscript\.dat|pns\.cfg|ponscr.*|onscripter.*)$"),
    ("AdvHD", r"^(advhd\.exe|.*\.ws2)$"),
    ("QLIE", r"^(.*\.pack|qlie.*)$"),
    ("Musica", r"^(.*\.paz)$"),
    ("ShiinaRio", r"^(.*\.war)$"), ("EntisGLS", r"^(.*\.noa|.*\.nos)$"), ("Nitroplus", r"^(.*\.npa)$"),
    ("AliceSoft System4", r"^(.*\.ain|.*\.ald|.*\.afa|system40\.exe)$"), ("FVP", r"^(.*\.hcb)$"),
    ("AdvWin32/WillPlus", r"^(rio\.arc|advwin32\.exe)$"), ("NeXAS", r"^(.*\.pac)$"),
    ("Installer:InstallShield", r"^(data1\.cab|data1\.hdr|.*\.hdr|ikernel\.ex_|setup\.inx|_setup\.dll)$"),
    ("Installer:MSI", r"^.*\.msi$"),
]
BIN_SIGS = [  # (種別, exe 内のバイト列)
    ("KiriKiri", rb"TVP\(KIRIKIRI\)"), ("KiriKiri", rb"XP3\r\n \n\x1a"),
    ("NScripter", rb"NScripter"), ("YU-RIS", rb"YU-RIS"), ("SiglusEngine", rb"SiglusEngine"),
    ("RealLive", rb"RealLive"), ("BGI/Ethornell", rb"BURIKO ARC20"), ("BGI/Ethornell", rb"PackFile    "),
    ("CatSystem2", rb"CatSystem2"), ("LiveMaker", rb"LiveMaker"), ("LiveMaker", rb"vf\x00\x00\x00"),
    ("Unity", rb"UnityPlayer"), ("Artemis", rb"Artemis"), ("PONScripter/ONScripter", rb"ONScripter"),
    ("AdvHD", rb"AdvHD"), ("QLIE", rb"QLIE"), ("NeXAS", rb"NeXAS"), ("ShiinaRio", rb"ShiinaRio"),
    ("EntisGLS", rb"EntisGLS"), ("Escude", rb"ESC-ARC"), ("Malie", rb"MalieSystem"), ("Malie", rb"malie"),
    ("Silky's AI6WIN", rb"AI6WIN"), ("Silky's AI5WIN", rb"AI5WIN"), ("AliceSoft System4", rb"System40"),
    ("CIRCUS", rb"CIRCUS"), ("Kaguya", rb"KaGuYa"), ("Lilim", rb"LILIM"), ("RPGMaker", rb"RGSS"), ("Majiro", rb"MajiroArcV"), ("CatSystem2", rb"CS2CONF"),
    ("Installer:InnoSetup", rb"Inno Setup Setup Data"), ("Installer:NSIS", rb"Nullsoft Install System"),
    ("Installer:NSIS", rb"NullsoftInst"), ("Installer:InstallShield", rb"InstallShield"),
    ("Installer:WiseInstaller", rb"Wise Installation"), ("SFX:7z", rb"7z\xbc\xaf\x27\x1c"),
    ("SFX:RAR", rb"Rar!\x1a\x07"), ("SFX:CAB", rb"MSCF\x00\x00\x00\x00"), ("SFX:LZH", rb"-lh5-"),
    ("SFX:ZIP", rb"PK\x03\x04"),
    ("DRM:SoftDenchi", rb"SoftDenchi"), ("DRM:DMM", rb"DMMGamePlayer"), ("DRM:Alpha-ROM", rb"ARCHIVE_ROM"),
    ("DRM:StarForce", rb"StarForce"), ("DRM:SecuROM", rb"SecuROM"),
    ("Runtime:DirectShow", rb"quartz.dll"), ("Runtime:VC71", rb"MSVCR71.dll"),
    ("Runtime:VC80", rb"MSVCR80.dll"), ("Runtime:VC90", rb"MSVCR90.dll"), ("Runtime:VC100", rb"MSVCR100.dll"),
    ("Runtime:MFC42", rb"MFC42.DLL"), ("Runtime:VB6", rb"MSVBVM60.DLL"), ("Runtime:D3DX9", rb"d3dx9_"),
    ("Runtime:DotNet", rb"_CorExeMain"), ("Runtime:WMP", rb"wmvcore.dll"), ("Runtime:Flash", rb"Flash.ocx"),
]

def scan_bin(path, limit=64 << 20):
    hits = set()
    try:
        with open(path, "rb") as f:
            head = f.read(limit)
            f.seek(max(0, os.path.getsize(path) - (8 << 20)))
            tail = f.read()
    except OSError:
        return hits
    for buf in (head, tail):
        for name, sig in BIN_SIGS:
            if sig in buf if isinstance(sig, bytes) else False:
                hits.add(name)
        low = buf.lower()
        for name, sig in BIN_SIGS:
            if name.startswith("Runtime:") and sig.lower() in low:
                hits.add(name)
    return hits

def pe_arch(path):
    try:
        with open(path, "rb") as f:
            d = f.read(4096)
        if d[:2] != b"MZ": return None
        off = int.from_bytes(d[0x3C:0x40], "little")
        if d[off:off+4] != b"PE\0\0": return "NE/MZ(16bit?)"
        m = int.from_bytes(d[off+4:off+6], "little")
        return {0x14C: "x86", 0x8664: "x64", 0xAA64: "arm64"}.get(m, hex(m))
    except OSError:
        return None

def detect(root):
    files, score, exes = [], {}, []
    if os.path.isfile(root):
        exes = [root]
    else:
        for d, dirs, fs in os.walk(root):
            if d[len(root):].count(os.sep) > 3: continue
            for n in fs + dirs:
                files.append(os.path.join(d, n))
    for p in files:
        n = os.path.basename(p).lower()
        for eng, rx in FILE_SIGS:
            if re.match(rx, n):
                score[eng] = score.get(eng, 0) + 1
        if n.endswith(".exe") and os.path.isfile(p):
            exes.append(p)
    exe_info = []
    for e in sorted(exes, key=lambda p: -os.path.getsize(p))[:8]:
        hits = scan_bin(e)
        for h in hits:
            score[h] = score.get(h, 0) + 3
        exe_info.append({"exe": os.path.relpath(e, root) if os.path.isdir(root) else e,
                         "size_mb": round(os.path.getsize(e) / 2**20, 1),
                         "arch": pe_arch(e), "hits": sorted(hits)})
    engines = sorted(((k, v) for k, v in score.items() if ":" not in k), key=lambda x: -x[1])
    tags = sorted(k for k in score if ":" in k)
    return {"path": root, "engine": engines[0][0] if engines else "unknown",
            "engine_scores": dict(engines), "tags": tags, "exes": exe_info}

if __name__ == "__main__":
    print(json.dumps(detect(sys.argv[1]), ensure_ascii=False, indent=1))

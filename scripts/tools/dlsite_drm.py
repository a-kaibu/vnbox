#!/usr/bin/env python3
"""dlsite_drm.py <list.tsv...>  DLsite の作品ページから DRM の種類を読み、各 tsv の末尾列に足す (PlayDRM / ソフト電池 / シリアル番号 / なし)"""
import re, sys, time, urllib.request
from concurrent.futures import ThreadPoolExecutor
H = {"User-Agent": "Mozilla/5.0", "Cookie": "adultchecked=1"}
def drm(row):
    site = "pro" if row[0].startswith("VJ") else "maniax"
    u = f"https://www.dlsite.com/{site}/work/=/product_id/{row[0]}.html"
    for _ in range(3):
        try:
            h = urllib.request.urlopen(urllib.request.Request(u, headers=H), timeout=40).read().decode("utf-8", "replace")
            break
        except Exception:
            time.sleep(3)
    else:
        return "?"
    kinds = []
    if re.search(r"PlayDRM型", h): kinds.append("PlayDRM")
    if re.search(r"ソフト電池", h): kinds.append("ソフト電池")
    if re.search(r"シリアル番号発行型|シリアル番号を発行", h): kinds.append("シリアル番号")
    if re.search(r"DLsite Play専用|DLsite Playのみ", h): kinds.append("DLsitePlay")
    return "+".join(kinds) or "なし"
for f in sys.argv[1:]:
    rows = [l.rstrip("\n").split("\t") for l in open(f, encoding="utf-8")]
    with ThreadPoolExecutor(4) as ex:
        kinds = list(ex.map(drm, rows))
    with open(f, "w", encoding="utf-8") as o:
        for r, k in zip(rows, kinds):
            o.write("\t".join(r[:8] + [k]) + "\n")

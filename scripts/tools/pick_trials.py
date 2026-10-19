#!/usr/bin/env python3
"""links.tsv から年度ごとにブランドが重ならない体験版を選ぶ。HEAD でサイズを確認"""
import os, random, re, sys, urllib.request
rows = [l.rstrip("\n").split("\t") for l in open(sys.argv[1], encoding="utf-8")]
per_year = int(sys.argv[3]) if len(sys.argv) > 3 else 2
bad = re.compile(r"(?i)patch|update|upd|wall|kabe|sabun|bgm|voice|movie|_pv|icon|banner|bnr|ss\d|cm\d|chara|tokuten|omake|_op|op_|manual|font|gekkan|countdown|wp_|screensaver|saver")
good = re.compile(r"(?i)trial|taiken|_try|try\.|_t\.zip|_tr\.")
bad2 = re.compile(r"(?i)demo")
random.seed(int(os.environ.get('SEED','11'))); used = set(l.split('\t')[1] for l in open(sys.argv[4], encoding='utf-8')) if len(sys.argv) > 4 else set(); out = open(sys.argv[2], "w", encoding="utf-8")
def size(u):
    try:
        r = urllib.request.urlopen(urllib.request.Request(u, method="HEAD", headers={"User-Agent": "Mozilla/5.0"}), timeout=20)
        return int(r.headers.get("Content-Length") or 0)
    except Exception:
        return 0
years = [int(y) for y in os.environ.get('YEARS', '').split()] or range(2006, 2027)
for year in years:
    cand = [r for r in rows if r[0] == str(year) and good.search(r[2].rsplit("/", 1)[1]) and not bad.search(r[2].rsplit("/", 1)[1]) and not bad2.search(r[2].rsplit("/", 1)[1]) and r[1] not in used]
    random.shuffle(cand); n = 0
    for y, brand, url, page in cand:
        if brand in used: continue
        s = size(url)
        if not (40 << 20) < s < (900 << 20): continue
        used.add(brand); n += 1
        out.write(f"{y}\t{brand}\t{s >> 20}\t{url}\n"); out.flush(); print(y, brand, s >> 20, url.rsplit('/', 1)[1], flush=True)
        if n >= per_year: break

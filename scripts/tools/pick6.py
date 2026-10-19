#!/usr/bin/env python3
"""pick6.py <out.tsv>  第6弾の候補: suezou の未試行の体験版 + DLsite の体験版 (pro / 同人)。
出力: id, url, 題名(分かれば), 出典, DRM(製品版), サイズMB"""
import collections, glob, os, random, re, sys, urllib.request
from concurrent.futures import ThreadPoolExecutor
V = "/home/user/vnbox"
random.seed(6)
out = []
# suezou: 作品ディレクトリが未試行のもの。年をラウンドロビン、同じ年・ブランドは 2 本まで
rows = [l.rstrip("\n").split("\t") for l in open(f"{V}/downloads/suezou/links.tsv", encoding="utf-8")]
good = re.compile(r"(?i)trial|taiken|_try|try\.|_t\.zip|_tr\.")
bad = re.compile(r"(?i)patch|update|upd|wall|kabe|sabun|bgm|voice|movie|_pv|icon|banner|bnr|ss\d|cm\d|chara|tokuten|omake|_op|op_|manual|font|gekkan|countdown|wp_|screensaver|saver|demo")
slug = lambda u: u.rsplit("/", 1)[0]
used = set()
for f in glob.glob(f"{V}/downloads/suezou/*.tsv"):
    if f.endswith("links.tsv"): continue
    for l in open(f, encoding="utf-8"):
        for x in l.rstrip("\n").split("\t"):
            if x.startswith("http"): used.add(slug(x))
cand = collections.defaultdict(list)
for y, brand, url, _ in rows:
    fn = url.rsplit("/", 1)[1]
    if good.search(fn) and not bad.search(fn) and slug(url) not in used and y != "?":
        cand[y].append((y, brand, url))
for y in cand: random.shuffle(cand[y])
order = []
while any(cand.values()):
    for y in sorted(cand):
        if cand[y]: order.append(cand[y].pop())
def size(u):
    try:
        r = urllib.request.urlopen(urllib.request.Request(u, method="HEAD", headers={"User-Agent": "Mozilla/5.0"}), timeout=20)
        return int(r.headers.get("Content-Length") or 0)
    except Exception:
        return 0
per = collections.Counter(); seen = set(); n = 0
with ThreadPoolExecutor(8) as ex:
    for (y, brand, url), s in zip(order, ex.map(lambda c: size(c[2]), order)):
        b = brand.replace("-", "_"); key = (y, b)
        if per[key] >= 2 or not (30 << 20) < s < (1500 << 20): continue
        work = re.sub(r"[^A-Za-z0-9]", "_", slug(url).rsplit("/", 1)[1])[:20]
        i = f"s{y}_{b}_{work}"
        if i in seen: continue
        per[key] += 1; seen.add(i); n += 1
        out.append((i, url, "", "suezou", "", str(s >> 20)))
        if n >= int(os.environ.get("NSUEZOU", "240")): break
# DLsite: メーカー/サークルごとに 2 本まで、1.6GB 未満
for f, lim in (("pro", int(os.environ.get("NPRO", "110"))), ("maniax", int(os.environ.get("NMANIAX", "110")))):
    rows = [l.rstrip("\n").split("\t") for l in open(f"{V}/downloads/dlsite/{f}.tsv", encoding="utf-8")]
    random.shuffle(rows)
    per = collections.Counter(); k = 0
    for w, name, maker, typ, opts, url, sz, date, drm in rows:
        if per[maker] >= 2 or not 0 < int(sz) < (1600 << 20): continue
        per[maker] += 1; k += 1
        out.append((f"dl_{w}", url, name, f"dlsite-{f}/{typ}", drm, str(int(sz) >> 20)))
        if k >= lim: break
random.shuffle(out)  # 出典が偏らないよう混ぜる
with open(sys.argv[1], "w", encoding="utf-8") as o:
    for r in out: o.write("\t".join(r) + "\n")
print(len(out), collections.Counter(r[3].split("/")[0] for r in out))

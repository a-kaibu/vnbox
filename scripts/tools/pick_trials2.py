#!/usr/bin/env python3
"""pick_trials2.py <links.tsv> <out.tsv> <総数> <used.tsv>
(年, ブランド) と作品ディレクトリが既出でない体験版を選ぶ (ブランドの重複は年が違えば可)。HEAD でサイズを確認"""
import os, random, re, sys, urllib.request, collections
from concurrent.futures import ThreadPoolExecutor
rows = [l.rstrip("\n").split("\t") for l in open(sys.argv[1], encoding="utf-8")]
total = int(sys.argv[3])
bad = re.compile(r"(?i)patch|update|upd|wall|kabe|sabun|bgm|voice|movie|_pv|icon|banner|bnr|ss\d|cm\d|chara|tokuten|omake|_op|op_|manual|font|gekkan|countdown|wp_|screensaver|saver|demo")
good = re.compile(r"(?i)trial|taiken|_try|try\.|_t\.zip|_tr\.")
slug = lambda u: u.rsplit("/", 1)[0]
used = [l.rstrip("\n").split("\t") for l in open(sys.argv[4], encoding="utf-8")]
used_yb = {(u[0], u[1].replace("-", "_")) for u in used}; used_slug = {slug(u[3]) for u in used}
random.seed(int(os.environ.get("SEED", "31")))
cand = collections.defaultdict(list)
for y, brand, url, page in rows:
    fn = url.rsplit("/", 1)[1]
    if not good.search(fn) or bad.search(fn) or (y, brand.replace("-", "_")) in used_yb or slug(url) in used_slug: continue
    cand[y].append((y, brand, url))
for y in cand: random.shuffle(cand[y])
order = []  # 年をラウンドロビンで回して偏らないようにする
while any(cand.values()):
    for y in sorted(cand):
        if cand[y]: order.append(cand[y].pop())
def size(u):
    try:
        r = urllib.request.urlopen(urllib.request.Request(u, method="HEAD", headers={"User-Agent": "Mozilla/5.0"}), timeout=20)
        return int(r.headers.get("Content-Length") or 0)
    except Exception:
        return 0
out = open(sys.argv[2], "w", encoding="utf-8"); n = 0; taken = set()
with ThreadPoolExecutor(8) as ex:
    for (y, brand, url), s in zip(order, ex.map(lambda c: size(c[2]), order)):
        key = (y, brand.replace("-", "_"))
        if key in taken or slug(url) in used_slug or not (40 << 20) < s < (900 << 20): continue
        taken.add(key); used_slug.add(slug(url)); n += 1
        out.write(f"{y}\t{brand}\t{s >> 20}\t{url}\n"); out.flush()
        if n >= total: break
print("picked", n)

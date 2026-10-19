#!/usr/bin/env python3
"""dlsite_list.py <site> <pages> <out.tsv>  DLsite の体験版ありの PC ゲーム一覧を作る
site: pro (美少女ゲーム) / maniax (同人)。出力: workno, 作品名, サークル/ブランド, 種別, options, 体験版URL, サイズ, 発売日, ソフト電池/DRM の注記"""
import json, re, sys, time, urllib.request
site, pages, out = sys.argv[1], int(sys.argv[2]), sys.argv[3]
cat = {"pro": "work_category%5B0%5D/pc", "maniax": "work_category%5B0%5D/doujin/work_type_category%5B0%5D/game"}[site]
H = {"User-Agent": "Mozilla/5.0", "Cookie": "adultchecked=1"}
def get(u):
    for i in range(3):
        try:
            return urllib.request.urlopen(urllib.request.Request(u, headers=H), timeout=40).read().decode("utf-8", "replace")
        except Exception as e:
            err = e; time.sleep(3)
    raise err
ids = []
for p in range(1, pages + 1):
    html = get(f"https://www.dlsite.com/{site}/fsr/=/language/jp/sex_category%5B0%5D/male/{cat}/options%5B0%5D/TRI/order/release_d/per_page/100/page/{p}")
    for w in re.findall(r"product_id/([RVB]J\d{6,8})", html):
        if w not in ids: ids.append(w)
    time.sleep(1)
def info(w):
    try:
        d = json.loads(get(f"https://www.dlsite.com/{site}/api/=/product.json?workno={w}") or "[]")
    except Exception:
        return None
    return d[0] if d else None
f = open(out, "w", encoding="utf-8")
from concurrent.futures import ThreadPoolExecutor
with ThreadPoolExecutor(4) as ex:
    for d in ex.map(info, ids):
        if not d: continue
        tr = [t for t in d.get("trials") or [] if t.get("url")]
        if not tr or not d.get("is_pc_work"): continue
        t = tr[0]
        f.write("\t".join([d["workno"], d["work_name"].replace("\t", " "), (d.get("maker_name") or "").replace("\t", " "), d.get("work_type") or "",
                           d.get("options") or "", "https:" + t["url"], str(t.get("file_size") or 0), (d.get("regist_date") or "")[:10]]) + "\n")
print("works", len(ids))

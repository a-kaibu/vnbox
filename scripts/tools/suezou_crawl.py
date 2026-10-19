#!/usr/bin/env python3
"""suezou ミラーの全ページからアーカイブへのリンクを集めて TSV (year, brand, url, page) に出す"""
import re, sys, time, urllib.request, urllib.parse
BASE = "http://suezou.dyndns.org/"
def get(u):
    for _ in range(3):
        try:
            return urllib.request.urlopen(urllib.request.Request(u, headers={"User-Agent": "Mozilla/5.0"}), timeout=30).read().decode("utf-8", "replace")
        except Exception:
            time.sleep(2)
    return ""
top = get(BASE + "top.html")
pages = sorted(set(re.findall(r'href="([a-z0-9_\-]+\.html)"', top, re.I)))
seen = set(); out = open(sys.argv[1], "w", encoding="utf-8")
for i, p in enumerate(pages):
    html = get(BASE + p); time.sleep(0.2)
    for href in re.findall(r'href="([^"]+\.(?:zip|exe|7z|rar|lzh|iso|cab))"', html, re.I):
        u = urllib.parse.urljoin(BASE + p, href)
        if u in seen: continue
        seen.add(u)
        m = re.search(r"/dl(\d{4})[a-z0-9]*/([^/]+)/", u)
        out.write(f"{m.group(1) if m else '?'}\t{m.group(2) if m else '?'}\t{u}\t{p}\n")
    if i % 40 == 0: print(f"{i}/{len(pages)} pages, {len(seen)} links", flush=True)
print(f"done: {len(pages)} pages, {len(seen)} links")

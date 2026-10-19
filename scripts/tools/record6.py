#!/usr/bin/env python3
"""record6.py [--note X] [--replace] <id>...  第6弾 (summary6.tsv) の結果を verified.tsv に記録する
題名: DLsite は作品名、suezou は起動したウィンドウのタイトル (無ければ id)。エンジン: launch.ini の判定。
注記の既定は「タイトル」。DLsite 製品版の DRM 種別と、配布物から見つかった DRM の痕跡も注記に添える。"""
import re, sys
V = '/home/user/vnbox'
args = sys.argv[1:]; note = 'タイトル'
if '--note' in args:
    i = args.index('--note'); note = args[i + 1]; del args[i:i + 2]
tt = None
if '--title' in args:
    i = args.index('--title'); tt = args[i + 1]; del args[i:i + 2]
replace = '--replace' in args
args = [a for a in args if a != '--replace']
lst = {c[0]: c for c in (l.rstrip('\n').split('\t') for l in open(f'{V}/downloads/batch6.tsv', encoding='utf-8'))}
summ = {c[0]: c for c in (l.rstrip('\n').split('\t') for l in open(f'{V}/results/batch/summary6.tsv', encoding='utf-8'))}
skip = re.compile(r'スタートアップ|設定|インストール|Exception|名前の変更|^Information$|確認|Default IME|^$')
def window_title(i, st):
    try:
        for l in open(f'{V}/results/b6_{i}_{st}/summary.txt', encoding='utf-8', errors='replace'):
            if l.startswith('windows='):
                ws = [w.strip() for w in l[8:].split('|') if not skip.search(w.strip())]
                return ws[0] if ws else ''
    except OSError:
        pass
    return ''
ledger = f'{V}/results/verified.tsv'
rows = open(ledger, encoding='utf-8').readlines()
if replace:
    rows = [l for l in rows if l.split('\t')[0] not in args]
have = {l.split('\t')[0] for l in rows}
for a in args:
    if a in have: continue
    c, s = lst.get(a), summ.get(a)
    if not c or not s:
        print('unknown id', a); continue
    title = tt or c[2] or window_title(a, s[1]) or a
    engine = s[4] if len(s) > 4 and s[4] else '-'
    extra = []
    if s[1] == 'classic' and s[2] == 'OK': extra.append('従来 Wine')
    if c[4] and c[4] not in ('なし', '-'): extra.append(f'製品版 DRM: {c[4]}')
    if len(s) > 7 and s[7]: extra.append(f'痕跡: {s[7].rstrip(",")}')
    rows.append(f"{a}\t{title}\t{engine}\t{note}{' (' + ', '.join(extra) + ')' if extra else ''}\n")
open(ledger, 'w', encoding='utf-8').writelines(rows)
strict = [l for l in rows if l.strip() and not l.startswith('#') and not l.rstrip('\n').split('\t')[-1].startswith('弱:')]
r6 = [l for l in strict if l.split('\t')[0] in summ]
print('total strict', len(strict), '| this round', len(r6))

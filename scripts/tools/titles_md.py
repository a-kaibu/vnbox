#!/usr/bin/env python3
"""titles_md.py  results/verified.tsv から VERIFICATION.md の「起動確認済みタイトル一覧」を作り直す (マーカー間を置換)"""
import re
V = '/home/user/vnbox/results/verified.tsv'; R = '/home/user/vnbox/VERIFICATION.md'
rows = [l.rstrip('\n').split('\t') for l in open(V, encoding='utf-8') if l.strip() and not l.startswith('#')]
import glob
dl = {}  # DLsite の作品番号 → 登録日
for f in glob.glob('/home/user/vnbox/downloads/dlsite/*.tsv'):
    for l in open(f, encoding='utf-8'):
        c = l.rstrip('\n').split('\t')
        if len(c) > 7: dl[c[0]] = c[7][:4]
def year(i):
    m = re.match(r'[bs](\d{4})_', i)
    if m: return m.group(1)
    if i.startswith('dl_'): return dl.get(i[3:], '-')
    return '-'
def source(i):
    if i.startswith('dl_RJ'): return 'DLsite 同人'
    if i.startswith('dl_'): return 'DLsite'
    if re.match(r'[bs]\d{4}_', i): return 'suezou'
    return '手動'
def cell(s): return s.replace('|', '／').strip()
def key(r): return (year(r[0]) == '-', year(r[0]), r[0])
strict = sorted([r for r in rows if not r[3].startswith('弱:')], key=key)
weak = sorted([r for r in rows if r[3].startswith('弱:')], key=key)
out = [f'タイトル画面以降まで目視で確認: **{len(strict)} 本** (ロゴ・注意画面止まりの {len(weak)} 本は下の折りたたみに別記)。',
       '年 = 配布ミラーの掲載年度 / DLsite の登録年 (「-」は第1弾の手動テスト分)。エンジンの「-」は自動判定できなかったもの。', '',
       '| # | 年 | 出典 | タイトル | エンジン | 確認できたところ | ID |', '|---|---|---|---|---|---|---|']
for n, r in enumerate(strict, 1):
    out.append(f'| {n} | {year(r[0])} | {source(r[0])} | {cell(r[1])} | {cell(r[2])} | {cell(r[3])} | `{r[0]}` |')
out += ['', '<details><summary>弱い確認 (ロゴ・注意画面・起動ダイアログまで) ' + str(len(weak)) + ' 本</summary>', '',
        '| 年 | タイトル | 止まったところ | ID |', '|---|---|---|---|']
out += [f'| {year(r[0])} | {cell(r[1])} | {cell(r[3][2:])} | `{r[0]}` |' for r in weak]
out += ['', '</details>']
s = open(R, encoding='utf-8').read()
a, b = '<!-- TITLES:BEGIN -->', '<!-- TITLES:END -->'
s = s[:s.index(a) + len(a)] + '\n' + '\n'.join(out) + '\n' + s[s.index(b):]
open(R, 'w', encoding='utf-8').write(s); print('strict', len(strict), 'weak', len(weak))

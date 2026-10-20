#!/usr/bin/env python3
"""stats_md.py  results/verified.tsv / excluded.tsv / 第6弾の一覧から VERIFICATION.md の「集計」を作り直す
(<!-- STATS:BEGIN --> と <!-- STATS:END --> の間を置換)"""
import collections
V = '/home/user/vnbox'
R = f'{V}/VERIFICATION.md'
rows = [l.rstrip('\n').split('\t') for l in open(f'{V}/results/verified.tsv', encoding='utf-8') if l.strip() and not l.startswith('#')]
strict = [r for r in rows if not r[-1].startswith('弱:')]
weak = [r for r in rows if r[-1].startswith('弱:')]
excluded = [l.rstrip('\n').split('\t') for l in open(f'{V}/results/excluded.tsv', encoding='utf-8') if l.strip() and not l.startswith('#')]
b6 = {c[0]: c for c in (l.rstrip('\n').split('\t') for l in open(f'{V}/downloads/batch6.tsv', encoding='utf-8'))}
ok6 = [r for r in strict if r[0] in b6]
weak6 = [r for r in weak if r[0] in b6]
ex6 = [c for c in excluded if c[0] in b6]
left6 = [i for i in b6 if i not in {r[0] for r in rows} and i not in {c[0] for c in excluded}]
src = collections.Counter('suezou' if b6[r[0]][3] == 'suezou' else 'DLsite' for r in ok6)
drm = collections.Counter(b6[r[0]][4] or 'なし' for r in ok6 if b6[r[0]][3].startswith('dlsite'))
eng = collections.Counter(r[2] if r[2] not in ('', '-') else '判定外 (独自エンジン等)' for r in ok6)

out = [f'**タイトル画面以降まで目視で確認できたもの: {len(strict)} 本** (一覧は下の「起動確認済みタイトル一覧」)。',
       '- 第1〜3弾: 102 本 (第1弾 22 本 + suezou ミラー 80 本)',
       '- 第4弾 (2026-10-19): suezou ミラーから新たに 108 本、ムジナ 1 本、未解決分の解消 59 本',
       f'- 第6弾 (2026-10-19〜20、Go 版 `vnbox add` でバッチ): 候補 {len(b6)} 本 (suezou {sum(1 for c in b6.values() if c[3] == "suezou")} / DLsite {sum(1 for c in b6.values() if c[3] != "suezou")}) から '
       f'**{len(ok6)} 本** (suezou {src["suezou"]} / DLsite {src["DLsite"]})。'
       f'対象外 {len(ex6)} 本、弱い確認 {len(weak6)} 本、未確定 {len(left6)} 本',
       '',
       f'対象外 (`results/excluded.tsv`、計 {len(excluded)} 本) は理由付き: 配布物が紹介動画・音声・HTML5 のみ、配布ファイルの破損や削除、VR 専用、GPU 必須 (D3D12 SM6 専用の UE 5.8)。',
       f'弱い確認 ({len(weak)} 本): ' + '; '.join(f'`{r[0]}` {r[-1][2:].strip()}' for r in weak),
       '']
if left6:
    out.append('第6弾で未確定: ' + ', '.join(f'`{i}`' for i in left6))
    out.append('')
out += ['第6弾の DLsite 分の製品版 DRM (作品ページの表記): ' + ' / '.join(f'{k} {v}' for k, v in drm.most_common()) + ' (体験版にはどれも入っていない)', '',
        '第6弾のエンジン別 (`vnbox add` の判定):', '', '| エンジン | 本数 |', '|---|---|']
out += [f'| {k} | {v} |' for k, v in eng.most_common()]
s = open(R, encoding='utf-8').read()
a, b = s.index('<!-- STATS:BEGIN -->'), s.index('<!-- STATS:END -->')
s = s[:a] + '<!-- STATS:BEGIN -->\n' + '\n'.join(out) + '\n' + s[b:]
open(R, 'w', encoding='utf-8').write(s)
print(len(strict), len(ok6), len(left6))

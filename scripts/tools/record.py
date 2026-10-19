#!/usr/bin/env python3
"""record.py [--weak] [--note X] <id>...  summary.tsv の窓タイトル/エンジンを使って verified.tsv に追記 (重複は無視)"""
import sys, re
args = sys.argv[1:]; weak = '--weak' in args; note = 'タイトル'
if '--note' in args: i = args.index('--note'); note = args[i+1]; del args[i:i+2]
replace = '--replace' in args
args = [a for a in args if a not in ('--weak', '--replace')]
if replace:  # 既存行を消してから書き直す (弱→確認済みへの格上げ等)
    keep = [l for l in open(V if 'V' in dir() else '/home/user/vnbox/results/verified.tsv', encoding='utf-8') if l.split('\t')[0] not in args]
    open('/home/user/vnbox/results/verified.tsv', 'w', encoding='utf-8').writelines(keep)
V = '/home/user/vnbox/results/verified.tsv'; S = '/home/user/vnbox/results/batch/summary.tsv'
have = {l.split('\t')[0] for l in open(V, encoding='utf-8')}
info = {}
for l in open(S, encoding='utf-8'):
    f = l.rstrip('\n').split('\t')
    if len(f) < 6: continue
    wins = [w.strip() for w in f[-1].split('|') if w.strip() and not re.search(r'スタートアップ|設定|インストール|Exception|名前の変更|^Information$|確認', w)]
    info[f[0]] = (wins[0] if wins else f[0], f[4] if f[4] != 'unknown' else '-')
with open(V, 'a', encoding='utf-8') as out:
    for a in args:
        if a in have: continue
        t, e = info.get(a, (a, '-'))
        out.write(f"{a}\t{t}\t{e}\t{'弱:' if weak else ''}{note}\n")
rows = [l.rstrip('\n').split('\t') for l in open(V, encoding='utf-8') if l.strip() and not l.startswith('#')]
new = [r for r in rows if r[0] in set(x.split('\t')[0] for x in open(S, encoding='utf-8').readlines()[120:]) and not r[-1].startswith('弱:')]
print('total strict', sum(1 for r in rows if not r[-1].startswith('弱:')), '| new strict (this round)', len(new))

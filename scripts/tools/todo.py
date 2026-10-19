#!/usr/bin/env python3
"""todo.py  今回 (summary.tsv 121 行目以降) の結果のうち、台帳にも reviewed.txt にも無いものを表示"""
import os
B='/home/user/vnbox/results/batch'
rows=[l.rstrip('\n').split('\t') for l in open(B+'/summary.tsv',encoding='utf-8').readlines()[120:]]
have={l.split('\t')[0] for l in open('/home/user/vnbox/results/verified.tsv',encoding='utf-8')}
rev=set(open(B+'/reviewed.txt').read().split()) if os.path.exists(B+'/reviewed.txt') else set()
todo=[r for r in rows if r[0] not in have and r[0] not in rev]
for r in todo: print(r[0], r[3], r[8])
print('NAMES', ' '.join(r[0] for r in todo if r[3] not in ('manual_installer','not_game','DL_FAIL','extract_fail')))

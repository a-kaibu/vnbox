#!/bin/bash
# retry_pass.sh  NG で残ったタイトル (games/<name> がある) を従来の 32bit Wine (WineHQ, 旧 WoW64) で再試行する
# 新 WoW64 では 32bit の kernel32 が syswow64 から読まれるため、それを検査するプロテクトで落ちるゲームがある
cd "$(dirname "$0")/../.."
PROBE="shot:t25 pass:5 sleep:10 shot:t1 sleep:12 shot:t2 click:640,400 sleep:3 key:Return sleep:4 shot:c"
OUT=results/batch/summary_retry.tsv
for k in results/batch/*.kept; do
  n=$(basename $k .kept); [ -d games/$n ] || continue
  grep -q "^$n	" $OUT 2>/dev/null && continue
  c=${n}_c32
  INFO=$(python3 -I scripts/detect.py games/$n)
  EXE=$(python3 -I -c 'import json,sys,re
d=json.loads(sys.argv[1])
c=[e for e in d["exes"] if not re.search(r"(?i)unins|setup|install|config|設定|launcher|crash|vc_?redist|dxsetup|directx|updater|readme|manual",e["exe"])]
print((c or d["exes"])[0]["exe"])' "$INFO" 2>/dev/null)
  [ -z "$EXE" ] && { echo -e "$n\tno_exe" >> $OUT; continue; }
  ( unset WINE_FLAVOR BASE; INPUT="$PROBE" scripts/run.sh $c "games/$n/$(dirname "$EXE")" "$(basename "$EXE")" 30 >/dev/null 2>&1 )
  v=$(scripts/tools/verdict.sh $c)
  montage results/$c/0*.png -tile 4x -geometry 320x200+2+2 -background '#222' results/batch/$c.png 2>/dev/null
  echo -e "$n\t$v\tclassic32" | tee -a $OUT
  rm -rf "/home/user/vnbox/prefixes/$c"
done
echo RETRY_DONE

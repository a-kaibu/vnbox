#!/bin/bash
# trim_results.sh  results/<run>/ のスクショを最後の 2 枚だけ残して消す (一覧画像に使うのは最後の 2 枚)。10 分ごとに繰り返す
cd "$(dirname "$0")/../../results"
while true; do
  for d in */; do d=${d%/}
    [ -f "$d/summary.txt" ] && grep -q alive_after "$d/summary.txt" || continue   # 実行中のものは触らない
    mapfile -t s < <(ls "$d"/0*.png 2>/dev/null); k=${#s[@]}
    for ((i=0;i<k-2;i++)); do rm -f "${s[$i]}"; done
  done
  sleep 600
done

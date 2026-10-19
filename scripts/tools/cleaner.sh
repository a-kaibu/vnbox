#!/bin/bash
# バッチ済みで verdict=OK のタイトルは本体を消してディスクを空ける (スクショと結果は残す)
cd "$(dirname "$0")/../.."; shopt -s nullglob; LOG=${1:-results/batch4.log}
while true; do
  for d in results/batch/*.done; do
    n=$(basename "$d" .done); [ -f results/batch/$n.cleaned ] && continue
    v=$(scripts/tools/verdict.sh $n)
    if [ "$v" = OK ]; then V=/home/user/vnbox; rm -rf "$V/games/$n" "$V/packages/$n" "$V/prefixes/$n" "$V/prefixes/run-$n"; echo "$n $v" > results/batch/$n.cleaned
    else echo "$n $v" > results/batch/$n.kept; fi
  done
  grep -q BATCH_DONE "$LOG" 2>/dev/null && break
  sleep 60
done

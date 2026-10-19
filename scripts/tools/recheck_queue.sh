#!/bin/bash
# recheck_queue.sh  results/batch/recheck_queue.tsv (id <TAB> 操作列) を 1 本ずつ recheck6.sh で再試行し続ける (処理済みは飛ばす)
cd "$(dirname "$0")/../.."
while true; do
  while IFS=$'\t' read -r id probe; do
    [ -z "$id" ] && continue
    grep -q "^$id	" results/batch/summary_re6.tsv 2>/dev/null && continue
    [ -d results/batch/$id.re6lock ] && continue
    grep "^$id	" downloads/batch6.tsv > /tmp/re6_$id.tsv
    PROBE="$probe" scripts/tools/recheck6.sh /tmp/re6_$id.tsv 1 >> results/batch/recheck6.log 2>&1
  done < results/batch/recheck_queue.tsv
  sleep 60
done

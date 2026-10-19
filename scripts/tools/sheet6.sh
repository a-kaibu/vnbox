#!/bin/bash
# sheet6.sh <out.png> [件数]  第6弾で未確認 (verified.tsv / recheck6.tsv / excluded.tsv に無い) の結果を一覧画像にする。対象 id を表示
cd "$(dirname "$0")/../.."; OUT=$1; N=${2:-12}
seen=$(cut -f1 results/verified.tsv results/batch/recheck6.tsv results/excluded.tsv 2>/dev/null | sort -u)
runs=(); ids=()
while IFS=$'\t' read -r id st v rest; do
  [ "$st" = "-" ] && continue
  grep -qx "$id" <<< "$seen" && continue
  ids+=("$id:$v"); runs+=("b6_${id}_$st"); [ ${#runs[@]} -ge $N ] && break
done < results/batch/summary6.tsv
[ ${#runs[@]} -eq 0 ] && { echo "none"; exit; }
scripts/tools/sheet.sh "$OUT" "${runs[@]}"; printf '%s\n' "${ids[@]}"

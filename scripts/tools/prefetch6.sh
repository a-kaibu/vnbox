#!/bin/bash
# prefetch6.sh <list.tsv> [並列数] [先読み数]  batch6.sh より先に配布物を downloads/b6pre/<id>/ へ取得しておく (取得完了で .done)
#   空き容量が 12GB を切るか、未使用の先読みが [先読み数] 個たまったら待つ
cd "$(dirname "$0")/../.."; V=$PWD; LIST=$1; P=${2:-3}; AHEAD=${3:-6}
mkdir -p downloads/b6pre
fetch(){ local id=$(printf '%s\n' "$1" | cut -f1) url=$(printf '%s\n' "$1" | cut -f2) D
  grep -q "^$id	" $V/results/batch/summary6.tsv 2>/dev/null && return
  [ -d $V/results/batch/$id.b6lock ] && return                  # batch6 が既に取り掛かっている
  # 空きを待ってから取り掛かる (先に印を付けると、batch6 がまだ始まらない取得を待ち続けてしまう)
  while [ "$(ls -d $V/downloads/b6pre/*/.done 2>/dev/null | wc -l)" -ge $AHEAD ] || [ "$(df --output=avail -B1G / | tail -1 | tr -d ' ')" -lt 5 ]; do sleep 20; done
  [ -d $V/results/batch/$id.b6lock ] && return
  D=$V/downloads/b6pre/$id; mkdir $D 2>/dev/null || return
  if curl -sSL --retry 3 -m 3600 -A Mozilla/5.0 -o "$D/$(basename "${url%%\?*}")" "$url"; then touch $D/.done; else touch $D/.fail; fi; }
export -f fetch; export V AHEAD
cut -f1-2 "$LIST" | tr '\n' '\0' | xargs -0 -P "$P" -I{} bash -c 'fetch "$1"' _ {}
echo PREFETCH_DONE

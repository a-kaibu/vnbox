#!/bin/bash
# 起動中のゲーム・配信・Xvfb をすべて止める (自分自身と親シェル、results/.protect に書いた prefix/ディスプレイは除外)
cd "$(dirname "$0")/.."
me=$$; parent=$PPID; P=$(cat results/.protect 2>/dev/null)
for p in prefixes/*/; do n=$(basename "$p"); grep -qx "$n" <<<"$P" && continue; WINEPREFIX=$PWD/$p wineserver -k 2>/dev/null; done
for pat in 'vnbox\.py test' 'scripts/play\.sh' 'scripts/stream\.py' '^Xvfb :'; do
  for p in $(pgrep -f "$pat"); do
    [ "$p" = "$me" ] || [ "$p" = "$parent" ] && continue
    d=$(tr '\0' ' ' </proc/$p/cmdline 2>/dev/null | grep -oE '^Xvfb :[0-9]+' | cut -d' ' -f2); [ -n "$d" ] && grep -qx "$d" <<<"$P" && continue
    kill "$p" 2>/dev/null
  done
done
echo stopped

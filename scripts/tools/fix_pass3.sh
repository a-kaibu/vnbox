#!/bin/bash
# fix_pass.sh <names.txt>  未解決タイトルを「取得 → add_game でパッケージ化 → 起動戦略を順に試す」で再試行する
#   戦略: vnbox (長め待機 + スキップ操作) → 従来 32bit Wine → vnbox + Vulkan 描画
#   結果: results/batch/summary_fix.tsv (name, 戦略, verdict)、スクショは results/fix_<name>_<戦略>/ と results/batch/fix_<name>.png
cd "$(dirname "$0")/../.."; V=$PWD
OUT=results/batch/summary_fix3.tsv
PROBE="shot:t60 pass:6 sleep:15 shot:t1 click:640,400 sleep:6 key:Return sleep:10 shot:t2 click:640,400 sleep:6 key:Return sleep:8 shot:c"
url_of(){ python3 -I - "$1" <<'PY'
import sys,glob
n=sys.argv[1]
for f in sorted(glob.glob('/home/user/vnbox/downloads/suezou/picked*.tsv')):
    for l in open(f,encoding='utf-8'):
        y,b,_,u=l.rstrip('\n').split('\t')[:4]
        if f"b{y}_{b.replace('-','_')}"==n: print(u); sys.exit()
PY
}
while read -r n; do
  [ -z "$n" ] && continue
  grep -q "^$n	" $OUT 2>/dev/null && continue
  mkdir results/batch/$n.fix3lock 2>/dev/null || continue
  echo "=== $n"
  PK=$V/packages/fix_$n; rm -rf "$PK"
  if [ -d games/$n ]; then SRC=$V/games/$n
  else
    u=$(url_of $n); [ -z "$u" ] && { echo -e "$n\t-\tno_url" >> $OUT; continue; }
    mkdir -p downloads/fix/$n; SRC=$V/downloads/fix/$n/$(basename "$u")
    curl -sSL --retry 3 -m 3600 -o "$SRC" "$u" || { echo -e "$n\t-\tDL_FAIL" >> $OUT; continue; }
  fi
  UPLOAD=0 WINE_FLAVOR=vnbox DISPLAY=:$((100 + $(echo -n "add-$n" | cksum | cut -d' ' -f1) % 400)) \
    timeout 3600 bin/vnbox add fix_$n "$SRC" -no-upload > results/batch/fix_$n.add.log 2>&1
  rm -rf "$V/downloads/fix/$n" "$V/games/$n"
  T=$(sed -n 's/^target=//p' "$PK/launch.ini" 2>/dev/null)
  if [ -z "$T" ]; then echo -e "$n\t-\tNG:package($(tail -1 results/batch/fix_$n.add.log | cut -c1-80))" >> $OUT; rm -rf "$PK"; continue; fi
  v=NG; best=""
  for st in vnbox classic vulkan; do
    R=fix_${n}_$st; P=$V/prefixes/run-$R; rm -rf "$P"
    case $st in vnbox|vulkan) FL=vnbox;; classic) FL=;; esac
    WINE_FLAVOR=$FL PREPARE_ONLY=1 scripts/launch.sh "$PK" "$P" >/dev/null 2>&1 || { echo "prepare failed $st"; continue; }
    [ $st = vulkan ] && WINE_FLAVOR=vnbox bash -c "source scripts/env.sh; WINEPREFIX=$P WINEARCH=win64 DISPLAY=:61 wine reg add 'HKCU\\Software\\Wine\\Direct3D' /v renderer /d vulkan /f >/dev/null 2>&1; WINEPREFIX=$P wineserver -w"
    WINE_FLAVOR=$FL WINEPREFIX_OVERRIDE=$P INPUT="$PROBE" timeout 900 scripts/run.sh $R "$P/drive_c/$(dirname "$T")" "$(basename "$T")" 60 >/dev/null 2>&1
    WINEPREFIX=$P wineserver -k 2>/dev/null; v=$(scripts/tools/verdict.sh $R); best=$st
    rm -rf "$P"
    [ "$v" = OK ] && break
  done
  scripts/tools/sheet.sh results/batch/fix_$n.png $(for st in vnbox classic vulkan; do [ -d results/fix_${n}_$st ] && echo fix_${n}_$st; done)
  echo -e "$n\t$best\t$v\t$T" | tee -a $OUT
  rm -rf "$PK"
done < "$1"
echo FIX_DONE

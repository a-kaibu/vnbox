#!/bin/bash
# recheck6.sh <list.tsv> [並列数]  第6弾の再試行 (操作列 PROBE を指定): 取得 → vnbox add (Go) → 起動 → スクショ → 判定 → 片付け
#   list.tsv: id, url, 題名, 出典, DRM(製品版), サイズMB (scripts/tools/pick6.py の出力)
#   結果: results/batch/summary6.tsv (id, 戦略, 判定, 起動 exe, エンジン, 出典, DRM 痕跡)、一覧画像 results/batch/b6_<id>.png
#   NG なら従来 Wine (Win7) でもう一度起動してみる。空き容量が 6GB を切ったら空くまで待つ
cd "$(dirname "$0")/../.."; V=$PWD
LIST=$1; JOBS=${2:-3}
OUT=results/batch/summary_re6.tsv
PROBE=${PROBE:-"shot:t40 pass:6 sleep:12 shot:t1 sleep:12 shot:t2 click:640,400 sleep:5 key:Return sleep:8 shot:t3 click:640,400 sleep:5 key:Return sleep:8 shot:c"}

# DRM の痕跡 (配布物の exe/dll 内の文字列とファイル名)
drm_scan(){ local d=$1 hits=""
  find "$d" -type f \( -iname '*.exe' -o -iname '*.dll' \) -size -80M 2>/dev/null | head -60 | while read -r f; do
    LC_ALL=C grep -a -o -i -E 'soft-?denchi|sdenchi|paltio|PlayDRM|dlsite ?drm|DMMGamePlayer|dmmgame|Software ?Defender|StarForce|SecuROM|Themida|WinLicense|Enigma ?protector|VMProtect|ProRing|Alpha-?ROM' "$f" 2>/dev/null
  done | tr 'A-Z' 'a-z' | sed 's/ //g' | sort -u | tr '\n' ',' ; }

one(){ # タブ区切りの空欄を詰めないよう cut で取り出す ("read" は連続したタブを 1 つとみなす)
  local f; f(){ printf '%s\n' "$1" | cut -f"$2"; }
  local id=$(f "$1" 1) url=$(f "$1" 2) title=$(f "$1" 3) src=$(f "$1" 4) drm=$(f "$1" 5) mb=$(f "$1" 6)
  grep -q "^$id	" $OUT 2>/dev/null && return
  mkdir results/batch/$id.re6lock 2>/dev/null || return
  while [ "$(df --output=avail -B1G / | tail -1 | tr -d ' ')" -lt 6 ]; do sleep 30; done
  local D=$V/downloads/r6/$id; mkdir -p $D; local F=$D/$(basename "${url%%\?*}")
  # 先読み (prefetch6.sh) 済みならそれを使う。取得中なら終わるのを待つ
  local PF=$V/downloads/r6pre/$id
  while [ -d $PF ] && [ ! -e $PF/.done ] && [ ! -e $PF/.fail ]; do sleep 10; done
  if [ -e $PF/.done ]; then mv "$PF/$(basename "$F")" "$F"; rm -rf $PF
  else rm -rf $PF; curl -sSL --retry 3 -m 3600 -A Mozilla/5.0 -o "$F" "$url" || { echo -e "$id\t-\tDL_FAIL\t\t\t$src\t$drm" >> $OUT; rm -rf $D; return; }
  fi
  local PK=$V/packages/$id; rm -rf "$PK"
  local targs=(); [ -n "$title" ] && targs=(-title "$title")
  timeout 3600 bin/vnbox add $id "$F" -no-upload "${targs[@]}" > results/batch/r6_$id.add.log 2>&1
  rm -rf $D
  local T=$(sed -n 's/^target=//p' "$PK/launch.ini" 2>/dev/null) E=$(sed -n 's/^engine=//p' "$PK/launch.ini" 2>/dev/null)
  if [ -z "$T" ]; then
    echo -e "$id\t-\tNG:package($(grep -v '^ ' results/batch/r6_$id.add.log | tail -1 | cut -c1-70))\t\t\t$src\t$drm" >> $OUT; rm -rf "$PK"; return
  fi
  local marks=$(drm_scan "$PK/files")
  local v=NG best="" st
  for st in vnbox classic; do
    local R=r6_${id}_$st P=$V/prefixes/run-r6_${id}_$st FL=vnbox; [ $st = classic ] && FL=
    rm -rf "$P"
    WINE_FLAVOR=$FL PREPARE_ONLY=1 scripts/launch.sh "$PK" "$P" >/dev/null 2>&1 || continue
    WINE_FLAVOR=$FL WINEPREFIX_OVERRIDE=$P INPUT="$PROBE" timeout 900 scripts/run.sh $R "$P/drive_c/$(dirname "$T")" "$(basename "$T")" 45 >/dev/null 2>&1
    WINEPREFIX=$P wineserver -k 2>/dev/null
    v=$(scripts/tools/verdict.sh $R); best=$st
    local d=$(cat results/$R/display.txt 2>/dev/null); [ -n "$d" ] && pkill -f "^Xvfb $d " 2>/dev/null
    rm -rf "$P"
    [ "$v" = OK ] && break
  done
  scripts/tools/sheet.sh results/batch/r6_$id.png $(for st in vnbox classic; do [ -d results/r6_${id}_$st ] && echo r6_${id}_$st; done)
  echo -e "$id\t$best\t$v\t$T\t$E\t$src\t$drm\t$marks" | tee -a $OUT
  rm -rf "$PK" "$V/downloads/r6pre/$id"
}
export -f one drm_scan; export V OUT PROBE
mkdir -p results/batch downloads/b6
cut -f1-6 "$LIST" | tr '\n' '\0' | xargs -0 -P "$JOBS" -I{} bash -c 'one "$1"' _ {}
echo RECHECK6_DONE

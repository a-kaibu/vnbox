#!/bin/bash
# batch.sh <picked.tsv>  年度/ブランド/サイズ/URL の一覧を順に: DL -> 展開 or 無人インストール -> 判定 -> プローブ
cd "$(dirname "$0")/../.."; source scripts/env.sh
[ "${WINE_FLAVOR:-}" = vnbox ] && export BASE=$VNBOX/prefixes/base-vnbox
export PROBE="shot:t25 pass:5 sleep:10 shot:t1 sleep:12 shot:t2 click:640,400 sleep:3 key:Return sleep:4 shot:c"
LIST=$1; [ "${REVERSE:-}" = 1 ] && { tac "$1" > "$1.rev"; LIST=$1.rev; }; OUTT=results/batch/summary.tsv; mkdir -p results/batch
while IFS=$'\t' read -r year brand mb url; do
  name="b${year}_${brand//-/_}"; [ -f results/batch/$name.done ] && continue
  mkdir results/batch/$name.lock 2>/dev/null || continue   # 並列実行時の重複防止
  f=downloads/batch/$name/$(basename "$url"); mkdir -p "$(dirname "$f")"
  echo "=== $name ($mb MB) $url"
  curl -sSL --retry 3 -m 3600 -o "$f" "$url" || { echo -e "$name\t$year\t$brand\tDL_FAIL" >> $OUTT; touch results/batch/$name.done; continue; }
  mode=extract; G=games/$name; rm -rf "$G"
  case "${f,,}" in
    *.exe)
      kind=$(python3 -I scripts/detect.py "$f" | python3 -I -c 'import json,sys;t=json.load(sys.stdin)["exes"][0]["hits"];print("inno" if "Installer:InnoSetup" in t else "nsis" if "Installer:NSIS" in t else "other")')
      if scripts/extract.sh "$f" "$G" >/dev/null 2>&1 && [ $(find "$G" -iname '*.exe' | wc -l) -gt 0 ] && [ "$kind" != inno ] && [ "$kind" != nsis ]; then mode=sfx
      elif [ "$kind" = inno ] || [ "$kind" = nsis ]; then
        mode=install_$kind; rm -rf "$G"; scripts/capture.sh begin $name >/dev/null
        export WINEPREFIX=$PWD/prefixes/$name DISPLAY=${BATCH_DISP:-:60}; ensure_x
        flags=$([ $kind = inno ] && echo "/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /SP-" || echo "/S")
        WINEDEBUG=-all timeout 900 wine "$f" $flags >/dev/null 2>&1; WINEDEBUG=-all wineserver -w
        scripts/capture.sh end $name > results/batch/$name.capture.txt 2>&1; unset WINEPREFIX
        G=packages/$name
      else mode=manual_installer; fi;;
    *) scripts/extract.sh "$f" "$G" >/dev/null 2>&1 || mode=extract_fail;;
  esac
  # 展開結果が動画だけ -> 対象外 / 中身がインストーラ -> 自動インストール
  if [ $mode = extract ] || [ $mode = sfx ]; then
    if [ $(find "$G" -iname '*.exe' | wc -l) -eq 0 ]; then mode=not_game
    elif find "$G" -maxdepth 3 \( -iname '*.msi' -o -iname 'data1.hdr' \) | grep -q . || \
         { [ "$(python3 -I scripts/detect.py "$G" | python3 -I -c 'import json,sys;print(json.load(sys.stdin)["engine"])')" = unknown ] && find "$G" -maxdepth 3 -iname 'setup.exe' | grep -q .; }; then
      d=$(dirname "$(find "$G" -maxdepth 3 \( -iname '*.msi' -o -iname 'setup.exe' -o -iname 'data1.hdr' \) | head -1)")
      if DISPLAY=${BATCH_DISP:-:60} scripts/autoinstall.sh $name "$d" > results/batch/$name.capture.txt 2>&1; then mode=autoinstall; rm -rf "$G"; G=packages/$name; else mode=manual_installer; fi
    fi
  fi
  det=$(python3 -I scripts/detect.py "$G" 2>/dev/null | python3 -I -c 'import json,sys
try:
  d=json.load(sys.stdin); print(d["engine"], ",".join(d["tags"]) or "-", (d["exes"][0]["arch"] if d["exes"] else "-"), sep="\t")
except Exception: print("unknown\t-\t-")')
  status=""
  case $mode in
    manual_installer|extract_fail|not_game) status=$mode;;
    install_*|autoinstall) T=$(sed -n 's/^target=//p' $G/launch.ini 2>/dev/null)
      if [ -n "$T" ]; then PREPARE_ONLY=1 scripts/launch.sh $G >/dev/null 2>&1
        WINEPREFIX_OVERRIDE=$PWD/prefixes/run-$name INPUT="$PROBE" \
          scripts/run.sh $name "prefixes/run-$name/drive_c/$(dirname "$T")" "$(basename "$T")" 30 > /dev/null 2>&1; status=probed
      else status=install_no_exe; fi;;
    *) WAIT=30 INPUT="$PROBE" scripts/probe.sh $name > /dev/null 2>&1; status=probed;;
  esac
  [ -d results/$name ] && montage results/$name/0*.png -tile 4x -geometry 320x200+2+2 -background '#222' results/batch/$name.png 2>/dev/null
  win=$(grep -m1 '^windows=' results/$name/summary.txt 2>/dev/null | cut -d= -f2-)
  verdict=$(scripts/tools/verdict.sh $name 2>/dev/null)
  echo -e "$name\t$year\t$brand\t$mode\t$det\t$status\t$verdict\t$win" | tee -a $OUTT
  rm -f "$f"; WINEPREFIX=$VNBOX/prefixes/$name wineserver -k 2>/dev/null; WINEPREFIX=$VNBOX/prefixes/run-$name wineserver -k 2>/dev/null
  touch results/batch/$name.done
  echo "disk: $(df -h / | awk 'NR==2{print $4}')"
done < "$LIST"
echo BATCH_DONE

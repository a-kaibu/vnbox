#!/bin/bash
# install_pass.sh <picked.tsv...>
# summary.tsv で manual_installer になったタイトルを、インストーラ GUI 自動操作 (wizard.sh) で入れ直して判定する
cd "$(dirname "$0")/../.."; source scripts/env.sh
export WINE_FLAVOR=vnbox; source scripts/env.sh; export BASE=$VNBOX/prefixes/base-vnbox
PROBE="shot:t25 pass:5 sleep:10 shot:t1 sleep:12 shot:t2 click:640,400 sleep:3 key:Return sleep:4 shot:c"
OUT=results/batch/summary_install.tsv
for name in $( { awk -F'\t' '$4=="manual_installer"{print $1}' results/batch/summary.tsv; for e in ${EXTRA:-}; do echo $e; done; } | sort -u); do
  grep -q "^$name	" $OUT 2>/dev/null && continue
  year=${name:1:4}; brand=${name:6}
  url=$(cat "$@" | awk -F'\t' -v y=$year -v b="$brand" '$1==y { g=$2; gsub("-","_",g); if (g==b) print $4 }' | head -1)
  [ -z "$url" ] && { echo -e "$name\tno_url" >> $OUT; continue; }
  echo "=== $name $url"
  d=downloads/manual/$name; mkdir -p $d; f=$d/$(basename "$url")
  curl -sSL --retry 3 -m 3600 -o "$f" "$url" || { echo -e "$name\tDL_FAIL" >> $OUT; continue; }
  src=$f
  case "${f,,}" in *.zip|*.lzh|*.7z|*.rar)   # 展開した中にインストーラがある
    scripts/extract.sh "$f" $d/x >/dev/null 2>&1
    src=$(find $d/x -maxdepth 3 \( -iname 'setup.exe' -o -iname '*install*.exe' -o -iname '*インストール*.exe' \) | head -1)
    [ -z "$src" ] && src=$(find $d/x -maxdepth 2 -iname '*.exe' | head -1);;
  esac
  scripts/capture.sh begin $name >/dev/null
  export WINEPREFIX=$VNBOX/prefixes/$name WINEARCH=win64 DISPLAY=:60 WINEDEBUG=-all; ensure_x
  (cd "$(dirname "$src")" && timeout 1500 wine "$(basename "$src")" >/dev/null 2>&1 &)
  sleep 15; scripts/wizard.sh results/batch/wiz_$name 60 > results/batch/wiz_$name.txt 2>&1
  sleep 5; wineserver -k 2>/dev/null; unset WINEPREFIX
  scripts/capture.sh end $name > results/batch/$name.capture.txt 2>&1
  T=$(sed -n 's/^target=//p' packages/$name/launch.ini 2>/dev/null)
  if [ -n "$T" ]; then
    PREPARE_ONLY=1 scripts/launch.sh packages/$name >/dev/null 2>&1
    WINEPREFIX_OVERRIDE=$VNBOX/prefixes/run-$name INPUT="$PROBE" scripts/run.sh $name "prefixes/run-$name/drive_c/$(dirname "$T")" "$(basename "$T")" 30 >/dev/null 2>&1
    v=$(scripts/tools/verdict.sh $name)
    montage results/$name/0*.png -tile 4x -geometry 320x200+2+2 -background '#222' results/batch/$name.png 2>/dev/null
  else v="NG:install_no_exe"; fi
  echo -e "$name\t$v\t$T" | tee -a $OUT
  rm -rf "$d"; scripts/stopall.sh >/dev/null 2>&1
  [ "$v" = OK ] && rm -rf "$VNBOX/packages/$name" "$VNBOX/prefixes/run-$name"
  rm -rf "$VNBOX/prefixes/$name"
done
echo INSTALL_PASS_DONE

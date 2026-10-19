#!/bin/bash
# sheet.sh <out.png> <name>...  各タイトルの最後のスクショ (と1つ前) を名前付きで並べた一覧を作る
OUT=$1; shift; T=$(mktemp -d); i=0
for n in "$@"; do
  s=($(ls /home/user/vnbox/results/$n/0*.png 2>/dev/null)); k=${#s[@]}; [ $k -eq 0 ] && continue
  a=${s[$((k-1))]}; b=${s[$((k>2?k-2:0))]}; i=$((i+1))
  convert \( "$b" -resize 300x170 -background '#222' -gravity center -extent 300x170 \) \( "$a" -resize 300x170 -background '#222' -gravity center -extent 300x170 \) +append \
    -gravity south -background '#000' -fill yellow -pointsize 15 -splice 0x20 -annotate +0+2 "$n" $T/$(printf %03d $i).png
done
montage $T/*.png -tile 2x -geometry +3+3 -background '#111' "$OUT"; rm -rf $T

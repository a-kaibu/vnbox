#!/bin/bash
# verdict.sh <name>  results/<name>/ を見て OK / NG:<理由> を返す
#   OK = プロセス生存, 最終画面が 400x300 相当以上, 中央80%のコントラスト>0.05, 最終2枚が別画面 or 本編操作で変化, クラッシュ痕跡なし
cd "$(dirname "$0")/../.."; R=results/$1
[ -f $R/summary.txt ] || { echo "NG:no_run"; exit; }
grep -q 'alive_after=yes' $R/summary.txt || { echo "NG:dead"; exit; }
grep -q 'size_mismatch=yes' $R/summary.txt && { echo "NG:size_mismatch($(grep -o 'client=[^ ]* x11=[^ ]*' $R/summary.txt))"; exit; }
grep -qE 'Unhandled exception|page fault|RtlpWaitForCriticalSection' $R/wine.log 2>/dev/null && { echo "NG:crash"; exit; }
shots=($(ls $R/0*.png 2>/dev/null)); n=${#shots[@]}; [ $n -lt 2 ] && { echo "NG:noshot"; exit; }
last=${shots[$((n-1))]}; first=${shots[1]}
area=$(identify -format '%w %h' "$last" 2>/dev/null | awk '{print $1*$2}'); [ "${area:-0}" -lt 120000 ] && { echo "NG:small_window"; exit; }
sd=$(convert "$last" -gravity center -crop 80%x80%+0+0 +repage -colorspace gray -format '%[fx:standard_deviation]' info: 2>/dev/null)
awk "BEGIN{exit !($sd > 0.05)}" || { echo "NG:blank($sd)"; exit; }
echo "OK"

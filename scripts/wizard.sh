#!/bin/bash
# wizard.sh <outdir> [最大ステップ]  DISPLAY 上のインストーラ系ダイアログを既定ボタンで進め続ける
# ダイアログ = ゲーム本体らしくない小さめのウィンドウ。各ステップのスクショを outdir に保存
# 押し方: Enter → (画面が変わらなければ) Space → Alt+N(次へ) → Alt+I(インストール) → Alt+Y(はい) → Alt+F(完了)
OUT=$1; MAX=${2:-30}; mkdir -p "$OUT"; idle=0; stuck=0
shot_hash(){ eval $(xdotool getwindowgeometry --shell $1 2>/dev/null) || return; import -window root -crop ${WIDTH}x${HEIGHT}+$X+$Y +repage png:- 2>/dev/null | md5sum | cut -c1-16; }
for i in $(seq -w 1 $MAX); do
  sleep 3
  best=""; for w in $(xdotool search --onlyvisible --name . 2>/dev/null); do
    eval $(xdotool getwindowgeometry --shell $w 2>/dev/null); n=$(xdotool getwindowname $w 2>/dev/null)
    [[ "$n" == "Default IME" || -z "$n" ]] && continue
    [ "$WIDTH" -lt 900 ] && [ "$HEIGHT" -lt 700 ] && best="$w"   # 後に見つかった(新しい)小ウィンドウを優先
  done
  if [ -z "$best" ]; then idle=$((idle+1)); [ $idle -ge 4 ] && { echo "no dialog, done"; break; }; continue; fi
  idle=0; eval $(xdotool getwindowgeometry --shell $best)
  import -window root -crop ${WIDTH}x${HEIGHT}+$X+$Y +repage "$OUT/wiz_$i.png" 2>/dev/null
  echo "$i [$(xdotool getwindowname $best)] ${WIDTH}x$HEIGHT"
  before=$(shot_hash $best)
  for k in Return space alt+n alt+i alt+y alt+f; do
    xdotool windowfocus $best 2>/dev/null; sleep 0.3; xdotool key $k; sleep 2
    xdotool search --onlyvisible --name . 2>/dev/null | grep -qx "$best" || break   # 閉じた
    [ "$(shot_hash $best)" != "$before" ] && break                                   # 次の画面へ進んだ
  done
  # どのキーでも変わらない画面 (進捗表示など) が続くなら待つだけにする
  [ "$(shot_hash $best 2>/dev/null)" = "$before" ] && stuck=$((stuck+1)) || stuck=0
  [ $stuck -ge 10 ] && { echo "dialog does not react, giving up"; break; }
done

#!/bin/bash
# 使い方: run.sh <name> <game_dir> <exe(相対)> [待機秒]
#   INPUT="click:180,387 sleep:3 key:Return shot:start ..."  で操作列を指定 (座標はゲームウィンドウ基準)
#   既定の操作列は「中央クリック+Enter」×6
set -uo pipefail
source "$(dirname "$0")/env.sh"
NAME=$1; GDIR=$2; EXE=$3; WAIT=${4:-20}
OUT=$VNBOX/results/$NAME; rm -rf "$OUT"; mkdir -p "$OUT"
export WINEPREFIX=${WINEPREFIX_OVERRIDE:-$VNBOX/prefixes/$NAME}
# exe が 64bit ならベースも 64bit
BASE=$VNBOX/prefixes/base; [ "$("$VNBOX/scripts/pearch.sh" "$GDIR/$EXE")" = x64 ] && { BASE=$VNBOX/prefixes/base64; export WINEARCH=win64; }
# 自前ビルドは新 WoW64 (32bit アプリも 64bit prefix で動く) なので専用ベースを使う
[[ "${WINE_FLAVOR:-}" == vnbox* ]] && { BASE=$VNBOX/prefixes/base-vnbox; export WINEARCH=win64; }
[ -d "$WINEPREFIX" ] || clone_prefix "$BASE" "$WINEPREFIX"
grep -q '#arch=win64' "$WINEPREFIX/system.reg" && export WINEARCH=win64
# 1ゲーム = 1 X ディスプレイ (WM なし)。配信時もこのディスプレイ/ウィンドウだけを取り込む
# 並列実行で別のゲームと同じディスプレイにならないよう、使われていない番号まで進める
D=$((100 + $(echo -n "$NAME" | cksum | cut -d' ' -f1) % 400)); for _ in $(seq 400); do [ -e /tmp/.X$D-lock ] || break; D=$((100 + (D - 99) % 400)); done
export DISPLAY=:$D; echo ":$D" > "$OUT/display.txt"
ensure_x; ensure_audio
export WINEDEBUG=${RUN_DEBUG:-err+all,fixme-all}
# .NET (Core) を入れた prefix では mscoree を止めない (IL だけの DLL が読めなくなる)
[ -d "$WINEPREFIX/drive_c/Program Files/dotnet" ] && export WINEDLLOVERRIDES="mshtml=;winemenubuilder.exe=d"
# パッケージの起動引数 (launch.sh が prefix に書いたもの) を、指定が無ければ使う
[ -z "${ARGS:-}" ] && [ -s "$WINEPREFIX/.vnbox-args" ] && ARGS=$(cat "$WINEPREFIX/.vnbox-args")
# ゲームのプロセスが生きているか。exe 名は正規表現として扱わず (「(体験版).exe」等)、
# 並列実行中の別 prefix の同名 exe (Game.exe 等) と取り違えないよう WINEPREFIX も確かめる
game_alive(){ local p pat; pat=$(basename "$EXE" | sed 's/[][\.*^$()+?{}|]/\\&/g')
  for p in $(pgrep -f -- "$pat"); do tr '\0' '\n' </proc/$p/environ 2>/dev/null | grep -qxF "WINEPREFIX=$WINEPREFIX" && return 0; done; return 1; }
cd "$GDIR"
wine "$EXE" ${ARGS:-} >"$OUT/wine.log" 2>&1 &
PID=$!
sleep "$WAIT"
# フルスクリーン化を WM 任せにするゲームは、WM が無いと X ウィンドウが 1x1 のまま残る。
# 検出したら Wine 自身にウィンドウ管理させる (X11 Driver Managed=N) 設定で起動し直す
big=$(for w in $(xdotool search --onlyvisible --name . 2>/dev/null); do eval $(xdotool getwindowgeometry --shell $w 2>/dev/null); echo $((WIDTH*HEIGHT)); done | sort -n | tail -1)
if [ "${big:-0}" -le 1 ] && [ -z "${NO_MANAGED_FALLBACK:-}" ]; then   # 起動が遅いだけのゲームもあるので 20 秒待って再確認
  sleep 20; big=$(for w in $(xdotool search --onlyvisible --name . 2>/dev/null); do eval $(xdotool getwindowgeometry --shell $w 2>/dev/null); echo $((WIDTH*HEIGHT)); done | sort -n | tail -1)
fi
if [ "${big:-0}" -le 1 ] && game_alive && [ -z "${NO_MANAGED_FALLBACK:-}" ]; then
  echo "managed_fallback=1" >> "$OUT/fallback.txt"
  wineserver -k 2>/dev/null; sleep 1
  wine reg add 'HKCU\Software\Wine\X11 Driver' /v Managed /d N /f >/dev/null 2>&1; wineserver -w
  wine "$EXE" ${ARGS:-} >>"$OUT/wine.log" 2>&1 &
  PID=$!; sleep "$WAIT"
fi
# ゲームウィンドウ (Wine デスクトップ以外で最大のもの) を探す
gamewin(){ for w in $(xdotool search --onlyvisible --name . 2>/dev/null); do
  n=$(xdotool getwindowname $w 2>/dev/null); [[ "$n" == *"デスクトップ"* || "$n" == *Desktop* || -z "$n" ]] && continue
  eval $(xdotool getwindowgeometry --shell $w 2>/dev/null); echo "$((WIDTH*HEIGHT)) $w"; done | sort -rn | head -1 | cut -d' ' -f2; }
W=$(gamewin)
# スクショは画面全体からゲームウィンドウ矩形を切り出す (D3D/GL ウィンドウは -window 直撮りだと黒くなることがある)
n=0; shot(){ n=$((n+1)); local W2 X Y WIDTH HEIGHT; W2=$(gamewin)
  if [ -n "$W2" ]; then eval $(xdotool getwindowgeometry --shell $W2)
    import -window root -display "$DISPLAY" -crop ${WIDTH}x${HEIGHT}+$X+$Y +repage "$OUT/$(printf %02d $n)_$1.png" 2>/dev/null
  else import -window root -display "$DISPLAY" "$OUT/$(printf %02d $n)_$1.png" 2>/dev/null; fi; }
shot boot
SEQ=${INPUT:-"click:400,300 key:Return click:400,300 key:Return click:400,300 key:Return shot:a click:400,300 key:Return click:400,300 key:Return click:400,300 key:Return shot:b"}
for op in $SEQ; do
  case $op in
    click:*) xy=${op#click:}; W=$(gamewin)
             if [ -n "$W" ]; then xdotool mousemove --window "$W" ${xy%,*} ${xy#*,}; else xdotool mousemove ${xy%,*} ${xy#*,}; fi; sleep 0.4; xdotool mousedown 1; sleep 0.1; xdotool mouseup 1; sleep 1.2;;
    key:*)   W=$(gamewin); [ -n "$W" ] && xdotool windowfocus "$W" 2>/dev/null; xdotool key ${op#key:} 2>/dev/null; sleep 1.2;;
    sleep:*) sleep ${op#sleep:};;
    pass:*)  # 起動設定などの小さいダイアログが最前面なら Enter → 効かなければ Space で抜ける (最大 N 回)
             for _ in $(seq ${op#pass:}); do
               W=$(gamewin); [ -z "$W" ] && { sleep 3; continue; }
               eval $(xdotool getwindowgeometry --shell $W 2>/dev/null)
               [ $((WIDTH*HEIGHT)) -ge 400000 ] && break
               xdotool windowfocus $W 2>/dev/null; sleep 0.3; xdotool key Return 2>/dev/null; sleep 4
               [ "$(gamewin)" = "$W" ] && { xdotool windowfocus $W 2>/dev/null; sleep 0.2; xdotool key space 2>/dev/null; sleep 4; }
             done;;
    shot:*)  shot ${op#shot:};;
    rec:*)   # rec:<秒> ゲームウィンドウを 30fps で録画し、ユニークフレーム数を数える
             W2=$(gamewin); eval $(xdotool getwindowgeometry --shell $W2)
             ffmpeg -v error -y -f x11grab -framerate 30 -video_size ${WIDTH}x${HEIGHT} -i "$DISPLAY+$X,$Y" -t ${op#rec:} -c:v libx264 -preset ultrafast -crf 23 "$OUT/rec.mp4"
             U=$(ffmpeg -i "$OUT/rec.mp4" -vf mpdecimate=hi=64*12:lo=64*5:frac=0.33 -f null - 2>&1 | grep -oE 'frame= *[0-9]+' | tail -1 | grep -oE '[0-9]+')
             T=$(ffprobe -v error -count_frames -select_streams v -show_entries stream=nb_read_frames -of csv=p=0 "$OUT/rec.mp4")
             echo "rec: unique_frames=$U / total=$T (${op#rec:}s)" | tee -a "$OUT/rec.txt";;
  esac
done
ALIVE=no; game_alive && ALIVE=yes
WINS=$(xdotool search --onlyvisible --name . getwindowname %@ 2>/dev/null | grep -v '^$' | sort -u | tr '\n' '|')
# Wine から見たウィンドウの描画領域 (client) と X のウィンドウの大きさを比べる。X の方が小さいと画面の右下が切れて見える
# (WM が最大化・全画面の要求を実行しなかった等)。いちばん大きいウィンドウで 16px 以上小さければ size_mismatch=yes
MISMATCH=no
[ -f "$VNBOX/runtime/tools/winsize.exe" ] || { mkdir -p "$VNBOX/runtime/tools"; i686-w64-mingw32-gcc -O1 -o "$VNBOX/runtime/tools/winsize.exe" "$VNBOX/tools/winsize/winsize.c" -luser32 2>/dev/null; }
if [ -f "$VNBOX/runtime/tools/winsize.exe" ] && [ "$ALIVE" = yes ]; then
  wine "$VNBOX/runtime/tools/winsize.exe" > "$OUT/winsize.txt" 2>/dev/null
  read -r cw ch < <(awk '{print $5*$6, $5, $6}' "$OUT/winsize.txt" | sort -n | tail -1 | cut -d' ' -f2-)
  W2=$(gamewin); [ -n "$W2" ] && eval $(xdotool getwindowgeometry --shell $W2 2>/dev/null)
  echo "client=${cw:-?}x${ch:-?} x11=${WIDTH:-?}x${HEIGHT:-?}" > "$OUT/sizes.txt"
  if [ -n "${cw:-}" ] && [ -n "${WIDTH:-}" ] && { [ $((cw - WIDTH)) -gt 16 ] || [ $((ch - HEIGHT)) -gt 16 ]; }; then MISMATCH=yes; fi
fi
wineserver -k 2>/dev/null; wait $PID 2>/dev/null
for p in $(pgrep -x Xvfb); do tr '\0' ' ' </proc/$p/cmdline | grep -q "^Xvfb $DISPLAY " && kill $p; done
{
  echo "name=$NAME exe=$EXE alive_after=$ALIVE size_mismatch=$MISMATCH $(cat "$OUT/sizes.txt" 2>/dev/null)"
  echo "windows=$WINS"
  echo "--- top errors ---"
  grep -E ':err:|Unhandled|not found|could not load|Could not' "$OUT/wine.log" | grep -v winebth \
    | sed -E 's/^[0-9a-f]+://' | sort | uniq -c | sort -rn | head -12
} | tee "$OUT/summary.txt"

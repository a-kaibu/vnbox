#!/bin/bash
# 1ゲーム = 1 X ディスプレイ + 1 音声シンク + 1 配信ポート で起動する
#   play.sh <name> <game_dir> <exe(相対)> [port]       例: play.sh saku games/saku/saku_trial うみねこのなく頃に.exe 8081
#   play.sh --package packages/<name> [port]
#   ブラウザで http://<server>:<port>/ を開く
set -euo pipefail
source "$(dirname "$0")/env.sh"
[ -f "$VNBOX/scripts/store.conf" ] && source "$VNBOX/scripts/store.conf"
if [ "$1" = --package ]; then
  # packages/<name> が手元に無く VNBOX_S3 があれば S3 から取ってくる
  [ -f "$2/launch.ini" ] || { [ -n "${VNBOX_S3:-}" ] && "$VNBOX/scripts/store.sh" pull-package "$(basename "$2")"; }
  PKG=$(realpath "$2"); NAME=$(basename "$PKG"); PORT=${3:-8080}
  PREPARE_ONLY=1 "$VNBOX/scripts/launch.sh" "$PKG" >/dev/null
  export WINEPREFIX=$VNBOX/prefixes/run-$NAME; T=$(sed -n 's/^target=//p' "$PKG/launch.ini")
  GDIR=$WINEPREFIX/drive_c/$(dirname "$T"); EXE=$(basename "$T"); ARGS=$(sed -n 's/^args=//p' "$PKG/launch.ini")
else
  NAME=$1; GDIR=$(realpath "$2"); EXE=$3; PORT=${4:-8080}; ARGS=${ARGS:-}
  export WINEPREFIX=$VNBOX/prefixes/$NAME
  BASE=$VNBOX/prefixes/base; [ "$("$VNBOX/scripts/pearch.sh" "$GDIR/$EXE")" = x64 ] && BASE=$VNBOX/prefixes/base64
# 自前ビルドは新 WoW64 (32bit アプリも 64bit prefix で動く) なので専用ベースを使う
[[ "${WINE_FLAVOR:-}" == vnbox* ]] && { BASE=$VNBOX/prefixes/base-vnbox; export WINEARCH=win64; }
  if [ ! -d "$WINEPREFIX" ]; then
    clone_prefix "$BASE" "$WINEPREFIX"
    mkdir -p "$WINEPREFIX/.vnbox-reg0"; cp "$WINEPREFIX/system.reg" "$WINEPREFIX/user.reg" "$WINEPREFIX/.vnbox-reg0/"; touch "$WINEPREFIX/.vnbox-applied"
  fi
fi
grep -q '#arch=win64' "$WINEPREFIX/system.reg" && export WINEARCH=win64
export DISPLAY=:$((100 + $(echo -n "$NAME" | cksum | cut -d' ' -f1) % 400)); ensure_x; ensure_audio
SINK=vn_$(echo -n "$NAME" | md5sum | cut -c1-8)
pactl list short sinks | grep -q "	$SINK	" || pactl load-module module-null-sink sink_name=$SINK sink_properties=device.description=$SINK >/dev/null
export PULSE_SINK=$SINK WINEDEBUG=${WINEDEBUG:--all}
# S3 にセーブがあれば戻してから起動する
[ -n "${VNBOX_S3:-}" ] && "$VNBOX/scripts/store.sh" save-pull "$NAME" "$WINEPREFIX" || true
(cd "$GDIR" && exec wine "$EXE" $ARGS) > "$VNBOX/results/play-$NAME.log" 2>&1 &
GPID=$!
# 1x1 ウィンドウのまま (フルスクリーンを WM 任せにするゲーム) なら Managed=N で起動し直す
sleep 20
big=$(for w in $(xdotool search --onlyvisible --name . 2>/dev/null); do eval $(xdotool getwindowgeometry --shell $w 2>/dev/null); echo $((WIDTH*HEIGHT)); done | sort -n | tail -1)
if [ "${big:-0}" -le 1 ] && kill -0 $GPID 2>/dev/null; then
  echo "1x1 window detected -> Managed=N で再起動"; wineserver -k; sleep 1
  wine reg add 'HKCU\Software\Wine\X11 Driver' /v Managed /d N /f >/dev/null 2>&1; wineserver -w
  (cd "$GDIR" && exec wine "$EXE" $ARGS) >> "$VNBOX/results/play-$NAME.log" 2>&1 &
  GPID=$!
fi
python3 -I "$VNBOX/scripts/stream.py" --display "$DISPLAY" --sink "$SINK" --port "$PORT" &
SPID=$!
echo "playing $NAME  display=$DISPLAY sink=$SINK  ->  http://$(hostname -I 2>/dev/null | awk '{print $1}'):$PORT/"
# プレイ中も定期的に (SAVE_INTERVAL 秒ごと、既定 300) セーブを S3 に上げ、終了時にも上げる
if [ -n "${VNBOX_S3:-}" ]; then
  (while sleep "${SAVE_INTERVAL:-300}"; do "$VNBOX/scripts/store.sh" save-push "$NAME" "$WINEPREFIX" >/dev/null 2>&1 || true; done) &
  BPID=$!
fi
trap 'kill $SPID ${BPID:-} 2>/dev/null; wineserver -k 2>/dev/null; wineserver -w 2>/dev/null; [ -n "${VNBOX_S3:-}" ] && "$VNBOX/scripts/store.sh" save-push "$NAME" "$WINEPREFIX" || true' EXIT INT TERM
wait $GPID

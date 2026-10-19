# 共通環境。source して使う
export VNBOX=${VNBOX:-/home/user/vnbox}
# WINE_FLAVOR=vnbox で自前ビルド (Wine 11.0 + ソフト電池パッチ等, 新WoW64) を使う
if [ "${WINE_FLAVOR:-}" = vnbox ] && [ -x /opt/wine-vnbox/bin/wine ]; then export PATH=/opt/wine-vnbox/bin:$PATH; fi
if [ "${WINE_FLAVOR:-}" = vnbox-test ] && [ -x /opt/wine-vnbox-test/bin/wine ]; then export PATH=/opt/wine-vnbox-test/bin:$PATH; fi
export LANG=ja_JP.UTF-8 LC_ALL=ja_JP.UTF-8
# 国内限定タイトルはタイムゾーンで「日本語版Windows」か判定する (SiglusEngine 系など)。Wine はホストの TZ を使う
export TZ=${TZ:-Asia/Tokyo}
export WINEARCH=${WINEARCH:-win32}
export WINEDEBUG=${WINEDEBUG:--all}
# mono / gecko のインストールダイアログを抑止(必要なタイトルだけ後で入れる)
export WINEDLLOVERRIDES=${WINEDLLOVERRIDES:-"mscoree,mshtml=;winemenubuilder.exe=d"}
export DISPLAY=${DISPLAY:-:99}
ensure_x(){
  if ! xdpyinfo -display "$DISPLAY" >/dev/null 2>&1; then
    # 落ちた Xvfb のロックが残っていると起動できないので掃除
    local n=${DISPLAY#:}; [ -f /tmp/.X$n-lock ] && ! kill -0 "$(tr -d ' ' </tmp/.X$n-lock)" 2>/dev/null && rm -f /tmp/.X$n-lock /tmp/.X11-unix/X$n
    Xvfb "$DISPLAY" -screen 0 ${XVFB_SIZE:-1920x1080}x24 -nolisten tcp >/dev/null 2>&1 &
    for i in $(seq 50); do xdpyinfo -display "$DISPLAY" >/dev/null 2>&1 && break; sleep 0.1; done
    # 最小のウィンドウマネージャ (枠なし)。WM が居ないと Wine の最大化・全画面要求が実行されず、
    # ゲームは 1920x1080 のつもりで描くのに X のウィンドウは小さいまま (右下が切れる / 1x1) になる
    if [ "${VNBOX_WM:-openbox}" = openbox ] && command -v openbox >/dev/null; then
      DISPLAY=$DISPLAY openbox --config-file "$VNBOX/scripts/wm/openbox-rc.xml" >/dev/null 2>&1 &
      sleep 0.5
    fi
  fi
}
ensure_audio(){
  # ヘッドレスでも DirectSound デバイスが存在するように null sink を立てる
  if ! pactl info >/dev/null 2>&1; then
    pulseaudio -D --exit-idle-time=-1 --disallow-exit -n \
      --load="module-null-sink sink_name=null" \
      --load="module-native-protocol-unix" >/dev/null 2>&1 || true
    sleep 1
  fi
}
# ベース prefix をハードリンクで複製 (数MBで済む)。書き換わるレジストリとユーザー領域だけ実体コピー
clone_prefix(){
  local src=$1 dst=$2
  cp -al "$src" "$dst"
  for f in system.reg user.reg userdef.reg; do rm -f "$dst/$f"; cp -a "$src/$f" "$dst/$f"; done
  rm -rf "$dst/drive_c/users"; cp -a "$src/drive_c/users" "$dst/drive_c/users"
}

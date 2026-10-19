#!/bin/bash
# パッケージからゲームを起動する: launch.sh <package_dir> [prefix_dir]
# 初回はベース prefix を複製し、レジストリ断片とファイルを流し込む
set -euo pipefail
source "$(dirname "$0")/env.sh"
PKG=$(realpath "$1"); NAME=$(basename "$PKG")
export WINEPREFIX=${2:-$VNBOX/prefixes/run-$NAME}
ini(){ sed -n "s/^$1=//p" "$PKG/launch.ini" | head -1; }
if [ ! -f "$WINEPREFIX/.vnbox-applied" ]; then
  rm -rf "$WINEPREFIX"; B=${BASE:-$VNBOX/prefixes/base}
  [ -z "${BASE:-}" ] && [ "$("$VNBOX/scripts/pearch.sh" "$PKG/files/$(ini target)" 2>/dev/null)" = x64 ] && B=$VNBOX/prefixes/base64
  [[ "${WINE_FLAVOR:-}" == vnbox* ]] && B=${BASE:-$VNBOX/prefixes/base-vnbox}
  grep -q '#arch=win64' "$B/system.reg" && export WINEARCH=win64
  clone_prefix "$B" "$WINEPREFIX"
  for r in system user; do
    [ -s "$PKG/$r.reg.frag" ] && python3 -I "$VNBOX/scripts/regdiff.py" apply "$PKG/$r.reg.frag" "$WINEPREFIX/$r.reg"
  done
  cp -a --remove-destination "$PKG/files/." "$WINEPREFIX/drive_c/"   # prefix は base の hardlink なので上書きせず置き換える
  R=$(ini renderer); [ -n "$R" ] && wine reg add 'HKCU\Software\Wine\Direct3D' /v renderer /d "$R" /f >/dev/null 2>&1
  # winetricks は system32 の DLL をその場で上書きするので、base と hardlink を共有したままだと base まで書き換わる。先に自前のコピーにする
  unshare(){ local d t; for d in system32 syswow64 Fonts; do t="$WINEPREFIX/drive_c/windows/$d"; [ -d "$t" ] && [ ! -e "$t.vnbox-own" ] || continue
    rm -rf "${t:?}.vnbox-copy"; cp -a "$t" "$t.vnbox-copy" && rm -rf "${t:?}" && mv "$t.vnbox-copy" "$t" && touch "$t.vnbox-own"; done; }
  for v in $(ini winetricks); do   # vnbox-<name> は scripts/verbs/<name>.sh
    case $v in vnbox-*) a=${v#vnbox-}; bash "$VNBOX/scripts/verbs/${a%%:*}.sh" "$WINEPREFIX" $( [[ $a == *:* ]] && tr ':' ' ' <<< "${a#*:}" );; *) unshare; winetricks -q "$v";; esac
  done
  wineserver -w 2>/dev/null || true
  # セーブ (store.sh) は「ここから後に変わったもの」なので、準備直後のレジストリを控えておく
  mkdir -p "$WINEPREFIX/.vnbox-reg0"; cp "$WINEPREFIX/system.reg" "$WINEPREFIX/user.reg" "$WINEPREFIX/.vnbox-reg0/"
  touch "$WINEPREFIX/.vnbox-applied"
fi
# 起動引数は prefix にも書いておく (run.sh で直接起動するときに使う)
printf '%s\n' "$(ini args)" > "$WINEPREFIX/.vnbox-args"
[ -n "${PREPARE_ONLY:-}" ] && { echo "$WINEPREFIX"; exit 0; }
T=$(ini target); cd "$WINEPREFIX/drive_c/$(dirname "$T")"
exec wine "$(basename "$T")" $(ini args)

#!/bin/bash
# autoinstall.sh <name> <setup.exe | .msi | フォルダ>
# インストーラ種別を判定して無人インストールし、差分を packages/<name>/ にする。無人化できない場合は exit 3
set -uo pipefail
cd "$(dirname "$0")/.."; source scripts/env.sh
NAME=$1; SRC=$(realpath "$2")
if [ -d "$SRC" ]; then
  MSI=$(find "$SRC" -maxdepth 2 -iname '*.msi' | head -1)
  SETUP=$(find "$SRC" -maxdepth 2 -iname 'setup.exe' -o -maxdepth 2 -iname 'install*.exe' | head -1)
else MSI=""; SETUP=$SRC; [[ "${SRC,,}" == *.msi ]] && { MSI=$SRC; SETUP=""; }; fi
kind=manual
if [ -n "$MSI" ]; then kind=msi
elif [ -n "$SETUP" ]; then
  hits=$(python3 -I scripts/detect.py "$SETUP" | python3 -I -c 'import json,sys;print(" ".join(json.load(sys.stdin)["exes"][0]["hits"]))')
  case "$hits" in *InnoSetup*) kind=inno;; *NSIS*) kind=nsis;; esac
  if [ $kind = manual ] && [ -d "$SRC" ] && ls "$SRC"/data1.hdr "$SRC"/*/data1.hdr >/dev/null 2>&1; then kind=installshield_cab; fi
fi
echo "[$NAME] installer=$kind"
[ $kind = manual ] && exit 3
scripts/capture.sh begin "$NAME" >/dev/null
export WINEPREFIX=$VNBOX/prefixes/$NAME DISPLAY=${DISPLAY:-:60} WINEDEBUG=-all; ensure_x
case $kind in
  msi)  (cd "$(dirname "$MSI")" && timeout 1200 wine msiexec /i "$(basename "$MSI")" /qn);;
  inno) timeout 1200 wine "$SETUP" /VERYSILENT /SUPPRESSMSGBOXES /NORESTART /SP-;;
  nsis) timeout 1200 wine "$SETUP" /S;;
  installshield_cab)  # レジストリは作れないがファイルは取り出せる
    hdr=$(ls "$SRC"/data1.hdr "$SRC"/*/data1.hdr 2>/dev/null | head -1)
    mkdir -p "$WINEPREFIX/drive_c/Program Files/$NAME" && unshield -d "$WINEPREFIX/drive_c/Program Files/$NAME" x "$(dirname "$hdr")/data1.cab" >/dev/null;;
esac
wineserver -w
scripts/capture.sh end "$NAME"

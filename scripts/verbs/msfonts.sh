#!/bin/bash
# MS ゴシック/明朝 などの名前を持つ別名フォント (IPA 由来) を prefix に登録する: msfonts.sh <prefix>
# Wine の Replacements は EnumFontFamiliesEx に出ないため、名前で探すゲームが見つけられず落ちる対策
set -euo pipefail; P=$(realpath "$1"); R=$(dirname "$0")/../../runtime/msfonts
[ -f "$R/msgothic.ttf" ] || python3 -I "$(dirname "$0")/../tools/make_ms_alias_fonts.py" "$R"
cp --remove-destination "$R"/*.ttf "$P/drive_c/windows/Fonts/"
for f in msgothic:"MS Gothic" mspgothic:"MS PGothic" msuigothic:"MS UI Gothic" msmincho:"MS Mincho" mspmincho:"MS PMincho"; do
  WINEPREFIX=$P WINEDEBUG=-all wine reg add 'HKLM\Software\Microsoft\Windows NT\CurrentVersion\Fonts' /v "${f#*:} (TrueType)" /d "${f%%:*}.ttf" /f >/dev/null 2>&1
  WINEPREFIX=$P WINEDEBUG=-all wine reg delete 'HKCU\Software\Wine\Fonts\Replacements' /v "${f#*:}" /f >/dev/null 2>&1 || true
done
WINEPREFIX=$P wineserver -w; echo "msfonts installed into $P"

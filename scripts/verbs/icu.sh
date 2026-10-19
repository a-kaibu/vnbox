#!/bin/bash
# prefix に icu.dll シム (+ICU 本体) を入れる: icu.sh <prefix>
set -euo pipefail; P=$(realpath "$1"); R=$(dirname "$0")/../../runtime/icu
if grep -q '#arch=win64' "$P/system.reg"; then
  cp "$R"/64/*.dll "$P/drive_c/windows/system32/"; cp --remove-destination "$R"/32/*.dll "$P/drive_c/windows/syswow64/"
else
  cp --remove-destination "$R"/32/*.dll "$P/drive_c/windows/system32/"
fi
echo "icu installed into $P"

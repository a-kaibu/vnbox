#!/bin/bash
# DXVK (D3D8/9/10/11 -> Vulkan) を prefix に入れる: dxvk.sh <prefix>
set -euo pipefail; P=$(realpath "$1"); D=$(ls -d "$(dirname "$0")"/../../downloads/dxvk/dxvk-*/ | tail -1)
if grep -q '#arch=win64' "$P/system.reg"; then cp "$D"/x64/*.dll "$P/drive_c/windows/system32/"; cp "$D"/x32/*.dll "$P/drive_c/windows/syswow64/"
else cp "$D"/x32/*.dll "$P/drive_c/windows/system32/"; fi
for dll in d3d8 d3d9 d3d10core d3d11 dxgi; do
  WINEPREFIX=$P WINEDEBUG=-all wine reg add 'HKCU\Software\Wine\DllOverrides' /v $dll /d native /f >/dev/null 2>&1
done
echo "dxvk $(basename "$D") installed into $P"

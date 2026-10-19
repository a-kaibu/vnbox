#!/bin/bash
# vcrun.sh <prefix>  最新の Visual C++ 再頒布可能パッケージ (14.x) の DLL を prefix に入れ、VC\Runtimes の版を登録する。
# Unreal Engine 5 などは起動前に「VC\Runtimes\x64 の版」と「system32\msvcp140_2.dll の版」が 14.50 以上かを調べ、
# 足りないと「Microsoft Visual C++ 2015-2022 Redistributable (x64) が必要」で止まる。
# vc_redist を Wine で実行する代わりに、中の cab から DLL を取り出して置く (cabextract)。
set -euo pipefail; P=$(realpath "$1"); V=$(realpath "$(dirname "$0")/../.."); C=$V/runtime/vcrun; mkdir -p "$C"
W=$(command -v wine); grep -q '#arch=win64' "$P/system.reg" && [ -x /opt/wine-vnbox/bin/wine ] && W=/opt/wine-vnbox/bin/wine
win64=no; grep -q '#arch=win64' "$P/system.reg" && win64=yes
ver=""
for arch in x64 x86; do
  [ $arch = x64 ] && [ $win64 = no ] && continue
  dir=$C/$arch
  if [ ! -f "$dir/msvcp140.dll" ]; then
    rm -rf "$dir" "$C/tmp"; mkdir -p "$dir" "$C/tmp"
    curl -sSL -m 600 -o "$C/tmp/vc.exe" "https://aka.ms/vc14/vc_redist.$arch.exe"
    cabextract -q -d "$C/tmp/b" "$C/tmp/vc.exe" 2>/dev/null || true
    suf=$([ $arch = x64 ] && echo amd64 || echo x86)
    for cab in "$C"/tmp/b/a*; do cabextract -q -d "$C/tmp/d" -F "*.dll_$suf" "$cab" 2>/dev/null || true; done
    for f in "$C"/tmp/d/*.dll_$suf; do b=$(basename "$f" "_$suf"); case $b in mfc*|vcamp*|vccorlib*) continue;; esac; cp "$f" "$dir/$b"; done
    rm -rf "$C/tmp"
  fi
  # 64bit prefix: x64 は system32、x86 は syswow64。32bit prefix: x86 は system32
  if [ $arch = x64 ]; then sys=system32; elif [ $win64 = yes ]; then sys=syswow64; else sys=system32; fi
  for f in "$dir"/*.dll; do rm -f "$P/drive_c/windows/$sys/$(basename "$f")"; cp "$f" "$P/drive_c/windows/$sys/"; done
  # 版 (msvcp140.dll の VS_FIXEDFILEINFO) を VC\Runtimes に書く
  ver=$(python3 -I - "$dir/msvcp140.dll" <<'PY'
import struct, sys
d = open(sys.argv[1], 'rb').read(); i = d.find(struct.pack('<I', 0xfeef04bd))
ms, ls = struct.unpack_from('<II', d, i + 8)
print(ms >> 16, ms & 0xffff, ls >> 16, ls & 0xffff)
PY
)
  read -r a b c d <<< "$ver"
  reg=$(mktemp --suffix=.reg)
  printf 'REGEDIT4\n\n[HKEY_LOCAL_MACHINE\\SOFTWARE\\Microsoft\\VisualStudio\\14.0\\VC\\Runtimes\\%s]\n"Installed"=dword:00000001\n"Major"=dword:%08x\n"Minor"=dword:%08x\n"Bld"=dword:%08x\n"Rbld"=dword:%08x\n"Version"="v%s.%s.%s.%02d"\n' \
    $arch $a $b $c $d $a $b $c $d > "$reg"
  regedit=regedit; [ $arch = x86 ] && [ $win64 = yes ] && regedit='C:\windows\syswow64\regedit.exe'
  WINEPREFIX=$P WINEDEBUG=-all "$W" "$regedit" /S "Z:${reg//\//\\}" >/dev/null 2>&1; rm -f "$reg"
done
for dll in msvcp140 msvcp140_1 msvcp140_2 msvcp140_atomic_wait msvcp140_codecvt_ids vcruntime140 vcruntime140_1 vcruntime140_threads concrt140 vcomp140; do
  WINEPREFIX=$P WINEDEBUG=-all "$W" reg add 'HKCU\Software\Wine\DllOverrides' /v $dll /d native,builtin /f >/dev/null 2>&1
done
WINEPREFIX=$P "${W%/wine}/wineserver" -w 2>/dev/null || true
echo "vcrun ${ver// /.} installed into $P"

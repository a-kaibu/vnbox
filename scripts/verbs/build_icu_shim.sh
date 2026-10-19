#!/bin/bash
# Windows 10 標準の icu.dll (接尾辞なしエクスポート) を、公式 ICU (u_xxx_74) への転送 DLL として生成する
# 使い方: build_icu_shim.sh <icu4c-*-Win32-MSVC2019.zip> <icu4c-*-Win64-MSVC2019.zip> <outdir>
set -euo pipefail
Z32=$(realpath "$1"); Z64=$(realpath "$2"); OUT=$(realpath -m "$3"); T=$(mktemp -d)
for arch in 32 64; do
  Z=$([ $arch = 32 ] && echo "$Z32" || echo "$Z64"); CC=$([ $arch = 32 ] && echo i686-w64-mingw32-gcc || echo x86_64-w64-mingw32-gcc)
  mkdir -p "$T/$arch" "$OUT/$arch"; (cd "$T/$arch" && unzip -q -o "$Z" 'bin*/*.dll' && find . -name 'icu*.dll' -exec cp {} . \;)
  V=$(ls "$T/$arch" | sed -n 's/^icuuc\([0-9]*\)\.dll$/\1/p')
  { echo "LIBRARY icu"; echo "EXPORTS"
    for lib in icuuc icuin; do
      winedump -j export "$T/$arch/$lib$V.dll" | awk '$1 ~ /^[0-9A-F]+$/ && $3 ~ /_'"$V"'$/ {print $3}' \
        | while read -r sym; do echo "  ${sym%_$V}=$lib$V.$sym"; done
    done; } > "$T/$arch/icu.def"
  echo 'int __stdcall DllMain(void*h,unsigned r,void*p){return 1;}' > "$T/$arch/stub.c"
  $CC -shared -nostdlib -o "$OUT/$arch/icu.dll" "$T/$arch/stub.c" "$T/$arch/icu.def" \
      -Wl,--entry,$([ $arch = 32 ] && echo _DllMain@12 || echo DllMain) -Wl,--enable-stdcall-fixup
  cp "$T/$arch"/icu{uc,in,dt}$V.dll "$OUT/$arch/"
  echo "win$arch: $(grep -c = "$T/$arch/icu.def") exports -> $OUT/$arch"
done
rm -rf "$T"

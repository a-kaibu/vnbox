#!/bin/bash
# build.sh <winmd フォルダ> [出力 dll]  xamlstub32.dll を作る (i686-w64-mingw32-gcc)
set -euo pipefail; cd "$(dirname "$0")"
W=${1:?winmd folder}; OUT=${2:-xamlstub32.dll}; T=$(mktemp -d)
${PYTHON:-python3} gen.py "$W" $T/gen.c
i686-w64-mingw32-gcc -O1 -shared -o "$OUT" runtime.c $T/gen.c -I. -lruntimeobject -Wl,--kill-at
rm -rf $T; ls -la "$OUT"

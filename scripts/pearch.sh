#!/bin/bash
# exe の PE アーキテクチャ (x86 / x64) を返す
python3 -I -c 'import sys; sys.path.insert(0, sys.argv[1]); from detect import pe_arch; print(pe_arch(sys.argv[2]) or "x86")' "$(dirname "$0")" "$1"

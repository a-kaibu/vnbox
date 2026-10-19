#!/bin/bash
# ベース prefix を作る: 32bit / ja_JP / Win7 / 日本語フォント置換 / 仮想デスクトップ
set -euo pipefail
source "$(dirname "$0")/env.sh"
BASE=$(realpath -m "${1:-$VNBOX/prefixes/base}")
export WINEPREFIX=$BASE
[ -e "$BASE" ] && { echo "exists: $BASE"; exit 1; }
ensure_x
wineboot -i
wineserver -w
wine reg add 'HKCU\Software\Wine' /v Version /d ${WINVER:-$([ "$WINEARCH" = win64 ] && echo win10 || echo win7)} /f >/dev/null
wine regedit /S "$(winepath -w "$VNBOX/scripts/base.reg.in")"
wine regedit /S "$(winepath -w "$VNBOX/scripts/fonts-ja.reg")"
wineserver -w
# 確認
wine reg query 'HKCU\Software\Wine\Fonts\Replacements' | head -5
wine cmd /c ver
echo "codepage: $(wine reg query 'HKLM\System\CurrentControlSet\Control\Nls\CodePage' /v ACP | tr -d '\r' | awk '/ACP/{print $3}')"
"$VNBOX/scripts/verbs/icu.sh" "$BASE"   # Win10 同様に icu.dll を標準装備
"$VNBOX/scripts/verbs/msfonts.sh" "$BASE"   # ＭＳ ゴシック等の名前を持つ実体フォント (列挙で探すゲーム対策)
wineserver -k || true
echo "base prefix ready: $BASE ($(du -sh "$BASE" | cut -f1))"

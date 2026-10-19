#!/bin/bash
# xamlstub.sh <prefix>  XAML Islands (Windows.UI.Xaml.Hosting.DesktopWindowXamlSource) を使うゲーム向けに、
# 何もしない WinRT 実装 xamlstub32.dll を prefix に入れて各クラスを登録する (Wine には Windows.UI.Xaml の実装が無く、例外で落ちる)
set -euo pipefail; P=$(realpath "$1"); V=$(realpath "$(dirname "$0")/../.."); R=$V/runtime/xamlstub
if [ ! -f "$R/xamlstub32.dll" ]; then   # 初回だけ作る: Windows SDK のメタデータから生成してビルド
  [ -d "$R/winmd" ] || "$V/tools/xamlstub/fetch_winmd.sh" "$R/winmd" >/dev/null
  [ -x "$R/venv/bin/python" ] || { python3 -m venv "$R/venv" && "$R/venv/bin/pip" install -q dnfile; }
  PYTHON="$R/venv/bin/python" "$V/tools/xamlstub/build.sh" "$R/winmd" "$R/xamlstub32.dll" >/dev/null
fi
mkdir -p "$P/drive_c/vnbox"; cp "$R/xamlstub32.dll" "$P/drive_c/vnbox/"
reg=$(mktemp --suffix=.reg); { echo "REGEDIT4"; while read -r c; do [ -n "$c" ] || continue
  printf '\n[HKEY_LOCAL_MACHINE\\Software\\Microsoft\\WindowsRuntime\\ActivatableClassId\\%s]\n"DllPath"="C:\\\\vnbox\\\\xamlstub32.dll"\n"ActivationType"=dword:00000000\n' "$c"
done < "$V/tools/xamlstub/classes.txt"; } > "$reg"
W=$(command -v wine); [ -x /opt/wine-vnbox/bin/wine ] && grep -q '#arch=win64' "$P/system.reg" && W=/opt/wine-vnbox/bin/wine
wreg="Z:${reg//\//\\}"
WINEPREFIX=$P WINEDEBUG=-all "$W" regedit /S "$wreg" >/dev/null 2>&1
# 64bit prefix では 32bit 側の表示 (WoW64) にも書く
grep -q '#arch=win64' "$P/system.reg" && WINEPREFIX=$P WINEDEBUG=-all "$W" 'C:\windows\syswow64\regedit.exe' /S "$wreg" >/dev/null 2>&1 || true
WINEPREFIX=$P "${W%/wine}/wineserver" -w 2>/dev/null || true; rm -f "$reg"
echo "xamlstub installed into $P"

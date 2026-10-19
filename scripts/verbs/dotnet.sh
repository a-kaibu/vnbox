#!/bin/bash
# dotnet.sh <prefix> <major> <x64|x86> [desktop]  .NET (Core) のランタイムを prefix の Program Files\dotnet に展開する
# (.NET 5 以降のアプリは apphost が「.NET location: Not found」で起動しない)。desktop は WPF / WinForms 用の WindowsDesktop も入れる。
# Microsoft の配布 zip (builds.dotnet.microsoft.com) をその版の最新で取り、runtime/dotnet にためておく
set -euo pipefail; P=$(realpath "$1"); MAJ=${2:?major}; ARCH=${3:-x64}; KIND=${4:-}
V=$(realpath "$(dirname "$0")/../.."); C=$V/runtime/dotnet; mkdir -p "$C"
rel=$C/releases-$MAJ.json
[ -s "$rel" ] || curl -sSL -m 120 -o "$rel" "https://builds.dotnet.microsoft.com/dotnet/release-metadata/$MAJ.0/releases.json"
urls=$(python3 -I - "$rel" "$ARCH" "$KIND" <<'PY'
import json, sys
r = json.load(open(sys.argv[1]))['releases'][0]
keys = ['runtime'] + (['windowsdesktop'] if sys.argv[3] == 'desktop' else [])
for k in keys:
    for f in r[k]['files']:
        if f['rid'] == 'win-' + sys.argv[2] and f['name'].endswith('.zip'):
            print(f['url'])
PY
)
dst=$P/drive_c/"Program Files"/dotnet; [ "$ARCH" = x86 ] && grep -q '#arch=win64' "$P/system.reg" && dst=$P/drive_c/"Program Files (x86)"/dotnet
mkdir -p "$dst"
for u in $urls; do
  f=$C/$(basename "$u"); [ -s "$f" ] || curl -sSL -m 1800 -o "$f.part" "$u" && { [ -s "$f" ] || mv "$f.part" "$f"; }
  unzip -q -o "$f" -d "$dst"
done
echo "dotnet $MAJ ($ARCH${KIND:+ $KIND}) installed into $dst"

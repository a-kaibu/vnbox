#!/bin/bash
# fetch_winmd.sh <出力フォルダ>  Windows SDK のメタデータ (.winmd) を NuGet の Microsoft.Windows.SDK.Contracts から取る
set -euo pipefail; OUT=${1:?}; V=${SDK_CONTRACTS_VERSION:-10.0.28000.2705}
mkdir -p "$OUT"; T=$(mktemp -d)
curl -sSL -m 600 -o $T/p.nupkg "https://api.nuget.org/v3-flatcontainer/microsoft.windows.sdk.contracts/$V/microsoft.windows.sdk.contracts.$V.nupkg"
unzip -q -o -j $T/p.nupkg 'ref/netstandard2.0/*.winmd' -d "$OUT"; rm -rf $T; ls "$OUT" | wc -l

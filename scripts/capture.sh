#!/bin/bash
# DoNotInstaller の Wine 版: インストーラの痕跡 (レジストリ + ファイル) をパッケージに閉じ込める
#   capture.sh begin <name>   ベース prefix を複製してスナップショットを取る
#   (この間に WINEPREFIX=prefixes/<name> でインストーラを実行する)
#   capture.sh end <name>     差分を packages/<name>/ に書き出す
set -euo pipefail
source "$(dirname "$0")/env.sh"
CMD=$1; NAME=$2
P=$VNBOX/prefixes/$NAME; S=$P/.vnbox-snap; PKG=$VNBOX/packages/$NAME
manifest(){ (cd "$1/drive_c" && find . -type f -printf '%P\t%s\t%T@\n' | sort); }
case $CMD in
begin)
  rm -rf "$P"; clone_prefix "${BASE:-$VNBOX/prefixes/base}" "$P"; mkdir -p "$S"
  cp "$P/system.reg" "$P/user.reg" "$S/"; manifest "$P" > "$S/manifest.tsv"
  echo "snapshot taken: $P";;
end)
  WINEPREFIX=$P wineserver -k 2>/dev/null || true; sleep 1
  rm -rf "$PKG"; mkdir -p "$PKG/files"
  python3 -I "$VNBOX/scripts/regdiff.py" diff "$S/system.reg" "$P/system.reg" "$PKG/system.reg.frag"
  python3 -I "$VNBOX/scripts/regdiff.py" diff "$S/user.reg" "$P/user.reg" "$PKG/user.reg.frag"
  manifest "$P" > "$S/manifest.after.tsv"
  # 追加/変更ファイル (一時ファイル・ログ・Wine 自身の管理ファイルは除外)
  comm -13 "$S/manifest.tsv" "$S/manifest.after.tsv" | cut -f1 \
    | grep -viE '^(windows/(temp|logs|installer|inf|winsxs|prefetch)/|users/[^/]+/(temp|appdata/local/temp|appdata/local/microsoft)/|.*\.(tmp|log)$)' \
    > "$PKG/files.txt" || true
  (cd "$P/drive_c" && tar cf - -T "$PKG/files.txt") | (cd "$PKG/files" && tar xf -)
  # 起動 exe の候補 (新規 exe を大きい順に)
  (cd "$PKG/files" && find . -iname '*.exe' -printf '%s\t%P\n' | sort -rn \
     | grep -viE 'unins|uninst|setup|install|config|設定|readme' | head -5 | cut -f2) > "$PKG/exe_candidates.txt" || true
  TARGET=$(head -1 "$PKG/exe_candidates.txt" || true)
  cat > "$PKG/launch.ini" <<INI
[launch]
target=$TARGET
args=
renderer=
INI
  echo "== package: $PKG"; echo "files: $(wc -l < "$PKG/files.txt")  size: $(du -sh "$PKG/files" | cut -f1)"
  echo "exe candidates:"; sed 's/^/  /' "$PKG/exe_candidates.txt"
  echo "registry (HKLM):"; python3 -I "$VNBOX/scripts/regdiff.py" show "$PKG/system.reg.frag" | head -30
  echo "registry (HKCU):"; python3 -I "$VNBOX/scripts/regdiff.py" show "$PKG/user.reg.frag" | head -15;;
esac

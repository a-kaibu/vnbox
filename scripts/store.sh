#!/bin/bash
# ゲームのパッケージとセーブデータを S3 (互換ストレージ含む) に置く
#   store.sh push-package <packages/name>      パッケージを tar.zst で上げる
#   store.sh pull-package <name>               packages/<name> に取ってくる (既にあれば何もしない)
#   store.sh save-push <name> [prefix]         セーブ (起動後に変わったファイル + レジストリ差分) を上げる
#   store.sh save-pull <name> [prefix] [ts]    セーブを prefix に戻す (ts 省略時は latest)
#   store.sh list                              パッケージと自分のセーブの一覧
# 設定 (環境変数 or scripts/store.conf):
#   VNBOX_S3=s3://bucket/prefix   (必須)
#   S3_ENDPOINT=https://...        (任意: Cloudflare R2 / MinIO / Wasabi など S3 互換の場合)
#   VNBOX_USER=<名前>              (任意: セーブの持ち主。既定 default)
#   認証は AWS CLI の標準 (AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_PROFILE 等)
# S3 上の配置:
#   <VNBOX_S3>/packages/<name>.tar.zst, <name>.ini
#   <VNBOX_S3>/saves/<user>/<name>/<UTC時刻>.tar.zst と latest.tar.zst (履歴は残す)
set -euo pipefail
source "$(dirname "$0")/env.sh"
[ -f "$VNBOX/scripts/store.conf" ] && source "$VNBOX/scripts/store.conf"
AWS=${AWS:-$(command -v aws || echo /opt/s3tools/bin/aws)}
R=${VNBOX_S3:?VNBOX_S3=s3://bucket/prefix を設定してください}; R=${R%/}; U=${VNBOX_USER:-default}
s3(){ "$AWS" ${S3_ENDPOINT:+--endpoint-url "$S3_ENDPOINT"} s3 "$@"; }
exists(){ s3 ls "$1" >/dev/null 2>&1; }
# prefix の既定: パッケージ起動なら run-<name>、直接起動なら <name>
prefix_of(){ [ -d "$VNBOX/prefixes/run-$1" ] && echo "$VNBOX/prefixes/run-$1" || echo "$VNBOX/prefixes/$1"; }

CMD=${1:?usage}; shift
case $CMD in
push-package)
  PKG=$(realpath "$1"); N=$(basename "$PKG")
  [ -f "$PKG/launch.ini" ] || { echo "not a package: $PKG"; exit 1; }
  SIZE=$(du -sb "$PKG" | cut -f1)
  tar -C "$(dirname "$PKG")" -cf - "$N" | zstd -q -T0 -10 | s3 cp - "$R/packages/$N.tar.zst" --expected-size "$SIZE" --only-show-errors
  s3 cp "$PKG/launch.ini" "$R/packages/$N.ini" --only-show-errors
  echo "uploaded package $N ($(numfmt --to=iec "$SIZE"))";;
pull-package)
  N=$1; D=$VNBOX/packages
  [ -f "$D/$N/launch.ini" ] && { echo "already local: $D/$N"; exit 0; }
  mkdir -p "$D"; s3 cp "$R/packages/$N.tar.zst" - --only-show-errors | zstd -q -d | tar -C "$D" -xf -
  echo "downloaded package $N -> $D/$N";;
save-push)
  N=$1; P=$(realpath -m "${2:-$(prefix_of "$N")}")
  [ -f "$P/.vnbox-applied" ] || [ -f "$P/.vnbox-reg0/user.reg" ] || { echo "no prepared prefix: $P"; exit 1; }
  M=$P/.vnbox-applied; [ -f "$M" ] || M=$P/.vnbox-reg0/user.reg
  T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
  # 起動準備の後に書き換わったファイル + 以前セーブから戻したファイル (mtime が古いので別に覚えている)
  (cd "$P/drive_c" && { find . -type f -newer "$M" ! -path './windows/*' ! -ipath '*/temp/*' ! -iname '*.log' -printf '%P\n'
                        cat "$P/.vnbox-saved-files" 2>/dev/null || true; } | sort -u | while IFS= read -r f; do [ -f "$f" ] && printf '%s\n' "$f"; done) > "$T/files.txt"
  for r in user system; do
    [ -f "$P/.vnbox-reg0/$r.reg" ] && python3 -I "$VNBOX/scripts/regdiff.py" diff "$P/.vnbox-reg0/$r.reg" "$P/$r.reg" "$T/$r.reg.frag"
  done
  tar -cf "$T/save.tar" -C "$P/drive_c" --transform 's,^,drive_c/,' -T "$T/files.txt"
  tar -rf "$T/save.tar" -C "$T" files.txt $(cd "$T" && ls *.reg.frag 2>/dev/null)
  # 前回と中身が同じなら上げない (定期保存で同じものを積み上げないため)
  H=$( (cd "$T" && cat files.txt *.reg.frag 2>/dev/null; cd "$P/drive_c" && tr '\n' '\0' < "$T/files.txt" | xargs -0 -r cat) | sha256sum | cut -c1-16)
  [ "$H" = "$(cat "$P/.vnbox-save-hash" 2>/dev/null)" ] && { echo "save unchanged ($N)"; exit 0; }
  zstd -q -T0 -19 "$T/save.tar" -o "$T/save.tar.zst"
  TS=$(date -u +%Y%m%dT%H%M%SZ)
  s3 cp "$T/save.tar.zst" "$R/saves/$U/$N/$TS.tar.zst" --only-show-errors
  s3 cp "$T/save.tar.zst" "$R/saves/$U/$N/latest.tar.zst" --only-show-errors
  echo "$H" > "$P/.vnbox-save-hash"
  echo "saved $N: $(wc -l < "$T/files.txt") files, $(stat -c %s "$T/save.tar.zst") bytes -> saves/$U/$N/$TS";;
save-pull)
  N=$1; P=$(realpath -m "${2:-$(prefix_of "$N")}"); TS=${3:-latest}
  [ -d "$P/drive_c" ] || { echo "prefix not found: $P (先に起動準備をしてください)"; exit 1; }
  exists "$R/saves/$U/$N/$TS.tar.zst" || { echo "no save for $N ($U/$TS)"; exit 0; }
  T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
  s3 cp "$R/saves/$U/$N/$TS.tar.zst" - --only-show-errors | zstd -q -d | tar -C "$T" -xf -
  WINEPREFIX=$P wineserver -k 2>/dev/null || true   # 動作中だとレジストリが書き戻されるため
  # prefix の drive_c はベースとハードリンクを共有しているので、上書きせず置き換える
  [ -d "$T/drive_c" ] && cp -a --remove-destination "$T/drive_c/." "$P/drive_c/"
  for r in user system; do
    [ -s "$T/$r.reg.frag" ] && python3 -I "$VNBOX/scripts/regdiff.py" apply "$T/$r.reg.frag" "$P/$r.reg"
  done
  { cat "$T/files.txt"; cat "$P/.vnbox-saved-files" 2>/dev/null || true; } | sort -u > "$P/.vnbox-saved-files.new"; mv "$P/.vnbox-saved-files.new" "$P/.vnbox-saved-files"
  echo "restored $N from saves/$U/$N/$TS ($(wc -l < "$T/files.txt") files)";;
list)
  echo "== packages"; { s3 ls "$R/packages/" || true; } | awk '/\.tar\.zst$/{printf "  %-40s %s %s  %s\n", $4, $1, $2, $3}'
  echo "== saves ($U)"; { s3 ls --recursive "$R/saves/$U/" || true; } | awk '/latest\.tar\.zst$/{n=$4; sub(".*/saves/[^/]*/","",n); sub("/latest.tar.zst","",n); printf "  %-40s %s %s\n", n, $1, $2}';;
*) sed -n '2,8p' "$0"; exit 1;;
esac

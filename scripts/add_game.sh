#!/bin/bash
# ゲームを 1 コマンドで「パッケージ」にする
#   add_game.sh <name> <配布物> [起動exe(相対パス)]
#     配布物: zip / lzh / 7z / rar / 自己解凍 exe / インストーラ exe / msi / 展開済みフォルダ
#   結果: packages/<name>/ (launch.ini + files/ + レジストリ断片)。VNBOX_S3 があれば S3 にも上げる
#   起動 (配信): scripts/play.sh --package packages/<name> 8080
#   UPLOAD=0 で S3 へのアップロードを止める。起動 exe が違ったら packages/<name>/launch.ini の target を直す
set -euo pipefail
export WINE_FLAVOR=${WINE_FLAVOR:-vnbox}
source "$(dirname "$0")/env.sh"
[ -f "$VNBOX/scripts/store.conf" ] && source "$VNBOX/scripts/store.conf"
[[ "$WINE_FLAVOR" == vnbox* ]] && export BASE=${BASE:-$VNBOX/prefixes/base-vnbox} WINEARCH=win64
NAME=$1; SRC=$(realpath "$2"); EXE=${3:-}
PKG=$VNBOX/packages/$NAME; [ -e "$PKG" ] && { echo "already exists: $PKG"; exit 1; }
W=$(mktemp -d "$VNBOX/downloads/add-$NAME.XXXX"); trap 'rm -rf "$W"' EXIT
export DISPLAY=${DISPLAY:-:$((100 + $(echo -n "add-$NAME" | cksum | cut -d' ' -f1) % 400))}

# 1) 展開 (インストーラ exe は展開できないのでそのまま次へ)
if [ -d "$SRC" ]; then D=$SRC
elif "$VNBOX/scripts/extract.sh" "$SRC" "$W/x" >/dev/null 2>&1 && [ -n "$(find "$W/x" -iname '*.exe' -print -quit)" ]; then D=$W/x
else D=""; fi
# 展開できたのに exe が無い = 紹介動画などゲームではない配布物
if [ -z "$D" ] && [ -n "$(ls -A "$W/x" 2>/dev/null)" ]; then echo "実行ファイルがありません (動画のみの配布物?): $(cd "$W/x" && find . -type f | head -3 | tr '\n' ' ')"; exit 4; fi

# 1b) WinRAR の自己解凍は Linux 側で開けないことがあるので、空の作業フォルダで Wine に展開させる
if [ -z "$D" ] && [ -f "$SRC" ] && grep -qa 'WinRAR SFX' "$SRC"; then
  echo "[$NAME] WinRAR SFX -> Wine で展開"; mkdir -p "$W/sfx"; ensure_x
  P=$W/sfxprefix; clone_prefix "$BASE" "$P"
  (cd "$W/sfx" && WINEPREFIX=$P timeout 1800 wine "$SRC" >/dev/null 2>&1 &)
  sleep 10; "$VNBOX/scripts/wizard.sh" "$VNBOX/results/add-$NAME" 80
  # 展開中は進捗ダイアログが残るので、出力が増えなくなるまで待つ
  prev=-1; while s=$(du -s "$W/sfx" | cut -f1); [ "$s" != "$prev" ]; do prev=$s; sleep 10; done
  WINEPREFIX=$P wineserver -k 2>/dev/null || true
  [ -n "$(find "$W/sfx" -iname '*.exe' -print -quit)" ] && D=$W/sfx
fi

# 2) インストーラなら Wine 上でインストールして差分をパッケージ化
inst=""
if [ -z "$D" ]; then inst=$SRC
else
  inst=$(find "$D" -maxdepth 3 \( -iname '*.msi' -o -iname 'data1.hdr' \) -print -quit)
  if [ -z "$inst" ] && [ "$(python3 -I "$VNBOX/scripts/detect.py" "$D" | python3 -I -c 'import json,sys;print(json.load(sys.stdin)["engine"])')" = unknown ]; then
    inst=$(find "$D" -maxdepth 3 \( -iname 'setup.exe' -o -iname 'install*.exe' -o -iname '*インストール*.exe' \) -print -quit)
  fi
fi
if [ -n "$inst" ]; then
  echo "[$NAME] installer: $inst"
  if ! "$VNBOX/scripts/autoinstall.sh" "$NAME" "$([ -n "$D" ] && echo "$D" || echo "$inst")"; then
    # 無人化できない種類は GUI を自動操作 (既定ボタンを押し続ける)
    "$VNBOX/scripts/capture.sh" begin "$NAME" >/dev/null
    ensure_x; (cd "$(dirname "$inst")" && WINEPREFIX=$VNBOX/prefixes/$NAME timeout 1800 wine "$(basename "$inst")" >/dev/null 2>&1 &)
    sleep 15; "$VNBOX/scripts/wizard.sh" "$VNBOX/results/add-$NAME" 60
    sleep 5; WINEPREFIX=$VNBOX/prefixes/$NAME wineserver -k 2>/dev/null || true
    "$VNBOX/scripts/capture.sh" end "$NAME"
  fi
  rm -rf "$VNBOX/prefixes/$NAME"
  [ -n "$EXE" ] && sed -i "s|^target=.*|target=$EXE|" "$PKG/launch.ini"
else
# 3) 展開するだけで動くゲーム: files/Games/<name>/ に置く
  mkdir -p "$PKG/files/Games/$NAME"; cp -a "$D/." "$PKG/files/Games/$NAME/"
  if [ -z "$EXE" ]; then
    EXE=$(python3 -I "$VNBOX/scripts/detect.py" "$PKG/files/Games/$NAME" | python3 -I -c 'import json,sys,re
d=json.load(sys.stdin)
c=[e for e in d["exes"] if not re.search(r"(?i)unins|setup|install|config|設定|launcher|crash|vc_?redist|dxsetup|directx|updater|readme|manual|support",e["exe"])]
print((c or d["exes"])[0]["exe"] if d["exes"] else "")')
  fi
  [ -z "$EXE" ] && { echo "exe が見つかりません: $D"; rm -rf "$PKG"; exit 1; }
  printf '[launch]\ntarget=Games/%s/%s\nargs=\nrenderer=\n' "$NAME" "$EXE" > "$PKG/launch.ini"
  : > "$PKG/system.reg.frag"; : > "$PKG/user.reg.frag"
fi
echo "== $PKG ($(du -sh "$PKG" | cut -f1))"; cat "$PKG/launch.ini"
grep -q '^target=.' "$PKG/launch.ini" || { echo "起動 exe が決まりませんでした (インストールに失敗した可能性)。$PKG を確認してください"; exit 2; }

# 4) S3 にも置く
if [ -n "${VNBOX_S3:-}" ] && [ "${UPLOAD:-1}" = 1 ]; then "$VNBOX/scripts/store.sh" push-package "$PKG"; fi

#!/bin/bash
# 使い方: extract.sh <archive> <dest>
# 日本語アーカイブを展開し、ファイル名を UTF-8 に揃える (Wine は UTF-8 名しか正しく扱えない)
set -euo pipefail
export LANG=ja_JP.UTF-8 LC_ALL=ja_JP.UTF-8
A=$(realpath "$1"); D=$2; mkdir -p "$D"; cd "$D"
case "${A,,}" in
  # 警告終了 (名前の文字コード等) でも中身が出ていれば続行する
  *.zip) unzip -q -O cp932 -o "$A" || 7z x -y -mcp=932 "$A" >/dev/null || [ -n "$(ls -A)" ] ;;
  *.lzh|*.lha) lha -xqf "$A" || [ -n "$(ls -A)" ] ;;
  *.7z|*.rar|*.cab) 7z x -y "$A" >/dev/null ;;
  *.exe) 7z x -y "$A" >/dev/null || { echo "not an extractable SFX (installer?): $A"; exit 2; } ;;
  *) echo "unknown archive: $A"; exit 1 ;;
esac
# Shift-JIS のままのファイル名を UTF-8 に変換 (既に UTF-8 なものはスキップされる)
convmv -r -f cp932 -t utf8 --notest --qfrom --qto . >/dev/null 2>&1 || true
find . -maxdepth 3 2>/dev/null | head -40 || true   # 件数が多いと head が先に閉じて SIGPIPE になるので無視

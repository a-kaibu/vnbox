#!/bin/bash
# fetch_list.tsv を順にダウンロード -> 展開 -> アーカイブ削除 (exe は残す)
cd "$(dirname "$0")/.."
fetch(){ local name=$1 url=$2 f; f=downloads/$name/$(basename "${url%%\?*}"); mkdir -p downloads/$name
  curl -sSL --retry 3 -m 3600 -o "$f" "$url" || { echo "FAIL dl $name"; return; }
  case "${f,,}" in
    *.exe) mkdir -p games/$name; echo "OK $name (installer/sfx kept: $f)";;
    *) scripts/extract.sh "$f" games/$name >/dev/null 2>&1 && { rm -f "$f"; echo "OK $name $(du -sh games/$name | cut -f1)"; } || echo "FAIL extract $name";;
  esac; }
export -f fetch
xargs -P 3 -L 1 bash -c 'fetch "$0" "$1"' < scripts/fetch_list.tsv
echo FETCH_ALL_DONE

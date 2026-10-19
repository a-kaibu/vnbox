#!/bin/bash
# probe.sh <name> [exe相対パス]  エンジン判定 -> 自動起動 -> 経過スクショ -> モンタージュ
cd "$(dirname "$0")/.."
NAME=$1; G=games/$NAME
INFO=$(python3 -I scripts/detect.py "$G")
EXE=${2:-$(python3 -I -c 'import json,sys,re
d=json.loads(sys.argv[1])
c=[e for e in d["exes"] if not re.search(r"(?i)unins|setup|install|config|設定|launcher|crash|vc_?redist|dxsetup|directx|updater|readme|manual",e["exe"])]
print((c or d["exes"])[0]["exe"])' "$INFO")}
ENG=$(python3 -I -c 'import json,sys;d=json.loads(sys.argv[1]);print(d["engine"],d["tags"])' "$INFO")
echo "[$NAME] engine=$ENG exe=$EXE"
INPUT="${INPUT:-shot:t20 sleep:15 shot:t35 click:400,300 sleep:3 shot:c1 key:Return sleep:3 shot:k1}" scripts/run.sh "$NAME" "$G/$(dirname "$EXE")" "$(basename "$EXE")" ${WAIT:-20}
montage results/$NAME/0*.png -tile 6x -geometry 320x240+2+2 -background '#222' results/$NAME/montage.png 2>/dev/null

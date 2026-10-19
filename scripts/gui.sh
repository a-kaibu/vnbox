#!/bin/bash
# 対話操作用: gui.sh shot <out.png> | click x y | key K | type TEXT | wins
source "$(dirname "$0")/env.sh"; export DISPLAY=${GUI_DISPLAY:-:50}
case $1 in
  shot) import -window root "$2" ;;
  click) xdotool mousemove $2 $3; sleep 0.3; xdotool mousedown 1; sleep 0.1; xdotool mouseup 1 ;;
  key) shift; xdotool key "$@" ;;
  type) shift; xdotool type --delay 50 "$*" ;;
  wins) for w in $(xdotool search --onlyvisible --name . 2>/dev/null); do eval $(xdotool getwindowgeometry --shell $w); echo "$w $X,$Y ${WIDTH}x$HEIGHT $(xdotool getwindowname $w)"; done ;;
esac

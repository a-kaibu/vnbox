#!/bin/bash
cd "$(dirname "$0")/../.."
for p in $(pgrep -f 'tools/cleaner\.sh'); do [ "$p" != "$$" ] && [ "$p" != "$PPID" ] && kill $p; done
rm -f 'results/batch/*.kept'
nohup scripts/tools/cleaner.sh > results/cleaner.log 2>&1 &

#!/bin/bash
# exe の PE アーキテクチャ (x86 / x64) を返す
case "$(file -b "$1" 2>/dev/null)" in
  PE32+*) echo x64;;
  *) echo x86;;
esac

# 日本語フォント置換 .reg を UTF-16LE で生成する (regedit 5.00 形式)
import sys
m = {
 "MS Gothic":"IPAGothic", "MS PGothic":"IPAPGothic", "MS UI Gothic":"IPAPGothic",
 "MS Mincho":"IPAMincho", "MS PMincho":"IPAPMincho",
 "ＭＳ ゴシック":"IPAGothic", "ＭＳ Ｐゴシック":"IPAPGothic",
 "ＭＳ 明朝":"IPAMincho", "ＭＳ Ｐ明朝":"IPAPMincho",
 "Meiryo":"Noto Sans CJK JP", "Meiryo UI":"Noto Sans CJK JP", "メイリオ":"Noto Sans CJK JP",
 "Yu Gothic":"Noto Sans CJK JP", "Yu Gothic UI":"Noto Sans CJK JP", "游ゴシック":"Noto Sans CJK JP",
 "Yu Mincho":"Noto Serif CJK JP", "游明朝":"Noto Serif CJK JP",
}
lines = ["Windows Registry Editor Version 5.00", "",
         r"[HKEY_CURRENT_USER\Software\Wine\Fonts\Replacements]"]
lines += [f'"{k}"="{v}"' for k, v in m.items()]
open(sys.argv[1], "w", encoding="utf-16", newline="\r\n").write("\n".join(lines) + "\n")

#!/usr/bin/env python3
"""IPA フォントの name テーブルを書き換えて「ＭＳ ゴシック」等の名前を持つ別名フォントを作る。
EnumFontFamiliesEx で MS 系フォントを名前で探すゲーム向け (Wine の置換設定は列挙には出ないため)。
使い方: make_ms_alias_fonts.py <outdir>"""
import sys, os
from fontTools.ttLib import TTFont
SRC = "/usr/share/fonts/opentype/ipafont-gothic/ipag.ttf", "/usr/share/fonts/opentype/ipafont-gothic/ipagp.ttf", \
      "/usr/share/fonts/opentype/ipafont-mincho/ipam.ttf", "/usr/share/fonts/opentype/ipafont-mincho/ipamp.ttf"
MAP = [  # (元, 出力名, 英語名, 日本語名)
    (SRC[0], "msgothic.ttf", "MS Gothic", "ＭＳ ゴシック"),
    (SRC[1], "mspgothic.ttf", "MS PGothic", "ＭＳ Ｐゴシック"),
    (SRC[1], "msuigothic.ttf", "MS UI Gothic", "MS UI Gothic"),
    (SRC[2], "msmincho.ttf", "MS Mincho", "ＭＳ 明朝"),
    (SRC[3], "mspmincho.ttf", "MS PMincho", "ＭＳ Ｐ明朝"),
]
out = sys.argv[1]; os.makedirs(out, exist_ok=True)
for src, fn, en, ja in MAP:
    f = TTFont(src); name = f["name"]
    for rec in list(name.names):
        if rec.nameID in (1, 3, 4, 6, 16, 17, 18): name.removeNames(nameID=rec.nameID)
    for lang_win, label in ((0x409, en), (0x411, ja)):
        name.setName(label, 1, 3, 1, lang_win); name.setName(label, 4, 3, 1, lang_win)
        name.setName("Regular", 2, 3, 1, lang_win); name.setName(label + ";vnbox", 3, 3, 1, lang_win)
    name.setName(en.replace(" ", ""), 6, 3, 1, 0x409)
    f.save(os.path.join(out, fn)); print("wrote", fn, en, ja)

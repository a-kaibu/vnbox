# ソフト電池ランタイム 5.0.9.0

ソフト電池 (Paltio) の DRM が掛かった製品版を動かすのに要るランタイム。`vnbox add` が DRM:SoftDenchi を
見つけると `vnbox-sdrt` (internal/verbs) で prefix に入れる。

- 出所: https://www.soft-denchi.jp/comdocs/dlguide/index.htm の
  https://acf.paltio.co.jp/sdrt/sdrt5090_x64.exe (md5 BFE6EA45F8D083C390B10574D53C929D) と
  https://acf.paltio.co.jp/sdrt/sdrt5090_win32.exe (md5 5E63D4FCC2FB976F194547420D31EF70)
- `sdrt5090_x64.exe` / `sdrt5090_win32.exe`: InstallShield のインストーラ (64bit / 32bit Windows 用)。
  前提の Visual C++ 2015-2019 ランタイムも入れる
- `License.txt`: ランタイムの使用許諾契約書
- 5.0.6.0 (XP / Vista 用) は使えない: DMM の認証先 www.dmm.co.jp/license/-/softdenchi/ が 502 を返し、
  「応答電文に形式上の誤りがあります」で止まる。5.0.9.0 は dlsoft.dmm.co.jp/api/auth/softdenchi/ に問い合わせる

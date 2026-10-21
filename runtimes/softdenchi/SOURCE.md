# ソフト電池ランタイム 5.0.6.0

ソフト電池 (Paltio) の DRM が掛かった製品版を動かすのに要るランタイム。`vnbox add` が DRM:SoftDenchi を
見つけると `vnbox-sdrt` (internal/verbs) で prefix に入れる。

- 出所: https://acf.paltio.co.jp/sdrt/sdrt5060.exe (WinRAR 自己解凍、sha256
  e36f90c4556a391fbaf7eaa998a36b8c1361b430583391c7e9c82d8bb2acae0a) を解凍したもの
- `sdrt5060_x64.exe` / `sdrt5060_win32.exe`: InstallShield のインストーラ (64bit / 32bit Windows 用)
- `vcredist_x64.exe` / `vcredist_x86.exe`: その前提の Visual C++ 2008 ランタイム (同梱されていたもの)
- `License.txt`: ランタイムの使用許諾契約書

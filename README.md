# vnbox — Windows ノベルゲームを Wine で動かし、ウィンドウ単位でブラウザ配信する

Linux サーバ上で Windows 向けノベルゲームを Wine で動かし、Android などのブラウザから遊ぶための道具一式。
「Windows を起動してリモートデスクトップ」の代わりに、**1ゲーム = 1つの X ディスプレイ + 1つの音声シンク + 1つの配信ポート** で、ゲームのウィンドウだけを配信する。

## アーキテクチャ

```
 配布物 (zip/lzh/7z/rar/SFX/インストーラ/msi)
   │  scripts/add_game.sh <name> <配布物>
   │    ├ 展開して動くもの → files/Games/<name>/ に置くだけ
   │    └ インストーラ     → ベース prefix の複製上でインストール (無人 or GUI 自動操作)
   │                          → 前後の差分 (ファイル + レジストリ) だけを取り出す
   ▼
 packages/<name>/          ← 1 ゲーム = 1 パッケージ (Wine/OS に依存しない中身だけ)
   launch.ini              起動 exe・引数・追加設定
   files/                  C:\ 以下に置くファイル
   system.reg.frag         HKLM の差分 (BGI などインストール先キーが要るもの)
   user.reg.frag           HKCU の差分
   │
   │  store.sh push-package / pull-package     ┌────────── S3 (S3 互換可) ──────────┐
   ├──────────────────────────────────────────▶│ packages/<name>.tar.zst   (本体)     │
   │                                           │ saves/<user>/<name>/<時刻>.tar.zst   │
   │                                           │ saves/<user>/<name>/latest.tar.zst   │
   ▼  scripts/play.sh --package packages/<name> └──────────────────────────────────────┘
 prefixes/run-<name>/      ← 実行用の Wine prefix        ▲ 起動前に save-pull
   = ベース prefix (base-vnbox) のハードリンク複製       │ プレイ中 5 分ごと + 終了時に save-push
   + パッケージを流し込み                               │ (準備後に変わったファイル + レジストリ差分だけ)
   │
   ▼
 Wine (自前ビルド /opt/wine-vnbox, 新 WoW64)。1 ゲームごとに:
   Xvfb :N (WM なし) ──x11grab──▶ ffmpeg MJPEG ──────┐
   PulseAudio null シンク ──monitor──▶ ffmpeg Opus/WebM ─┤ stream.py (HTTP :port) ◀──▶ ブラウザ (Android 等)
   xdotool ◀── POST /input (タップ・キー) ─────────────┘
```

- **ベース prefix** (`prefixes/base-vnbox`): 64bit / Win10 / コードページ 932 / 日本語フォント置換 / ＭＳ ゴシック等の実体フォント / icu.dll / 仮想デスクトップ無し。`scripts/mkbase.sh` で作る。各ゲームの prefix はこれのハードリンク複製なので、1 本あたりの追加ディスクはゲーム本体とセーブ分だけ。
- **パッケージ**はゲームの中身だけで、Wine や prefix を含まない。ベースや Wine を更新しても作り直さずに使える。
- **1 ゲーム = 1 X ディスプレイ + 1 音声シンク + 1 ポート**。複数人・複数本を同時に動かしても映像・音声・入力は混ざらない。

## ゲームの追加

```bash
# 1 コマンドでパッケージ化 (展開 → インストーラ判定 → インストール/配置 → 起動 exe 推定)
scripts/add_game.sh mygame ~/Downloads/mygame_trial.zip
scripts/add_game.sh mygame2 ~/Downloads/setup.exe                    # インストーラでも同じ
scripts/add_game.sh mygame3 ~/Downloads/game.zip 'Game/start.exe'    # 起動 exe を明示
# → packages/<name>/ ができる。VNBOX_S3 が設定されていれば S3 にも上がる (UPLOAD=0 で抑止)

scripts/play.sh --package packages/mygame 8080    # 配信開始 → ブラウザで http://<server>:8080/
```

- インストーラの扱い: MSI は `msiexec /qn`、Inno は `/VERYSILENT`、NSIS は `/S`、それ以外 (InstallShield / CreateInstall / 独自) は `wizard.sh` がダイアログの既定ボタンを押し続ける。WinRAR 自己解凍は空の作業フォルダで Wine に展開させる。
- 起動 exe の推定が外れたら `packages/<name>/launch.ini` の `target=` を直す (C:\ からの相対パス)。`renderer=` (gl/vulkan) や `winetricks=` も書ける。
- 起動ダイアログ (画面モード選択など) は配信画面でそのまま操作すればよい。自動テストでは `run.sh` の操作列 `pass:N` が Enter → Space で抜ける。
- 動くかの確認だけなら: `INPUT="shot:t25 pass:5 sleep:10 shot:a" scripts/run.sh <name> <ゲームフォルダ> <exe> 25` → `results/<name>/` にスクショ。
- 確認例: zip 版 (淫落痴漢電車) と WinRAR 自己解凍版 (獄淫の尖塔) を `add_game.sh` で追加し、パッケージからの起動まで確認。

## ストリーミングの方式

| 経路 | 方式 | 実装 |
|---|---|---|
| 映像 | ゲームウィンドウの矩形だけを `ffmpeg -f x11grab` で取り込み、MJPEG (既定 15fps, `-q:v 5`) を `multipart/x-mixed-replace` で `<img>` に流す | `stream.py` `/video` |
| 音声 | ゲーム専用の PulseAudio null シンクの monitor を Opus 96kbps / WebM にして `<audio>` に流す (モバイルは最初のタップで開始) | `stream.py` `/audio` |
| 入力 | タップ/クリック/ホイール/キーを `POST /input` で受け、表示倍率を逆算した座標で `xdotool` がそのウィンドウへ送る。画面下に Enter / Esc / Skip(Ctrl) / 右クリックのボタン | `stream.py` `/input` |
| 画面 | スマホ縦横どちらでもレターボックス表示 | `stream.py` のページ |

- 1x1 のまま表示されないゲーム (フルスクリーンを WM 任せにするもの) は `play.sh` が検出して `Managed=N` で起動し直す。
- **注意 (PoC の制約)**: `stream.py` は `0.0.0.0` で待ち受け、**認証が無い**。インターネットに出す場合は必ずリバースプロキシ (Caddy/nginx + 認証、Cloudflare Access、Tailscale など) の内側に置くこと。
- MJPEG は実装が単純な代わりに帯域が多い。本番は同じ構成のまま映像を H.264 + WebRTC (GStreamer webrtcbin / Selkies) に置き換えると帯域と遅延が下がる。

## ゲームデータとセーブの保存 (S3)

`scripts/store.sh` がパッケージ (本体) とセーブを S3 または S3 互換ストレージ (Cloudflare R2 / MinIO / Wasabi 等) に置く。

```bash
# scripts/store.conf (または環境変数)
VNBOX_S3=s3://my-bucket/vnbox
S3_ENDPOINT=https://<account>.r2.cloudflarestorage.com   # AWS S3 なら不要
VNBOX_USER=yashikota                                     # セーブの持ち主 (人ごとに分ける)
# 認証は AWS CLI 標準: AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_PROFILE

scripts/store.sh push-package packages/mygame          # 本体を上げる (tar + zstd)
scripts/store.sh pull-package mygame                   # 別サーバで取ってくる
scripts/store.sh save-push mygame                      # セーブを上げる (履歴付き)
scripts/store.sh save-pull mygame [20261019T015356Z]   # セーブを戻す (時刻を省略すると最新)
scripts/store.sh list
```

- **自動化**: `VNBOX_S3` が設定されていれば `play.sh --package` は、手元にパッケージが無ければ S3 から取得し、起動前に最新セーブを戻し、プレイ中 5 分ごと (`SAVE_INTERVAL`) と終了時にセーブを上げる。内容が前回と同じなら上げない。
- **セーブに含めるもの**: 起動準備の後に変更・作成されたファイル (ゲームフォルダ内の `.sav` 類、`AppData`、`Documents` など。`windows/` と一時ファイルは除外) と、HKCU/HKLM のレジストリ差分。ゲームごとの保存場所を知らなくても拾える。サイズは通常 KB〜数 MB。
- **履歴**: `saves/<user>/<name>/<UTC時刻>.tar.zst` を消さずに積むので、任意の時点に戻せる。古いものは S3 のライフサイクルルールで期限切れにするとよい。
- サーバを捨てても `packages/` と `saves/` が S3 にあれば、新しいサーバで `mkbase.sh` → `play.sh --package` だけで続きから遊べる。
- AWS CLI は `/opt/s3tools` (venv) に入れてある。PATH に `aws` があればそちらを使う。設定のひな形は `scripts/store.conf.example`。
- 確認したこと (S3 互換のテストサーバ moto で): パッケージの往復 (内容が完全一致)、セーブの保存 → 新しい prefix へ復元 → 追加保存 → 再復元 → 過去の版への巻き戻し、`play.sh` 経由での自動取得・自動保存 (NScripter の `gloval.sav` が保存された)。**本物の AWS S3 / R2 では未確認**。

## 構成

```
vnbox.py                 入口 (list / detect / test / play / capture-begin / capture-end)
scripts/
  add_game.sh            配布物 1 つからパッケージを作る (下の各スクリプトを組み合わせる)
  store.sh               パッケージとセーブの S3 保存・取得 (store.conf で設定)
  play.sh                配信付き起動 (専用 Xvfb + 専用 PulseAudio シンク + stream.py + セーブ自動同期)
  stream.py              配信サーバ (映像 MJPEG / 音声 Opus-WebM / 入力 POST→xdotool)
  launch.sh              パッケージを新しい prefix に展開して起動 (PREPARE_ONLY=1 で準備だけ)
  env.sh                 共通環境 (ja_JP.UTF-8, TZ=Asia/Tokyo, Wine の選択, Xvfb, PulseAudio, prefix のハードリンク複製)
  mkbase.sh              ベース prefix 作成
  base.reg.in            ベースのレジストリ調整 (仮想デスクトップ無し, XRandR モード変更無効, クラッシュダイアログ無効)
  gen_fonts_reg.py       MS ゴシック/明朝/メイリオ/游 → IPA/Noto の置換 .reg
  extract.sh             zip/lzh/7z/rar/cab/SFX exe の展開 + Shift-JIS ファイル名を UTF-8 に変換
  detect.py              エンジン / インストーラ / DRM / ランタイム依存 / PE アーキテクチャ判定
  autoinstall.sh         インストーラの無人インストール (MSI / Inno / NSIS / InstallShield cab)
  wizard.sh              インストーラ GUI の自動操作 (既定ボタンを Enter → Space)
  capture.sh             インストール前後の差分キャプチャ (DoNotInstaller の Wine 版)
  regdiff.py             Wine の system.reg / user.reg を直接 diff・merge
  run.sh                 自動テスト (操作列 INPUT: click/key/shot/sleep/pass、スクショ・ログを results/ へ)
  stopall.sh             全停止
  verbs/                 icu.dll シム / MS 別名フォント / DXVK
  tools/                 一括テスト用 (suezou_crawl, pick_trials2, batch, verdict, sheet, record, titles_md など)
packages/<name>/         パッケージ
prefixes/                ベース prefix と実行用 prefix
results/verified.tsv     起動確認済みタイトルの台帳 (README の一覧はここから生成: scripts/tools/titles_md.py)
wine-build/              自前 Wine のパッチ
```

## 動作確認結果 (Wine 11.0 / Ubuntu 24.04 / GPU なし・ソフトウェア描画 / 2026-10-18)

「動作」= タイトル画面から本編に入り、テキスト送りまでスクリーンショットで確認したもの。

| エンジン | タイトル | 入手形態 | 結果 | 必要だった手当て |
|---|---|---|---|---|
| NScripter | ナルキッソス SIDE 2nd | lzh | 動作 | - |
| NScripter + プラグイン | トライアンソロジー 体験版 | zip | 動作 (タイトル動画がカクつく) | - |
| NScripter | 彼岸花の咲く夜に お試し版 | CreateInstall 独自インストーラ | 動作 | capture → package |
| PONScripter | うみねこのなく頃に咲 体験版 | zip | 動作 | - |
| 吉里吉里2 / KAG3 | KAG 紹介デモ | zip | 動作 | (データ側の config_version 修正) |
| 吉里吉里 商用 (xp3 暗号化) | 先輩、好きです……押忍！ 体験版 | zip | 動作 | - |
| YU-RIS | YU-RIS β4 サンプル | zip | 動作 (YMV 動画も再生) | - |
| BGI/Ethornell | クダンノフォークロア 体験版 | 独自 Setup.exe | 動作 | capture → package (HKLM のインストール先キーが必要) |
| CatSystem2 | 果つることなき未来ヨリ 体験版 | zip | 動作 | 起動時の画面モード選択 |
| SiglusEngine | プリマドール 体験版 (Key) | zip | 動作 | - |
| RealLive | 5 -ファイブ- 体験版 | SFX ウィザード | 動作 | capture → package |
| Artemis | 遥かなるニライカナイ 体験版 | zip | 動作 (音声確認済) | - |
| QLIE | 天翼のアスクレイン 体験版 | zip | 動作 | 仮想画面を 1920x1080 に |
| Majiro | あいれぼ 体験版 | 独自スタートアップ | 動作 | capture → package |
| Softpal ADV | 転性魔王さま 体験版 / 花子さん 体験版 | zip | 動作 | - |
| AdvHD (WillPlus, D3D11) | 有閑夫人倶楽部 体験版 | 7z SFX | 動作 | **icu.dll シム** |
| Musica (minori) | Wind RP 体験版 | zip | 動作 | - |
| TyranoScript (NW.js) | 転生した世界で… 体験版 | zip | 動作 | - |
| Ren'Py 8 (x64) | The Question | zip | 動作 | **64bit prefix + Win10 指定** |
| SakanaGL (OpenGL, x64) | でか乳巫女と巨乳シスター 体験版 | zip | 動作 (vnbox Wine + Managed=N) | 1x1 ウィンドウ対策 (後述) |
| Unity 2022 (x86, D3D11) | 旭光のマリアージュ 体験版 | zip | 動作 (vnbox Wine) | 新 WoW64 でメモリ不足が解消 |

第1弾 21 本は、自前 Wine (後述) で 21 本すべてタイトル以降まで動作。

## 自前 Wine (`/opt/wine-vnbox`, `WINE_FLAVOR=vnbox`)

Wine 11.0 のソースに次のパッチを当て、`--enable-archs=i386,x86_64` (新 WoW64) でビルドしたもの。
ベース prefix は `prefixes/base-vnbox` (64bit, Win10, CP932)。32bit ゲームもこの prefix で動く。

| パッチ | 内容 | 効いたタイトル |
|---|---|---|
| `wine-build/third_party/fitudao-wine-patches/softdenchi-fixes.patch` | fitudao 氏の ソフト電池 修正 (crypt32 / imagehlp / msi) | (ソフト電池 DRM 作品向け。今回の体験版には該当なし) |
| `wine-build/vnbox-quartz-seek-deadlock.patch` | winegstreamer: 下流が sample を返さないまま seek されると `flushing_cs` で最大60秒デッドロックする問題を修正 (GetBuffer をポーリング化し、flush/stop 要求で抜ける) | 中の人などいない (Alcot, 吉里吉里2 krmovie): ロゴ動画で60秒固まる → 即座にタイトルへ |
| (新 WoW64 ビルド自体) | 32bit プロセスでも 64bit 側でドライバ/llvmpipe が動くため、32bit アドレス空間が枯れない | Unity 2022 x86: GL_OUT_OF_MEMORY → 本編まで動作 |
| `wine-build/vnbox-kernelbase-mitigation-policy.patch` | kernelbase: `GetProcessMitigationPolicy` の stub が出力バッファを初期化せずに TRUE を返していた。Wine は緩和策を何も実装していないので「全ポリシー無効」(ゼロ埋め) を返す | ムジナ (2024, ChakraCore 同梱) |
| `wine-build/vnbox-ntdll-writecopy-oldprot.patch` | ntdll: イメージの書き込み可能ページは Wine では常に WRITECOPY 扱いで、書き込み後も `VirtualProtect` の旧保護属性が `PAGE_WRITECOPY` のまま。Windows では書き込み後 `PAGE_READWRITE` になる。`/proc/self/pagemap` で私有コピー化済みかを見て、旧保護属性を Windows と同じ値で返す | ムジナ: ChakraCore が読み取り専用データの保護を戻すときに旧属性を厳密に検査して fail-fast していた |

この 2 本は kernelbase.dll / ntdll (PE と ntdll.so) だけを再ビルドして `/opt/wine-vnbox` に差し替えた (元のファイルは `wine-build/orig-dlls/`)。

## 第2弾: suezou ミラーの体験版を年代別に一括テスト (2006〜2026)

`scripts/tools/suezou_crawl.py` でミラー全 195 ページから 4,009 件のリンクを集め、年度ごとにブランドが重ならないよう抽出。
`scripts/tools/batch.sh` が DL → 展開 / 無人インストール → エンジン判定 → 起動 → スクショ を自動で回す (vnbox Wine 使用)。

第2弾 (51 本) と第3弾 (67 本、2 並列) の結果は `results/batch/summary.tsv`、各タイトルの経過スクショは `results/batch/<id>.png`。
判定は `scripts/tools/verdict.sh` (プロセス生存 / 最終画面が十分大きい / 画面中央にコントラスト / クラッシュ痕跡なし) で一次判定し、**全件スクショを目視**して確定した。

## 動作確認の集計

**タイトル画面以降まで目視で確認できたもの: 211 本** (一覧は下の「起動確認済みタイトル一覧」)。
- 第1〜3弾: 102 本 (第1弾 22 本 + suezou ミラー 80 本)
- 第4弾 (2026-10-19): suezou ミラーから新たに 108 本 (`downloads/suezou/picked4b.tsv`、3 並列バッチ)
- 以前不可だったムジナ (2024) が Wine パッチ 2 本で動作: 1 本

ロゴ・注意画面・起動ダイアログまでしか確認できていないもの 15 本は「弱:」として別扱い (211 本には含めない)。
第4弾は「同じ年の同じブランドは 1 本・既に試した作品は除く」で選んだ (ブランドの重複なしでは候補が尽きたため)。

| エンジン (検出名) | 本数 |
|---|---|
| 吉里吉里 / KiriKiri2 | 56 |
| Softpal ADV | 17 |
| YU-RIS | 10 |
| SiglusEngine / RealLive | 7 |
| EntisGLS / NeXAS / QLIE / BGI / CatSystem2 | 21 |
| AdvWin32/WillPlus / NScripter / AdvHD / Unity / Majiro / Escude / AliceSoft System4 / Kaguya / PONScripter / Artemis / Musica / TyranoScript / Ren'Py / SakanaGL / ShiinaRio | 29 |
| 判定外 (独自エンジン等) | 71 |

未解決 (2026-10-19 時点):
- 黒画面のまま: 第2〜4弾で十数本 (sukerasomero, bishop 2008/2012/2021, lumpofsugar 2011/2013, silkys 系など。未調査)。lumpofsugar 2010 はクリティカルセクションのデッドロック (パッチ前の ntdll でも同じ)。
- インストーラが残ったもの: waffle / ensemble / marine / pulltop / blackcyc など。`scripts/tools/install_pass.sh` で GUI 自動インストールに回せる (未実行)。
- 展開の失敗 3 本 (lass-pixy 2019, erectlip 2012, lilim-darkness 2007) は `extract.sh` の判定を直したので再実行待ち (`downloads/suezou/redo4.tsv`)。

## 起動確認済みタイトル一覧

<!-- TITLES:BEGIN -->
タイトル画面以降まで目視で確認: **211 本** (ロゴ・注意画面止まりの 15 本は下の折りたたみに別記)。
年 = 配布ミラーの掲載年度 (「-」は第1弾の手動テスト分)。エンジンの「-」は自動判定できなかったもの。

| # | 年 | タイトル | エンジン | 確認できたところ | ID |
|---|---|---|---|---|---|
| 1 | 2006 | その横顔を見つめてしまう ～a profile完全版～ web体験版 | KiriKiri | タイトル | `b2006_akabeesoft` |
| 2 | 2006 | EXTRAVAGANZA～蟲愛でる少女～体験版 | - | タイトル | `b2006_blackcyc` |
| 3 | 2006 | 『オイラは番台２』体験版 | - | タイトル | `b2006_interheart` |
| 4 | 2006 | 淫皇覇伝アマツ（体験版） | Kaguya | タイトル | `b2006_kaguya` |
| 5 | 2006 | とらぶるっ！ | - | タイトル | `b2006_lilim` |
| 6 | 2006 | いつか、届く、あの空に。【体験版】 | BGI/Ethornell | タイトル→環境設定 | `b2006_lumpofsugar` |
| 7 | 2006 | ソウルイーター　体験版　Game Edition | - | ゲーム開始 (名前入力) | `b2006_lune` |
| 8 | 2006 | 人妻搾乳飯店－体験版－ | - | タイトル | `b2006_marine` |
| 9 | 2006 | ソウルレイザー☆ユナ 体験版 | - | タイトル | `b2006_mille_feuille` |
| 10 | 2006 | 機械仕掛けのイブ　～Ｄｅａ　Ｅｘ　Ｍａｃｈｉｎａ～ | KiriKiri | タイトル | `b2006_ninetail` |
| 11 | 2006 | ななついろ★ドロップス-体験版 | - | 本編 | `b2006_unisonshift` |
| 12 | 2006 | いな☆こい！ ～お稲荷さまとモテモテのたたり～ 体験版 ver 1.00 | YU-RIS | タイトル | `b2006_whirlpool` |
| 13 | 2007 | 必殺痴漢人　- TRIAL VERSION - | - | タイトル→メニュー | `b2007_anim` |
| 14 | 2007 | 姫奴隷 | - | タイトル | `b2007_bishop` |
| 15 | 2007 | かみぱに！体験版 | QLIE | タイトル | `b2007_clochette` |
| 16 | 2007 | あかね色に染まる坂 | - | タイトル | `b2007_feng` |
| 17 | 2007 | ハーレム×すくらっち体験版 | RealLive | タイトル | `b2007_hadashi` |
| 18 | 2007 | でるた！（体験版） | Kaguya | タイトル | `b2007_kaguya` |
| 19 | 2007 | 看護戦隊ナースレンジャー | - | タイトル | `b2007_lilim` |
| 20 | 2007 | 風輪奸山　体験版 | - | タイトル | `b2007_lune` |
| 21 | 2007 | しおふきマーメイド体験版 | - | ゲーム開始 (名前入力) | `b2007_marine` |
| 22 | 2007 | リリカル♪りりっく体験版 | EntisGLS | コンフィグ画面 | `b2007_marmalade` |
| 23 | 2007 | 『Bullet Butlers』Web体験版　ver1.1 | - | 本編 | `b2007_propeller` |
| 24 | 2007 | 聖炎天使エレアノール 体験版 | RealLive | タイトル | `b2007_sagaplanets` |
| 25 | 2007 | b2007_whirlpool | YU-RIS | タイトル (最終画面は暗転) | `b2007_whirlpool` |
| 26 | 2008 | ＷＬＯ-世界恋愛機構- web用体験版 | KiriKiri | タイトル | `b2008_akabeesoft` |
| 27 | 2008 | 息子の友達に犯されて  - TRIAL VERSION - | - | タイトル | `b2008_anim` |
| 28 | 2008 | Hime のち Honey - 体験版 | KiriKiri | タイトル | `b2008_asa_project` |
| 29 | 2008 | Venus Blood Chimera | KiriKiri | タイトル | `b2008_dualtail` |
| 30 | 2008 | ヴェルディア幻奏曲体験版 | Escude | 本編 | `b2008_escude` |
| 31 | 2008 | お姉さん×すくらっち体験版 | RealLive | タイトル | `b2008_hadashi` |
| 32 | 2008 | FairlyLife WEB体験版02 H_Only Ver1.00 | - | コンフィグ画面 | `b2008_hook` |
| 33 | 2008 | 夏神（体験版０２） | - | タイトル | `b2008_kaguya` |
| 34 | 2008 | びんかんアスリート | - | タイトル | `b2008_marine` |
| 35 | 2008 | 魔法少女沙枝 Vol.2 体験版 | KiriKiri | タイトル (MSI) | `b2008_mille_feuille` |
| 36 | 2008 | 晴れハレはーれむ 体験版Ver1.00 | - | タイトル | `b2008_smee` |
| 37 | 2009 | 愛娘という名の玩具　- TRIAL VERSION - | - | タイトル | `b2009_anim` |
| 38 | 2009 | 美乳淫妹 遥「私にイヤらしいコスプレさせるなんて……この変態！」 | Softpal | タイトル | `b2009_crossover` |
| 39 | 2009 | 夏ノ雨 -体験版- | EntisGLS | タイトル | `b2009_cube` |
| 40 | 2009 | 魔法少女ラヴィリオン 体験版 | KiriKiri | タイトル | `b2009_dualtail` |
| 41 | 2009 | さらさらささら（体験版） | - | タイトル | `b2009_kaguya` |
| 42 | 2009 | あい☆きゃん | - | タイトル | `b2009_lilim` |
| 43 | 2009 | キスと魔王と紅茶（体験版） | EntisGLS | タイトル | `b2009_marmalade` |
| 44 | 2009 | シグナルハート 体験版 Ver.01 | QLIE | 本編 | `b2009_purplesoftware` |
| 45 | 2010 | 置き場がない！ web体験版 | KiriKiri | タイトル | `b2010_akabeesoft` |
| 46 | 2010 | 贄姫 - TRIAL VERSION - | - | タイトル | `b2010_anim` |
| 47 | 2010 | 学園３～華麗なる悦辱～ 体験版 | - | タイトル | `b2010_bishop` |
| 48 | 2010 | JINKI EXTEND Re:VISION -SECOND IMPACT- | NeXAS | タイトル | `b2010_giga` |
| 49 | 2010 | Reunion～３日間だけのプチ同棲～　体験版 | KiriKiri | タイトル | `b2010_grand_cru` |
| 50 | 2010 | キラリ☆南国小麦色　体験版 | - | タイトル | `b2010_kaguya` |
| 51 | 2010 | 痴覚過敏 体験版 | EntisGLS | タイトル | `b2010_undermoon` |
| 52 | 2010 | 涼風のメルト -Where wishes are drawn to each other- WEB体験版 | YU-RIS | タイトル | `b2010_whirlpool` |
| 53 | 2011 | NTR彼女はいじめられっ娘 - TRIAL VERSION - | - | タイトル | `b2011_anim_team_mm` |
| 54 | 2011 | 裏教師～背徳の淫悦授業～ 体験版 | - | タイトル | `b2011_bishop` |
| 55 | 2011 | シオンの血族 | SiglusEngine | タイトル | `b2011_catwalk` |
| 56 | 2011 | Starless 体験版 | - | タイトル (独自インストーラ) | `b2011_empress` |
| 57 | 2011 | りとる†びっち ～Little Bitch Girls in Hog Farm～ 体験版 | Softpal | タイトル | `b2011_erectlip` |
| 58 | 2011 | ホチキス - 体験版 | NeXAS | タイトル | `b2011_giga` |
| 59 | 2011 | はっぴぃふぁくとりー　体験版 | KiriKiri | タイトル | `b2011_grand_cru` |
| 60 | 2011 | 『種憑け村』序章体験版・導ノ儀 | ShiinaRio | タイトル | `b2011_guilty` |
| 61 | 2011 | StrawberryNauts Ver1.00 | - | タイトル | `b2011_hook` |
| 62 | 2011 | アルバイトの先輩の女のコに仕事を教わっていたら始まっちゃうＨな関係。　体験版 | - | タイトル | `b2011_kaguya` |
| 63 | 2011 | ユユカナ 体験版 | KiriKiri | タイトル (従来 32bit Wine。新 WoW64 だと kernel32 認証エラー) | `b2011_nanawind` |
| 64 | 2011 | 姫様限定！～PrincessLimited～web体験版ver1.0 | - | タイトル | `b2011_princess_sugar` |
| 65 | 2011 | ラブラブル体験版 Ver1.00 | - | タイトル | `b2011_smee` |
| 66 | 2012 | JOKER-死線の果ての道化師- | KiriKiri | タイトル | `b2012_akabeesoft3` |
| 67 | 2012 | 中の人などいない！　体験版 | KiriKiri2 | タイトル (quartz パッチ) | `b2012_alcot` |
| 68 | 2012 | 母娘乱館 体験版 | AliceSoft System4 | 本編テキスト | `b2012_alicesoft` |
| 69 | 2012 | 学園ＮＴＲ -僕の知らない彼女の淫顔- 体験版 | - | タイトル | `b2012_atelier_gh` |
| 70 | 2012 | 美脚隷嬢優奈「脚もお尻もこんなに穢すなんて……絶対に許さない！」体験版 | KiriKiri | タイトル | `b2012_cross_over` |
| 71 | 2012 | 星空へ架かる橋ＡＡ 円佳SPアフター体験版 | - | タイトル | `b2012_feng` |
| 72 | 2012 | つぼい君のスイッチ！体験版 | - | 本編 | `b2012_fudegaki_soft` |
| 73 | 2012 | キスベル　体験版 | NeXAS | タイトル | `b2012_giga` |
| 74 | 2012 | リヴォルバーガール☆ハンマーレディ　体験版 | KiriKiri | タイトル | `b2012_kai` |
| 75 | 2012 | 恋妹SWEET☆DAYS 体験版 | KiriKiri | タイトル | `b2012_parasol` |
| 76 | 2012 | 籠女の繭 体験版 | - | タイトル | `b2012_tinkerbell` |
| 77 | 2012 | MonsterParkⅡ | - | タイトル | `b2012_trois` |
| 78 | 2013 | ランス01体験版 | AliceSoft System4 | タイトル | `b2013_alicesoft` |
| 79 | 2013 | 息子の友達に堕とされて　体験版 | YU-RIS | タイトル | `b2013_anim` |
| 80 | 2013 | 花嫁狩凌 ～絶対快楽の白濁淫毒～ 体験版 | KiriKiri | タイトル | `b2013_cross_over` |
| 81 | 2013 | VenusBlood-GAIA- WEB専用ADV体験版 | KiriKiri | タイトル | `b2013_dualtail` |
| 82 | 2013 | お嬢様はご機嫌ナナメ 体験版 | AdvHD | タイトル (InstallShield) | `b2013_ensemble` |
| 83 | 2013 | キスアト体験版 | NeXAS | タイトル | `b2013_giga` |
| 84 | 2013 | PriministAr（体験版） Ver1.00 | - | タイトル | `b2013_hooksoft` |
| 85 | 2013 | 妹のおかげでモテすぎてヤバい。 体験版 | KiriKiri | タイトル | `b2013_hulotte` |
| 86 | 2013 | 肉牝Ｒ３０（体験版） | - | タイトル | `b2013_kaguya` |
| 87 | 2013 | ナマイキデレーション体験版 WEB Ver. | KiriKiri | タイトル | `b2013_madosoft` |
| 88 | 2013 | ぜったいマジラブ！あまイチャはぷにんぐ！体験版 Ver 1.0 | - | タイトル | `b2013_tinkerbell` |
| 89 | 2014 | 体験版 甘えてくれる恋人とのエッチなひととき編 | CatSystem2 | タイトル | `b2014_azarashi_soft` |
| 90 | 2014 | Re;Lord ～ヘルフォルトの魔女とぬいぐるみ～ - 体験版 Ver. | Escude | タイトル | `b2014_escude` |
| 91 | 2014 | しゃぶらレンタル　体験版 | - | タイトル | `b2014_kaguya` |
| 92 | 2014 | 奪われた学園～罠に落ちた優等生～　体験版 | KiriKiri | タイトル | `b2014_undermoon` |
| 93 | 2014 | こすぷれえっち 体験版 | - | タイトル (MS 別名フォント) | `b2014_wendybell` |
| 94 | 2014 | G.I.B. ガールズ・イン・ブラック  WEB体験版 | YU-RIS | タイトル | `b2014_whirlpool` |
| 95 | 2015 | お兄ちゃん、教えて……～義妹とヒミツの保健実習～　体験版　Ver1.00 | KiriKiri | タイトル | `b2015_ammolite` |
| 96 | 2015 | 愛娘は、マゾ性隷～娘だからって遠慮しないでね？～　体験版　Ver1.00 | KiriKiri | タイトル | `b2015_appetite` |
| 97 | 2015 | ClosedGAME体験版 | Majiro | タイトル | `b2015_empress` |
| 98 | 2015 | 巨乳大魔王のドスケベクエスト ～完全敗北した少年勇者クンＵＣ～　【体験版】 | Softpal | タイトル | `b2015_erectlip` |
| 99 | 2015 | 淫らに喘ぐ七人の夜這いする孕女　体験版 | AdvHD | タイトル | `b2015_guilty` |
| 100 | 2015 | ろーらいず!!! 体験版 | SiglusEngine | タイトル (TZ=Asia/Tokyo) | `b2015_hamham` |
| 101 | 2015 | トラベリングスターズ 体験版02 Ver1.00 | BGI/Ethornell | タイトル (ログに例外あり) | `b2015_hooksoft` |
| 102 | 2015 | ハラミタマ　体験版 | - | タイトル | `b2015_kaguya` |
| 103 | 2015 | 極煌戦姫ミストルティア 体験版 | KiriKiri | タイトル | `b2015_mille_feuille` |
| 104 | 2015 | おしおき生意気ギャル ～エロいオシオキをしたら自分からエッチを求めてきた～ 体験版 | KiriKiri | タイトル | `b2015_pachipachi_soft` |
| 105 | 2016 | 満淫オーク電車～くっ！　任務じゃなければこんな場所…～　体験版　Ver1.00 | KiriKiri | タイトル | `b2016_apadash` |
| 106 | 2016 | アナル＊ティーチャー～感度良肛！？　逆レッスン～　体験版　Ver1.00 | KiriKiri | タイトル | `b2016_appetite` |
| 107 | 2016 | 間宮くんちの五つ子事情 体験版 | KiriKiri | タイトル | `b2016_cube` |
| 108 | 2016 | お嬢様はイイナリみたいです！？ 体験版 | - | タイトル | `b2016_evoll` |
| 109 | 2016 | リプキス　体験版 | NeXAS | タイトル | `b2016_giga` |
| 110 | 2016 | はらかつ！ ～気になるあの子と子作りエッチ～ Trial Version: 1.0.0.0 [data ver:1.00] | - | 本編 | `b2016_inter_heart_glossy` |
| 111 | 2017 | いつまでも俺は母に恋してる！  - 体験版２ - | - | タイトル | `b2017_anim` |
| 112 | 2017 | 催眠アプリで寝取りハメ～これまでの恨み、ヤり返してやる！　体験版　Ver1.00 | KiriKiri | タイトル | `b2017_appetite` |
| 113 | 2017 | はらかつ２　～子作りエッチでお悩み解決～ Trial Version: 1.0.0.0 [data ver:1.00] | - | 本編テキスト | `b2017_inter_heart_glossy` |
| 114 | 2017 | 女装千年王国　体験版 | YU-RIS | タイトル | `b2017_no_strike` |
| 115 | 2017 | ことのは・アムリラート | KiriKiri | タイトル (OP動画再生後) | `b2017_sukerasparo` |
| 116 | 2017 | 透けちゃって！！～九死に一生を得て濃密セカンドライフ～ Trial Version: 1.0.0.0 [data ver:1.00] | - | 本編テキスト | `b2017_sweet_heart` |
| 117 | 2017 | あなたに恋する恋愛ルセット　【体験版】 | Softpal | タイトル | `b2017_unisonshift` |
| 118 | 2018 | わんこの嫁入り ～ようこそ！いぬのしっぽへ～ 体験版 version 1.00 | - | タイトル | `b2018_alumi_soft` |
| 119 | 2018 | P.S.あなたを心から愛しています～最期まで気づけなかった妻の苦しみ～　体験版 | KiriKiri | タイトル | `b2018_appetite` |
| 120 | 2018 | 彼女が異種族だった場合～クールな先輩ダークエルフとの甘やかし同棲日誌～体験版 | YU-RIS | タイトル | `b2018_casket` |
| 121 | 2018 | 爆乳催眠女教師　【体験版】 | Softpal | タイトル | `b2018_erectlip` |
| 122 | 2018 | 催眠学園１年生体験版 | - | タイトル | `b2018_fudegaki_soft` |
| 123 | 2018 | 俺の上であがく六人の伽女　体験版２ | AdvWin32/WillPlus | タイトル | `b2018_guilty` |
| 124 | 2018 | ノットフィクション！ Trial Version: 1.0.0.0 [data ver:1.00] | - | タイトル | `b2018_hachimitsu_soft` |
| 125 | 2018 | LoveHolic　体験版 | - | タイトル | `b2018_kaguya` |
| 126 | 2018 | 僕と彼女のご奉仕同棲　体験版 | AdvWin32/WillPlus | タイトル | `b2018_tokohana` |
| 127 | 2019 | いつまでも息子のままじゃいられない！５ - 体験版 - | - | タイトル | `b2019_anim` |
| 128 | 2019 | 女体化パニック！３～お、俺は男だ！　でも、悔しいけど気持ちいいっ……のか！？　体験版 | KiriKiri | タイトル | `b2019_appetite` |
| 129 | 2019 | アイドル裏営業～汚されたステージ衣装～体験版 | - | タイトル | `b2019_channel_ubai` |
| 130 | 2019 | VenusBlood FRONTIER International SLG Trial 日本語版 | KiriKiri | タイトル | `b2019_dualtail` |
| 131 | 2019 | アイキス 豪華体験版 | NeXAS | タイトル | `b2019_giga` |
| 132 | 2019 | どっちのｉが好きですか？　Ｈシーン体験版　Ver1.00 | BGI | タイトル | `b2019_hooksoft` |
| 133 | 2019 | お姉さん先生　体験版 | - | タイトル | `b2019_kaguya` |
| 134 | 2019 | WhiteBlue　web体験版ver2.0 | - | タイトル | `b2019_lilim_darkness` |
| 135 | 2019 | モウソウスピーカー～Ｈな妄想が聴こえてくるよ～　体験版 | KiriKiri | タイトル | `b2019_sator` |
| 136 | 2019 | おにギュＨ＿ＨＣＢ体験版 | KiriKiri | タイトル | `b2019_tinkle_position` |
| 137 | 2020 | 墓多 DYING ZOMBIES -体験版- | KiriKiri | タイトル | `b2020_akabeesoft3` |
| 138 | 2020 | 優来（ゆら）があいつに抱かれる理由（わけ） 体験版 | KiriKiri | タイトル | `b2020_at_sakura` |
| 139 | 2020 | アナベル・メイドガーデン体験版 | QLIE | タイトル (ログに例外あり) | `b2020_barista_lab` |
| 140 | 2020 | はじカノ -君がいる部屋- 体験版 | - | タイトル | `b2020_caramel_box_strawberry` |
| 141 | 2020 | 悪堕ち 魔導処女～絶対支配の触手調教～　体験版 | KiriKiri | タイトル | `b2020_chaos_r` |
| 142 | 2020 | 爆乳淫辱霊媒師　【体験版】 | Softpal | タイトル | `b2020_erectlip` |
| 143 | 2020 | アイシング-love coating- 体験版 Version: 1.0.0.1 [data ver:1.00] | Unity | タイトル | `b2020_hachimitsu_soft` |
| 144 | 2020 | リトルシックガール　～桃蜜は妹の香り～　体験版 | KiriKiri | タイトル | `b2020_lass_pixy` |
| 145 | 2020 | shin-neko ～再会した妹との新たな関係～　【体験版】 | Softpal | タイトル | `b2020_milimili` |
| 146 | 2020 | ティーリオ傭兵団の初仕事 体験版 v1.2.0 | - | 本編 (SRPG) | `b2020_rikuzendo` |
| 147 | 2021 | 愛する彼女は画面の中、他の男の上で腰をふる 体験版 | KiriKiri | タイトル | `b2021_at_sakura` |
| 148 | 2021 | 神聖昂燐ダクリュオン～正しい天使の育てかた～　体験版 | KiriKiri | タイトル | `b2021_chaos_r` |
| 149 | 2021 | アイドル姉妹をエッチにプロデュース！ 体験版 | KiriKiri | タイトル | `b2021_kelp` |
| 150 | 2021 | 魔法少女消耗戦線DEADΩAEGIS_体験版 | - | タイトル | `b2021_metalogiq` |
| 151 | 2021 | ちんかもツインズ２！　～夏だ！海だ！ふたごの妹おっぱい対決だ！～　【体験版】 | Softpal | タイトル | `b2021_milimili` |
| 152 | 2021 | プリンセス☆シスターズ！～四姉妹は全員あなたの許嫁～　web体験版ver1.0 | - | タイトル | `b2021_princess_sugar` |
| 153 | 2021 | 人妻LovelyMoment | - | タイトル | `b2021_stick` |
| 154 | 2021 | 淫妖蟲 穢 ～凌蝕桃源退魔録～体験版 Version01 | - | タイトル | `b2021_tinkerbell` |
| 155 | 2021 | けもの道☆ガーリッシュスクエア　体験版 | YU-RIS | タイトル | `b2021_whirlpool` |
| 156 | 2022 | 黒ギャル子ちゃんとしっぽり温泉旅行 ～Hしまくり三泊四日の旅～　【体験版】 | Softpal | タイトル | `b2022_amuse_craft_erotica` |
| 157 | 2022 | 愛妻Ｘ交姦妊活スワッピング - 体験版 - | - | タイトル | `b2022_anim` |
| 158 | 2022 | 愛する恋人を大嫌いな旧友に寝取られた件 体験版 | KiriKiri | タイトル | `b2022_at_sakura` |
| 159 | 2022 | BstTrial | Unity | タイトル | `b2022_ba_ku` |
| 160 | 2022 | あやかし遊郭～花魁道中～体験版 | YU-RIS | タイトル | `b2022_casket` |
| 161 | 2022 | 進軍ゴブリン軍団～人類総苗床化作戦～ 体験版 | KiriKiri | タイトル | `b2022_chaos_r` |
| 162 | 2022 | 華は短し、踊れよ乙女 - 体験版 | AdvWin32/WillPlus | タイトル | `b2022_ensemble` |
| 163 | 2022 | 南国プリズン体験版 | - | タイトル | `b2022_evoll` |
| 164 | 2022 | リンパにATATA! ～メス牡蠣ミルクどぴゅらっしゅ♡～ | KiriKiri | タイトル | `b2022_hending` |
| 165 | 2022 | 羞恥隷嬢学園 | - | タイトル | `b2022_lunaprism` |
| 166 | 2022 | 触災の島～Island of the Dead～体験版　Ver1.00 | KiriKiri | タイトル | `b2022_syoku` |
| 167 | 2023 | 最強の人妻ヒロインが寝取られるまで～お願い！夫にはバラさないで…～　体験版　Ver1.00 | KiriKiri | タイトル | `b2023_apadash` |
| 168 | 2023 | 洗脳航路～エイリアンの苗床に導かれるエリート女軍人たち～　体験版　Ver1.00 | KiriKiri | タイトル | `b2023_appetite` |
| 169 | 2023 | 堕とされた義姉 体験版 | KiriKiri | タイトル | `b2023_at_sakura` |
| 170 | 2023 | 神聖昂燐エストランジェ体験版 | KiriKiri | タイトル | `b2023_chaos_r` |
| 171 | 2023 | 爆乳遊廓 ～癒され太夫孕ませの夜鶴～　【体験版】 | Softpal | タイトル | `b2023_erectlip` |
| 172 | 2023 | 夜勤病棟 リメイク 体験版 | - | タイトル | `b2023_fg_remake` |
| 173 | 2023 | 瑠璃桜 | KiriKiri | タイトル | `b2023_lump_of_sugar_all` |
| 174 | 2023 | 非道狩～ヒトカリ～ 体験版 | Softpal | タイトル | `b2023_turumiku_another` |
| 175 | 2023 | モラトリアム 体験版 | - | タイトル | `b2023_wendybell` |
| 176 | 2024 | 性調教済だった愛する彼女 体験版 | KiriKiri | タイトル | `b2024_at_sakura` |
| 177 | 2024 | ちょっとナマイキなトイレの花子さん ～えっちぃ学園のおばけとエロエロ搾精性活～ 【体験版】 | Softpal | 本編テキスト | `b2024_milimili` |
| 178 | 2024 | 狙われた優等生　身代わりの代償　体験版 | EntisGLS | タイトル | `b2024_syrup_mm` |
| 179 | 2024 | ムジナ 体験版Ver02 | - | タイトル (kernelbase + ntdll パッチ) | `b2024_tinkerbell` |
| 180 | 2025 | 学生会長・紫藤喜那の淫鬱 体験版 | KiriKiri | タイトル | `b2025_at_sakura` |
| 181 | 2025 | のーぶるバトラー TRIAL EDITION | AdvWin32/WillPlus | タイトル | `b2025_ensemble_sweet` |
| 182 | 2025 | 爆乳淫乱ビーチ　体験版 | Softpal | タイトル | `b2025_erectlip` |
| 183 | 2025 | Vanilla Android　シコ猿DT大学生の俺と美少女アンドロイドがいろんなプレイでハメパコミッション　体験版 | KiriKiri | タイトル | `b2025_monoceros_plus` |
| 184 | 2025 | 小日向柚月と初夜したいっ！　【体験版】 | Softpal | タイトル | `b2025_unisonshift` |
| 185 | 2026 | ママラブ！２～俺の彼女は俺のママ☆世話焼き巨乳凛桜ママ～ - 体験版 - | - | タイトル | `b2026_anim` |
| 186 | 2026 | ディメンション凸ラバース!! 【体験版】 | Softpal | タイトル | `b2026_crystalia` |
| 187 | 2026 | どいなかガールズゆーとぴあ Trial Version: 1.0.0.0 [data ver:1.00] | - | タイトル | `b2026_dessert_soft` |
| 188 | 2026 | OfficeLove -クールな上司と秘密のイキ抜き- Trial Edition | AdvWin32/WillPlus | タイトル | `b2026_tokohana` |
| 189 | 2026 | 友恵の探偵物語～のぞき魔な私の天職～体験版 | - | タイトル | `b2026_wendybell` |
| 190 | - | 有閑夫人倶楽部 体験版 | AdvHD | 本編テキスト (icu シム) | `r1_advhd` |
| 191 | - | 遥かなるニライカナイ 体験版 | Artemis | 本編 + 音声 | `r1_artemis` |
| 192 | - | クダンノフォークロア 体験版 | BGI | 本編テキスト (独自Setup) | `r1_bgi_kudan` |
| 193 | - | 果つることなき未来ヨリ 体験版 | CatSystem2 | 本編テキスト | `r1_cs2_hatsumira` |
| 194 | - | 彼岸花の咲く夜に お試し版 | NScripter | 本編 (CreateInstall) | `r1_higanbana` |
| 195 | - | KAG3 紹介デモ | KiriKiri2 | 本編テキスト | `r1_kag3` |
| 196 | - | 先輩、好きです……押忍！ 体験版 | KiriKiri | 本編テキスト | `r1_krkr_apt306` |
| 197 | - | あいれぼ 体験版 | Majiro | 本編テキスト (独自インストーラ) | `r1_majiro` |
| 198 | - | Wind RP 体験版 | Musica | 本編テキスト | `r1_musica` |
| 199 | - | ナルキッソス SIDE 2nd | NScripter | 本編テキスト | `r1_naru2` |
| 200 | - | 天翼のアスクレイン 体験版 | QLIE | 本編テキスト | `r1_qlie` |
| 201 | - | 5 -ファイブ- 体験版 | RealLive | 本編テキスト (SFX) | `r1_reallive_five` |
| 202 | - | The Question | Ren'Py | 本編テキスト | `r1_renpy` |
| 203 | - | でか乳巫女と巨乳シスター 体験版 | SakanaGL | タイトル (Managed=N) | `r1_sakana` |
| 204 | - | うみねこのなく頃に咲 体験版 | PONScripter | 本編テキスト | `r1_saku` |
| 205 | - | プリマドール 体験版 | SiglusEngine | 本編テキスト | `r1_siglus_primadoll` |
| 206 | - | トイレの花子さん 体験版 | Softpal | 本編テキスト | `r1_softpal_hanako` |
| 207 | - | 転性魔王さま 体験版 | Softpal | 本編テキスト | `r1_softpal_tenmao` |
| 208 | - | トライアンソロジー 体験版 | NScripter | 本編 | `r1_trt` |
| 209 | - | 転生した世界で… 体験版 | TyranoScript | 本編テキスト | `r1_tyrano` |
| 210 | - | 旭光のマリアージュ 体験版 | Unity | 本編テキスト (vnbox Wine) | `r1_unity` |
| 211 | - | YU-RIS サンプル | YU-RIS | 演出/動画 | `r1_yuris` |

<details><summary>弱い確認 (ロゴ・注意画面・起動ダイアログまで) 15 本</summary>

| 年 | タイトル | 止まったところ | ID |
|---|---|---|---|
| 2008 | いたずら極―リアル―　体験版 | 注意画面以降 | `b2008_interheart` |
| 2009 | らぶでれーしょん！ Ver1.00 | ロゴ以降 | `b2009_smee` |
| 2014 | ＰＲＥＴＴＹ×Ｃ∧ＴＩＯＮ | 注意画面 | `b2014_hibiki_works` |
| 2014 | 春風センセーション！体験版 | 注意画面以降 | `b2014_windmill_oasis` |
| 2016 | 小悪魔発育チュ～！（体験版） | ロゴ以降 | `b2016_kaguya` |
| 2017 | なちゅらるばけーしょん | 年齢注意画面 | `b2017_hibiki_works` |
| 2017 | ニュートンと林檎の樹　体験版＋ | 起動 (インストーラ経由) | `b2017_laplacian` |
| 2017 | 隣人に壊されていく俺の妻 体験版 | 解像度選択画面 (WinRAR SFX) | `b2017_waffle` |
| 2018 | 恋はそっと咲く花のように　体験版 | 注意画面以降 | `b2018_ensemble` |
| 2018 | グランギニョルの夜 体験版Ver.01 | 注意画面→CG | `b2018_tinkerbell` |
| 2020 | 世話焼き奥さんで人の頼みを断れない亜衣さんにお願いして中出しハメ放題のドスケベ妻になってもらった 体験版 | 起動 | `b2020_pin_point` |
| 2021 | 売国王姫～堕落のメス豚母娘～体験版 | CG表示 (タイトルか未確定) | `b2021_pin_point` |
| 2025 | お姉ちゃんのあまエロレッスン２ ～お隣の巨乳お姉ちゃんにイッパイ甘やかされるめちゃエロライフ～  - 体験版 - | 注意画面 (Managed=N) | `b2025_anim` |
| 2025 | 虜ノ焔 ～淫欲の炎に満ちた地下闘技場～ 体験版 | ロゴ以降 | `b2025_guilty` |
| 2026 | Triptych ～トリプティック～ 体験版 | 注意画面 | `b2026_tinkerbell` |

</details>
<!-- TITLES:END -->

## 分かったこと (落ちる原因と対策)

1. **仮想デスクトップは不要、むしろ害**: NScripter (DirectDraw) で描画がデスクトップ原点にずれた。ウィンドウ単位にしたら解消し、配信とも相性が良い。
2. **ロケール**: `ja_JP.UTF-8` を locale-gen した上で prefix を作るとコードページ 932 になる。Shift-JIS のアーカイブ名は展開時に UTF-8 へ変換しないと Wine から見えない (`extract.sh`)。
3. **32bit / 64bit を exe で切り替える**: 古い VN は 32bit prefix (Win7)、Ren'Py 8 や新しいエンジンは 64bit prefix (Win10)。`requires Windows 10` で起動拒否されるものがある。
4. **icu.dll**: Win10 標準だが Wine に無い。AdvHD は起動前に c0000135 で落ちる → 公式 ICU への転送 DLL をベースに標準装備。
5. **インストーラ**: Wine 上で普通にインストールし、prefix の差分 (レジストリはテキストファイル同士の diff) を取るだけで DoNotInstaller 相当になる。ゲーム固有キーは `HKLM\Software\<ブランド>\<タイトル>` 系 (BGI はこれが無いと起動しなかった)。Wine 自身が書く MMDevices / winepulse / MenuFiles / Console / デバイス列挙は除外済み。
6. **WM なし環境の注意**: キー入力は毎回 `windowfocus` が必要。D3D/GL のウィンドウは `import -window` 直撮りだと黒いことがあるので画面から矩形を切り出す。仮想画面は 720p ゲーム + 枠より大きくしないと「解像度が足りない」と縮小される (QLIE)。
7. **動画**: DirectShow (quartz + winegstreamer, i386 の GStreamer プラグイン必須) で MPEG-1 は再生されるが、SampleGrabber 方式の NScripter ではタイトル動画が約 8fps。LAV Filters でも同値。GPU 環境で再確認が必要。
8. **起動ダイアログ**: CatSystem2 (画面モード選択) や QLIE (起動設定) はレシピの操作列で自動通過できる。Softpal の「初回起動時のウインドウモード」ダイアログはクリックも Enter も効かず、**フォーカスを当てて Space** で進む (`wizard.sh` が自動でフォールバック)。
9. **1x1 ウィンドウ**: フルスクリーン化を WM 任せにするゲーム (SakanaGL, FVP 系 ANIM 作品) は、WM なしだと X ウィンドウが 1x1 のまま。`X11 Driver\Managed=N` (Wine 自身がウィンドウを管理) で解決。`run.sh` / `play.sh` が 1x1 を検出すると自動でこの設定にして起動し直す。全体に掛けると Unity などが黒画面になるので、検出時だけ切り替える。
10. **タイムゾーン**: SiglusEngine 系の一部は「日本語版 Windows 判定」をタイムゾーンで行い、UTC だと起動を拒否する。`env.sh` で `TZ=Asia/Tokyo` を既定にした。
12. **MS フォントの実体**: こすぷれえっち (2014) は `EnumFontFamiliesEx` で「ＭＳ ゴシック」を名前で探し、無いと CRT の不正パラメータ例外で落ちた。Wine の置換 (Replacements) は列挙に出ない。IPA フォントの name テーブルを書き換えた「ＭＳ ゴシック / Ｐゴシック / 明朝 / Ｐ明朝 / UI Gothic」を prefix の Fonts に登録して解決 (`scripts/verbs/msfonts.sh`、ベースに標準装備)。
13. **従来 32bit Wine が要るもの**: ユユカナ (2011, nanawind) は「Authentication failed : unrecognized kernel32 module」。新 WoW64 では 32bit の kernel32 が syswow64 から読まれるためで、WineHQ の従来 32bit prefix では動いた。NG 時は `scripts/tools/retry_pass.sh` が従来 Wine で再試行する。
14. **起動の遅いゲーム**: BGI は起動に 40 秒ほど黒画面のことがある。1x1 ウィンドウ検出は 20 秒待ってから Managed=N に切り替えるようにした。
11. **インストーラの自動操作**: MSI は `msiexec /qn`、それ以外 (CreateInstall / BGI / Majiro / CatSystem2 / InstallShield / 独自) は `wizard.sh` が小さいダイアログに Enter (効かなければ Space) を送り続けるだけで、今回の 8 種すべて完了した。WinRAR SFX は Linux 側の unrar で開けないことがあり、Wine で SFX 自体を動かして展開した。

## 次にやること

- 映像を WebRTC 化 (H.264 + Opus) し、配信にログイン (認証) を付ける。タッチ操作 (スワイプ=バックログ等) の割り当て。
- 本物の S3 / R2 で `store.sh` を通しで確認。複数サーバで同じ人のセーブを同時に書かないよう、プレイ中はロック (S3 上のロックファイル) を置く。
- 黒画面のまま・インストーラが残ったタイトルを `retry_pass.sh` (従来 32bit Wine) / `install_pass.sh` で再試行。
- GPU のある自宅サーバで動画のフレームレートを再計測。
- エンジン別にブラウザ内実行 (WASM) へ逃がせるもの: 吉里吉里 (krkrsdl2)、NScripter/PONScripter (onsyuri)、Ren'Py (web 書き出し)、TyranoScript (元々 HTML5)。それ以外は配信で。

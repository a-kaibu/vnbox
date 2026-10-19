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
   Xvfb :N (WM なし) ──x11grab──▶ ffmpeg H.264 ──────┐   WebRTC (映像・音声・入力のデータチャネル)
   PulseAudio null シンク ──monitor──▶ ffmpeg Opus ────┤ vnbox serve (HTTP :port) ◀──▶ ブラウザ (Android 等)
   xdotool ◀── データチャネル / POST /input (タップ・キー) ┘
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

Go 版 (`vnbox serve` / `vnbox play`) は **WebRTC (H.264 + Opus)** で配信し、繋がらないときだけ MJPEG に切り替える。

| 経路 | 方式 | 実装 |
|---|---|---|
| 映像 | ゲームウィンドウの矩形だけを `ffmpeg -f x11grab` で取り込み、libx264 (constrained baseline, zerolatency, 30fps, CRF 23, 上限 3Mbps) で H.264 にして WebRTC で送る。H.264 を復号できないブラウザには VP8。エンコーダはゲームごとに 1 つで、見ている人全員に配る (誰も見ていなければ 20 秒後に止める) | `go/internal/stream/rtc.go` |
| キーフレーム | 定期は 10 秒ごと。新しい視聴者が来たときと、視聴者がパケット欠落を知らせたとき (PLI / FIR) にエンコーダを起動し直して即座にキーフレームを出す (3 秒に 1 回まで) | 同上 |
| 音声 | ゲーム専用の PulseAudio null シンクの monitor を Opus 96kbps (20ms) にして WebRTC で送る。モバイルは最初のタップで消音を解除 | 同上 |
| 入力 | WebRTC のデータチャネルで JSON を受け、表示倍率を逆算した座標で `xdotool` がそのウィンドウへ送る (データチャネルが無いときは `POST /input`)。タッチ操作は下記 | `stream.go` / `page.html` |
| シグナリング | ページが offer を `POST rtc?codec=h264|vp8` に送り、ICE 収集を済ませた answer を返す 1 往復 (ポータルの認証の内側) | `rtc.go` |
| 代替 | WebRTC が 12 秒以内に繋がらなければ MJPEG (`multipart/x-mixed-replace`) + Opus/WebM に切り替える | `stream.go` `/video` `/audio` |

帯域の実測 (1280x720、同じ画面を同じ時間受信):

| 画面 | MJPEG (15fps, q5) | WebRTC H.264 (30fps) |
|---|---|---|
| Hello,good-bye タイトル (背景が動く) | 34 Mbps | 0.25 Mbps |
| Hello,good-bye 本編 (全面にフィルム調のノイズ演出) | 12 Mbps | 約 3 Mbps (上限に張り付く) |
| あいれぼ タイトル (ほぼ静止) | 14 Mbps | 0.08 Mbps |

H.264 は前の画面との差分だけを送るので、止まっている時間が長いノベルゲームでは MJPEG の 1/50〜1/150 になる。全面が動く演出のある場面だけ上限 (`-bitrate`) まで使う。

**ネットワーク設定** (`vnbox serve` / `play` の引数):

- 同じ LAN / Tailscale などの VPN 内: 設定不要 (ホスト候補で直接繋がる)。
- ルータでポートを開ける: `-rtc-port 8443 -public-ip <グローバル IP>`。映像・音声を 1 つの UDP ポートにまとめ、同じ番号の TCP でも受ける (UDP が通らない回線向け)。HTTP (`-addr`) と合わせて 2 ポートを転送すればよい。
- 開けられない (CGNAT 等): `-ice stun:stun.l.google.com:19302,turn:user:pass@turn.example.com:3478` で STUN/TURN を使う (環境変数 `VNBOX_ICE` / `VNBOX_PUBLIC_IP` でも可)。
- 動作確認用に `bin/rtcprobe` (`go/cmd/rtcprobe`): ブラウザ無しで WebRTC 接続し、受け取った H.264 / Opus をファイルに保存して kbps を表示する。

**タッチ操作** (スマホ・タブレット):

| 操作 | 動作 |
|---|---|
| タップ | その位置をクリック (読み進め・選択) |
| 長押し / 2 本指タップ | 右クリック (メニュー、ウィンドウ消し) |
| 下へスワイプ / 上へスワイプ | ホイール上 (バックログ) / ホイール下 |
| ☰ メニュー | 決定 (Enter)、右クリック、バックログ、スキップ (Ctrl 押しっぱなしの切り替え。2 分で自動解除)、Space、Esc、文字入力 (名前入力用)、全画面 (横向き固定)、受信中の方式と帯域の表示 |

- 1x1 のまま表示されないゲーム (フルスクリーンを WM 任せにするもの) は `Managed=N` で起動し直す。
- シェル版の試作 (`stream.py`) は MJPEG のみで認証も無い。使うなら必ずリバースプロキシ等の内側に置くこと。

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

## Go 実装 (`go/`, コマンド `vnbox`)

シェル/Python の試作 (`scripts/`) と同じことを 1 つのバイナリで行う。データの置き場所・パッケージ形式・S3 上の配置はシェル版と共通なので混在して使える。

```bash
cd go && go build -o ../bin/vnbox ./cmd/vnbox        # Go 1.24+
bin/vnbox add mygame ~/Downloads/trial.zip -title "○○ 体験版"   # パッケージ化 (VNBOX_S3 があれば S3 にも上げる。-no-upload で抑止)
bin/vnbox add mygame setup.zip -click "157,408"     # インストーラの最初の画面 (BGI のメニュー等) でクリックする座標 (ウィンドウ基準)
bin/vnbox meta mygame -fetch                        # 批評空間 (届かなければ VNDB) からブランド・発売日・点数・タグ・紹介・カバーを取得
bin/vnbox meta -all                                 # meta.json の無い全パッケージに対して取得
bin/vnbox meta-search 姫狩り                         # 候補 ID の一覧 → bin/vnbox meta mygame -id egs:12345 / -id vndb:v1195 で指定
bin/vnbox meta mygame -title T -engine E -thumb 画像 # ギャラリー表示を手で直す
bin/vnbox serve -addr :8080 -token <秘密の文字列>     # ポータル: ギャラリーから「遊ぶ」で起動・配信、「終了」でセーブを上げて停止
bin/vnbox play mygame -addr :8080 -token <...>      # 1 本だけ起動して配信 (ゲームを閉じるか Ctrl-C で終了、終了時にセーブ)
bin/vnbox store list | history <name> | push-package <name> | pull-package <name> | save-push <name> | save-pull <name> [時刻]
bin/vnbox capture begin <name> / capture end <name> # 手動インストールの差分取り
bin/vnbox detect <フォルダ> / extract <アーカイブ> <出力先> / regdiff diff|apply|show
```

| パッケージ | 役割 |
|---|---|
| `internal/wine` | Wine の選択 (vnbox / classic)、環境変数、Xvfb・PulseAudio、prefix のハードリンク複製 |
| `internal/regdiff` | Wine の .reg を直接 diff / merge |
| `internal/detect` | エンジン・インストーラ・依存の判定、起動 exe の推定、PE アーキテクチャ |
| `internal/extract` | zip (CP932 名を UTF-8 へ、zip slip 防止) / lzh / 7z / rar / SFX の展開 |
| `internal/install` | インストーラ判定 (MSI / Inno / NSIS / InstallShield / 名前不定のインストーラ)、無人インストール、ダイアログ自動操作、WinRAR SFX を Wine で展開、InstallShield setup.exe の埋め込み MSI を Wine を使わずに取り出す (`isx.go`) |
| `internal/capture` | インストール前後の差分をパッケージ化 |
| `internal/add` | 配布物 1 つ → パッケージ |
| `internal/pkg` | パッケージの読み書き、実行用 prefix の準備 |
| `internal/store` | S3 (AWS SDK v2): パッケージとセーブの保存・取得・履歴 |
| `internal/stream` | 配信: WebRTC (H.264 / VP8 + Opus、データチャネル入力、pion/webrtc) と代替の MJPEG / Opus-WebM、タッチ操作のページ |
| `internal/session` | 1 本のゲームの起動〜終了 (1x1 ウィンドウ時の Managed=N 再起動、定期・終了時のセーブ) |
| `internal/meta` | 批評空間 (SQL フォーム) / VNDB (Kana API) からのメタデータ取得、タイトルの照合 (「体験版」等を除いて正規化)、カバー画像の保存 |
| `internal/portal` | ギャラリー・詳細画面、サムネイル (プレイ中はライブ)、トークン認証 (一度 `?token=` で開くと Cookie)、複数セッションの管理 |

- **認証**: `-token` (または `VNBOX_TOKEN`) を付けると、トークンを知らない人は一覧も配信も操作もできない。トークン無しで起動すると警告を出す。
- **テスト**: `cd go && go test ./...` (S3 部分は S3 互換のテストサーバ moto を自動で起動して確認。無ければスキップ)。
- 動作確認: zip 版・WinRAR 自己解凍版・Inno Setup 版・LiberalInstaller 版を `vnbox add` で追加して起動、`vnbox play` で S3 からの取得 → 起動 → 認証 → 映像 54 フレーム/4 秒・音声・入力 → 終了時のセーブ保存、`vnbox serve` でポータルからの起動・停止を確認。

## ギャラリーとメタデータ

`vnbox serve` のトップはゲームのギャラリー。カードにはサムネイル・タイトル・ブランド・発売年・点数・エンジン・セーブの有無が出る。

- **サムネイル**: プレイ中のゲームは 3 秒ごとに実際の画面 (ライブ)。プレイしたことがあれば、起動 45 秒後・定期セーブ時・「終了」時の画面を `packages/<name>/thumb.jpg` に残すので「最後に見た画面」になる。まだ遊んでいなければカバー画像、それも無ければ名前から作った仮画像。
- **絞り込み・並び替え**: タイトル / ブランド / タグの文字検索 (`/` でフォーカス)、「プレイ中」「セーブあり」、タイトル順・発売日順・点数順・ブランド順 (選択はブラウザに記憶)。
- **詳細画面** (`/game/<name>`): カバー、ブランド・発売日・点数・票数・シリーズ、紹介文、タグ、批評空間 / VNDB / 公式 / DMM / DLsite へのリンク、最後に見た画面、遊ぶ/続ける/終了。
- **メタデータ** (`vnbox meta <name> -fetch`、`vnbox meta -all`):
  - 第一候補は **批評空間** (ErogameScape)。公開 SQL フォームに `gamelist` / `brandlist` / `povgroups_toukei` を問い合わせ、ブランド・発売日・中央値・データ数・POV タグ (ジャンル/背景/傾向)・シリーズ・DMM 品番 (カバーは DMM のパッケージ画像)。日本語のキーワードは `convert_from(decode('<hex>','hex'),'UTF8')` で送り、問い合わせは 2.5 秒以上空ける。問い合わせ内容は [minuspiral/playnite-with-erogamescape](https://github.com/minuspiral/playnite-with-erogamescape) (MIT) を参考にした。
  - 批評空間に繋がらないときは **VNDB** (`api.vndb.org/kana`) に自動で切り替える (1 回失敗したらその実行中は批評空間を待たない)。点数は VNDB 評価 (10〜100)、紹介文は英語。カバーは性的表現の評価が 2 (露骨) 未満のものだけ使う。
  - 照合: 配布物のタイトルから「体験版 / お試し版 / Web体験版 / Trial / Ver.x」等を除いて部分一致検索し、全角半角・記号を無視して一致するものを優先 (同点なら票数の多いもの)。「～」の表記ゆれで見つからないときは記号で区切った最長部分で再検索する。外れたときは `vnbox meta-search` で ID を調べて `-id egs:<ID>` / `-id vndb:v<ID>` で固定する。
  - 保存先は `packages/<name>/meta.json` と `cover.jpg` (幅 600px)。S3 にはパッケージ本体とは別に `packages/<name>.json` / `.cover.jpg` / `.jpg` (サムネイル) も置くので、S3 にしか無いゲームもギャラリーに絵と情報付きで並ぶ (サーバ側は `runtime/gallery/` にキャッシュ)。
  - この開発環境 (海外のクラウド) からは批評空間に接続できなかった (TLS 接続がリセットされる) ため、実データでの確認は VNDB 側のみ。批評空間の部分は応答の HTML を模したテストサーバで確認している。
- `GET /api/games` で同じ一覧を JSON でも取れる。

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
wine-build/              自前 Wine のパッチ (third_party/ は fitudao 氏のパッチ、LGPL-2.1)
go/                      Go 実装 (上記)
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

| `wine-build/vnbox-qasf-dmo-no-output-loop.patch` | qasf: DMO ラッパーが、出力なし (S_FALSE) なのに INCOMPLETE を立てる WMA デコーダで無限ループし、動画が始まらない | よめがみ (Alcot, 吉里吉里 WMV オープニング): 黒画面のまま → タイトルへ |
| `wine-build/vnbox-wined3d-read-scanline.patch` | wined3d: `D3DCAPS9.Caps` が 0 だった。実機のドライバは必ず `D3DCAPS_READ_SCANLINE` を返す (GetRasterStatus は wined3d がエミュレートしている) | Hello,good-bye (Lump of Sugar 2010, BGI): 「ディスプレイアダプタが必要不可欠な機能をサポートしていません」→ インストーラもゲームも動作 |
| `wine-build/vnbox-kernelbase-safe-disk-free.patch` | kernelbase: 空き容量を 32bit で掛け算する (または 64bit 値を切り捨てる) 古いインストーラが、空きが 4GB の倍数を少し超えると「容量不足」になる。実際の空き以下で、下位 32bit が 2GB-1MB になる値を `GetDiskFreeSpace(Ex)` が返す (64bit で正しく扱うアプリは最大 4GB 少なく見えるだけ) | よろず屋 (ShiinaRio 系インストーラ)、百千の定にかわたれし剋 (InstallShield の前段) |
| `wine-build/vnbox-wmreader-allocator-retry.patch` | winegstreamer: WM Reader が、アプリ側アロケータが未コミット (VFW_E_NOT_COMMITTED) のとき即座にストリーム終端扱いにしていた。少し待って再試行する | (同上の調査で見つけた不具合。単独で直ったタイトルは未確認) |
| `wine-build/vnbox-quartz-videowindow-unlocked.patch` | quartz: フィルタグラフの `IVideoWindow` 各メソッドが、グラフのロックを持ったままレンダラのウィンドウを表示・非表示・移動していた。動画スレッドが `put_Visible` で UI スレッドへメッセージを送り、UI スレッドが `NotifyOwnerMessage` で同じロックを待つとデッドロックする。ロックはレンダラを探す間だけにした | スズノネセブン！ (Clochette 2008, CatSystem2): OP 動画のあと黒画面のまま → タイトルへ |
| `wine-build/vnbox-win32u-own-frame-visible.patch` | win32u: タイトルバーを自分で描くウィンドウ (WM_NCCALCSIZE でクライアント領域を枠いっぱいに広げる Chromium / NW.js / Electron) でも、Wine は WM が枠を描く前提で上端のタイトルバー分を X ウィンドウから外していた (vnbox の WM は枠を描かない)。クライアント領域が想定の枠にかかるときはウィンドウ全体を見せる | RPG ツクール MZ (NW.js) 製の体験版多数: 上 30px が欠けて X 側 1280x720 / Wine 側 1280x750 → 全体が見える |

後から足したパッチ (kernelbase / ntdll / qasf / winegstreamer / wined3d) は、該当 DLL だけを再ビルドして `/opt/wine-vnbox` に差し替えた (元のファイルは `wine-build/orig-dlls/`)。Wine を作り直すときは `wine-build/` のパッチを全部当てて `--enable-archs=i386,x86_64` でビルドする。

## 第2弾: suezou ミラーの体験版を年代別に一括テスト (2006〜2026)

`scripts/tools/suezou_crawl.py` でミラー全 195 ページから 4,009 件のリンクを集め、年度ごとにブランドが重ならないよう抽出。
`scripts/tools/batch.sh` が DL → 展開 / 無人インストール → エンジン判定 → 起動 → スクショ を自動で回す (vnbox Wine 使用)。

第2弾 (51 本) と第3弾 (67 本、2 並列) の結果は `results/batch/summary.tsv`、各タイトルの経過スクショは `results/batch/<id>.png`。
判定は `scripts/tools/verdict.sh` (プロセス生存 / 最終画面が十分大きい / 画面中央にコントラスト / クラッシュ痕跡なし) で一次判定し、**全件スクショを目視**して確定した。

## 動作確認の集計

**タイトル画面以降まで目視で確認できたもの: 270 本** (一覧は下の「起動確認済みタイトル一覧」)。
- 第1〜3弾: 102 本 (第1弾 22 本 + suezou ミラー 80 本)
- 第4弾 (2026-10-19): suezou ミラーから新たに 108 本 (`downloads/suezou/picked4b.tsv`、3 並列バッチ)
- 以前不可だったムジナ (2024) が Wine パッチ 2 本で動作: 1 本
- 未解決分の解消 (2026-10-19): 黒画面・インストーラ残り・展開失敗だったものを Go 版 `vnbox add` の改良と Wine パッチで再試行し 59 本 (これで「弱:」扱いは 0 本)

配布物に実行ファイルが無いもの (紹介動画のみ) と配布ファイル自体が壊れているものは `results/excluded.tsv` に理由付きで除外 (11 本)。
第4弾は「同じ年の同じブランドは 1 本・既に試した作品は除く」で選んだ (ブランドの重複なしでは候補が尽きたため)。

エンジン別の内訳 (第4弾時点の 211 本。その後の 59 本は `results/verified.tsv` の engine 列を参照):

| エンジン (検出名) | 本数 |
|---|---|
| 吉里吉里 / KiriKiri2 | 56 |
| Softpal ADV | 17 |
| YU-RIS | 10 |
| SiglusEngine / RealLive | 7 |
| EntisGLS / NeXAS / QLIE / BGI / CatSystem2 | 21 |
| AdvWin32/WillPlus / NScripter / AdvHD / Unity / Majiro / Escude / AliceSoft System4 / Kaguya / PONScripter / Artemis / Musica / TyranoScript / Ren'Py / SakanaGL / ShiinaRio | 29 |
| 判定外 (独自エンジン等) | 71 |

未解決: なし (2026-10-19 時点。除外 11 本は上記の理由で対象外)。最後まで残った 2 本の原因:
- **Hello,good-bye (lumpofsugar 2010, BGI)**: BGI が D3D9 の `Caps & D3DCAPS_READ_SCANLINE` を必須にしており、Wine は 0 を返していた → wined3d パッチ。インストーラのメニュー「インストール」は `-click 157,408` (`recipes/add_hints.tsv`)。
- **姫狩りダンジョンマイスター (eushully 2009)**: InstallShield 12 のブートストラッパが Wine 上で %TEMP% への MSI 展開を途中 (209/243MB) で止め、壊れた MSI で失敗していた → setup.exe の末尾に難読化して埋め込まれた MSI を直接取り出す (`internal/install/isx.go`、Python 版 `scripts/tools/isx.py`) と `msiexec /qn` で入る。

## 起動確認済みタイトル一覧

<!-- TITLES:BEGIN -->
タイトル画面以降まで目視で確認: **270 本** (ロゴ・注意画面止まりの 0 本は下の折りたたみに別記)。
年 = 配布ミラーの掲載年度 / DLsite の登録年 (「-」は第1弾の手動テスト分)。エンジンの「-」は自動判定できなかったもの。

| # | 年 | 出典 | タイトル | エンジン | 確認できたところ | ID |
|---|---|---|---|---|---|---|
| 1 | 2006 | suezou | その横顔を見つめてしまう ～a profile完全版～ web体験版 | KiriKiri | タイトル | `b2006_akabeesoft` |
| 2 | 2006 | suezou | b2006_alcot | - | タイトル (再試行・パッチ済み Wine) | `b2006_alcot` |
| 3 | 2006 | suezou | EXTRAVAGANZA～蟲愛でる少女～体験版 | - | タイトル | `b2006_blackcyc` |
| 4 | 2006 | suezou | 『オイラは番台２』体験版 | - | タイトル | `b2006_interheart` |
| 5 | 2006 | suezou | 淫皇覇伝アマツ（体験版） | Kaguya | タイトル | `b2006_kaguya` |
| 6 | 2006 | suezou | とらぶるっ！ | - | タイトル | `b2006_lilim` |
| 7 | 2006 | suezou | いつか、届く、あの空に。【体験版】 | BGI/Ethornell | タイトル→環境設定 | `b2006_lumpofsugar` |
| 8 | 2006 | suezou | ソウルイーター　体験版　Game Edition | - | ゲーム開始 (名前入力) | `b2006_lune` |
| 9 | 2006 | suezou | 人妻搾乳飯店－体験版－ | - | タイトル | `b2006_marine` |
| 10 | 2006 | suezou | ソウルレイザー☆ユナ 体験版 | - | タイトル | `b2006_mille_feuille` |
| 11 | 2006 | suezou | 機械仕掛けのイブ　～Ｄｅａ　Ｅｘ　Ｍａｃｈｉｎａ～ | KiriKiri | タイトル | `b2006_ninetail` |
| 12 | 2006 | suezou | b2006_pulltop | - | タイトル (Go 版: クリック操作で LiberalInstaller を通過) | `b2006_pulltop` |
| 13 | 2006 | suezou | ななついろ★ドロップス-体験版 | - | 本編 | `b2006_unisonshift` |
| 14 | 2006 | suezou | いな☆こい！ ～お稲荷さまとモテモテのたたり～ 体験版 ver 1.00 | YU-RIS | タイトル | `b2006_whirlpool` |
| 15 | 2007 | suezou | b2007_akabeesoft | - | タイトル (Go 版: Inno Setup を無人インストール) | `b2007_akabeesoft` |
| 16 | 2007 | suezou | 必殺痴漢人　- TRIAL VERSION - | - | タイトル→メニュー | `b2007_anim` |
| 17 | 2007 | suezou | 姫奴隷 | - | タイトル | `b2007_bishop` |
| 18 | 2007 | suezou | かみぱに！体験版 | QLIE | タイトル | `b2007_clochette` |
| 19 | 2007 | suezou | あかね色に染まる坂 | - | タイトル | `b2007_feng` |
| 20 | 2007 | suezou | ハーレム×すくらっち体験版 | RealLive | タイトル | `b2007_hadashi` |
| 21 | 2007 | suezou | でるた！（体験版） | Kaguya | タイトル | `b2007_kaguya` |
| 22 | 2007 | suezou | 看護戦隊ナースレンジャー | - | タイトル | `b2007_lilim` |
| 23 | 2007 | suezou | b2007_lilim_darkness | - | タイトル (再試行・パッチ済み Wine) | `b2007_lilim_darkness` |
| 24 | 2007 | suezou | 風輪奸山　体験版 | - | タイトル | `b2007_lune` |
| 25 | 2007 | suezou | しおふきマーメイド体験版 | - | ゲーム開始 (名前入力) | `b2007_marine` |
| 26 | 2007 | suezou | リリカル♪りりっく体験版 | EntisGLS | コンフィグ画面 | `b2007_marmalade` |
| 27 | 2007 | suezou | 『Bullet Butlers』Web体験版　ver1.1 | - | 本編 | `b2007_propeller` |
| 28 | 2007 | suezou | 聖炎天使エレアノール 体験版 | RealLive | タイトル | `b2007_sagaplanets` |
| 29 | 2007 | suezou | b2007_whirlpool | YU-RIS | タイトル (最終画面は暗転) | `b2007_whirlpool` |
| 30 | 2008 | suezou | ＷＬＯ-世界恋愛機構- web用体験版 | KiriKiri | タイトル | `b2008_akabeesoft` |
| 31 | 2008 | suezou | 息子の友達に犯されて  - TRIAL VERSION - | - | タイトル | `b2008_anim` |
| 32 | 2008 | suezou | Hime のち Honey - 体験版 | KiriKiri | タイトル | `b2008_asa_project` |
| 33 | 2008 | suezou | b2008_bishop | - | タイトル (ランチャー startup.exe から起動) | `b2008_bishop` |
| 34 | 2008 | suezou | b2008_blackcyc | - | タイトル (再試行・パッチ済み Wine) | `b2008_blackcyc` |
| 35 | 2008 | suezou | Venus Blood Chimera | KiriKiri | タイトル | `b2008_dualtail` |
| 36 | 2008 | suezou | ヴェルディア幻奏曲体験版 | Escude | 本編 | `b2008_escude` |
| 37 | 2008 | suezou | お姉さん×すくらっち体験版 | RealLive | タイトル | `b2008_hadashi` |
| 38 | 2008 | suezou | FairlyLife WEB体験版02 H_Only Ver1.00 | - | コンフィグ画面 | `b2008_hook` |
| 39 | 2008 | suezou | いたずら極―リアル―　体験版 | FVP | タイトル (再試行・Go 版でパッケージ化) | `b2008_interheart` |
| 40 | 2008 | suezou | 夏神（体験版０２） | - | タイトル | `b2008_kaguya` |
| 41 | 2008 | suezou | びんかんアスリート | - | タイトル | `b2008_marine` |
| 42 | 2008 | suezou | 魔法少女沙枝 Vol.2 体験版 | KiriKiri | タイトル (MSI) | `b2008_mille_feuille` |
| 43 | 2008 | suezou | 晴れハレはーれむ 体験版Ver1.00 | - | タイトル | `b2008_smee` |
| 44 | 2009 | suezou | 愛娘という名の玩具　- TRIAL VERSION - | - | タイトル | `b2009_anim` |
| 45 | 2009 | suezou | 美乳淫妹 遥「私にイヤらしいコスプレさせるなんて……この変態！」 | Softpal | タイトル | `b2009_crossover` |
| 46 | 2009 | suezou | 夏ノ雨 -体験版- | EntisGLS | タイトル | `b2009_cube` |
| 47 | 2009 | suezou | 魔法少女ラヴィリオン 体験版 | KiriKiri | タイトル | `b2009_dualtail` |
| 48 | 2009 | suezou | 姫狩りダンジョンマイスター 体験版 | AGE (Eushully) | InstallShield ペイロードを isx 展開 → MSI /qn | `b2009_eushully` |
| 49 | 2009 | suezou | さらさらささら（体験版） | - | タイトル | `b2009_kaguya` |
| 50 | 2009 | suezou | あい☆きゃん | - | タイトル | `b2009_lilim` |
| 51 | 2009 | suezou | キスと魔王と紅茶（体験版） | EntisGLS | タイトル | `b2009_marmalade` |
| 52 | 2009 | suezou | シグナルハート 体験版 Ver.01 | QLIE | 本編 | `b2009_purplesoftware` |
| 53 | 2009 | suezou | らぶでれーしょん！ Ver1.00 | FVP | タイトル (起動時の Save フォルダ作成確認を OK) | `b2009_smee` |
| 54 | 2010 | suezou | 置き場がない！ web体験版 | KiriKiri | タイトル | `b2010_akabeesoft` |
| 55 | 2010 | suezou | 贄姫 - TRIAL VERSION - | - | タイトル | `b2010_anim` |
| 56 | 2010 | suezou | 学園３～華麗なる悦辱～ 体験版 | - | タイトル | `b2010_bishop` |
| 57 | 2010 | suezou | JINKI EXTEND Re:VISION -SECOND IMPACT- | NeXAS | タイトル | `b2010_giga` |
| 58 | 2010 | suezou | Reunion～３日間だけのプチ同棲～　体験版 | KiriKiri | タイトル | `b2010_grand_cru` |
| 59 | 2010 | suezou | キラリ☆南国小麦色　体験版 | - | タイトル | `b2010_kaguya` |
| 60 | 2010 | suezou | Hello,good-bye 体験版 | BGI/Ethornell | wined3d READ_SCANLINE パッチ + BGI インストーラ -click 157,408 | `b2010_lumpofsugar` |
| 61 | 2010 | suezou | b2010_marine | Softpal | タイトル (起動設定ファイル警告を OK で通過) | `b2010_marine` |
| 62 | 2010 | suezou | 痴覚過敏 体験版 | EntisGLS | タイトル | `b2010_undermoon` |
| 63 | 2010 | suezou | b2010_waffle | - | タイトル (インストーラが Win10 非対応 → 従来 Wine (Win7) でインストール) | `b2010_waffle` |
| 64 | 2010 | suezou | 涼風のメルト -Where wishes are drawn to each other- WEB体験版 | YU-RIS | タイトル | `b2010_whirlpool` |
| 65 | 2011 | suezou | NTR彼女はいじめられっ娘 - TRIAL VERSION - | - | タイトル | `b2011_anim_team_mm` |
| 66 | 2011 | suezou | 裏教師～背徳の淫悦授業～ 体験版 | - | タイトル | `b2011_bishop` |
| 67 | 2011 | suezou | b2011_blackcyc | - | タイトル (Go 版 vnbox add/serve で WinRAR SFX から) | `b2011_blackcyc` |
| 68 | 2011 | suezou | シオンの血族 | SiglusEngine | タイトル | `b2011_catwalk` |
| 69 | 2011 | suezou | Starless 体験版 | - | タイトル (独自インストーラ) | `b2011_empress` |
| 70 | 2011 | suezou | りとる†びっち ～Little Bitch Girls in Hog Farm～ 体験版 | Softpal | タイトル | `b2011_erectlip` |
| 71 | 2011 | suezou | ホチキス - 体験版 | NeXAS | タイトル | `b2011_giga` |
| 72 | 2011 | suezou | はっぴぃふぁくとりー　体験版 | KiriKiri | タイトル | `b2011_grand_cru` |
| 73 | 2011 | suezou | 『種憑け村』序章体験版・導ノ儀 | ShiinaRio | タイトル | `b2011_guilty` |
| 74 | 2011 | suezou | StrawberryNauts Ver1.00 | - | タイトル | `b2011_hook` |
| 75 | 2011 | suezou | アルバイトの先輩の女のコに仕事を教わっていたら始まっちゃうＨな関係。　体験版 | - | タイトル | `b2011_kaguya` |
| 76 | 2011 | suezou | b2011_lumpofsugar | BGI/Ethornell | タイトル (BGI インストーラ: メニューをクリック) | `b2011_lumpofsugar` |
| 77 | 2011 | suezou | b2011_marine | Softpal | タイトル (再試行・パッチ済み Wine) | `b2011_marine` |
| 78 | 2011 | suezou | ユユカナ 体験版 | KiriKiri | タイトル (従来 32bit Wine。新 WoW64 だと kernel32 認証エラー) | `b2011_nanawind` |
| 79 | 2011 | suezou | 姫様限定！～PrincessLimited～web体験版ver1.0 | - | タイトル | `b2011_princess_sugar` |
| 80 | 2011 | suezou | b2011_pulltop | - | タイトル (再試行・パッチ済み Wine) | `b2011_pulltop` |
| 81 | 2011 | suezou | ラブラブル体験版 Ver1.00 | - | タイトル | `b2011_smee` |
| 82 | 2012 | suezou | JOKER-死線の果ての道化師- | KiriKiri | タイトル | `b2012_akabeesoft3` |
| 83 | 2012 | suezou | 中の人などいない！　体験版 | KiriKiri2 | タイトル (quartz パッチ) | `b2012_alcot` |
| 84 | 2012 | suezou | 母娘乱館 体験版 | AliceSoft System4 | 本編テキスト | `b2012_alicesoft` |
| 85 | 2012 | suezou | 学園ＮＴＲ -僕の知らない彼女の淫顔- 体験版 | - | タイトル | `b2012_atelier_gh` |
| 86 | 2012 | suezou | b2012_bishop | - | タイトル (ランチャー startup.exe から起動) | `b2012_bishop` |
| 87 | 2012 | suezou | 美脚隷嬢優奈「脚もお尻もこんなに穢すなんて……絶対に許さない！」体験版 | KiriKiri | タイトル | `b2012_cross_over` |
| 88 | 2012 | suezou | b2012_erectlip | - | タイトル (再試行・Go 版でパッケージ化) | `b2012_erectlip` |
| 89 | 2012 | suezou | 星空へ架かる橋ＡＡ 円佳SPアフター体験版 | - | タイトル | `b2012_feng` |
| 90 | 2012 | suezou | つぼい君のスイッチ！体験版 | - | 本編 | `b2012_fudegaki_soft` |
| 91 | 2012 | suezou | キスベル　体験版 | NeXAS | タイトル | `b2012_giga` |
| 92 | 2012 | suezou | リヴォルバーガール☆ハンマーレディ　体験版 | KiriKiri | タイトル | `b2012_kai` |
| 93 | 2012 | suezou | 恋妹SWEET☆DAYS 体験版 | KiriKiri | タイトル | `b2012_parasol` |
| 94 | 2012 | suezou | 籠女の繭 体験版 | - | タイトル | `b2012_tinkerbell` |
| 95 | 2012 | suezou | MonsterParkⅡ | - | タイトル | `b2012_trois` |
| 96 | 2012 | suezou | b2012_waffle | - | タイトル (解像度選択→はい をクリック) | `b2012_waffle` |
| 97 | 2013 | suezou | ランス01体験版 | AliceSoft System4 | タイトル | `b2013_alicesoft` |
| 98 | 2013 | suezou | 息子の友達に堕とされて　体験版 | YU-RIS | タイトル | `b2013_anim` |
| 99 | 2013 | suezou | 花嫁狩凌 ～絶対快楽の白濁淫毒～ 体験版 | KiriKiri | タイトル | `b2013_cross_over` |
| 100 | 2013 | suezou | VenusBlood-GAIA- WEB専用ADV体験版 | KiriKiri | タイトル | `b2013_dualtail` |
| 101 | 2013 | suezou | お嬢様はご機嫌ナナメ 体験版 | AdvHD | タイトル (InstallShield) | `b2013_ensemble` |
| 102 | 2013 | suezou | キスアト体験版 | NeXAS | タイトル | `b2013_giga` |
| 103 | 2013 | suezou | PriministAr（体験版） Ver1.00 | - | タイトル | `b2013_hooksoft` |
| 104 | 2013 | suezou | 妹のおかげでモテすぎてヤバい。 体験版 | KiriKiri | タイトル | `b2013_hulotte` |
| 105 | 2013 | suezou | b2013_inter_heart | - | 本編 (Go 版: 自己解凍を C: に置いて展開) | `b2013_inter_heart` |
| 106 | 2013 | suezou | 肉牝Ｒ３０（体験版） | - | タイトル | `b2013_kaguya` |
| 107 | 2013 | suezou | b2013_lumpofsugar | BGI/Ethornell | タイトル (BGI インストーラ: メニューをクリック) | `b2013_lumpofsugar` |
| 108 | 2013 | suezou | ナマイキデレーション体験版 WEB Ver. | KiriKiri | タイトル | `b2013_madosoft` |
| 109 | 2013 | suezou | ぜったいマジラブ！あまイチャはぷにんぐ！体験版 Ver 1.0 | - | タイトル | `b2013_tinkerbell` |
| 110 | 2014 | suezou | 体験版 甘えてくれる恋人とのエッチなひととき編 | CatSystem2 | タイトル | `b2014_azarashi_soft` |
| 111 | 2014 | suezou | b2014_ensemble | - | タイトル (再試行・パッチ済み Wine) | `b2014_ensemble` |
| 112 | 2014 | suezou | b2014_ensemble_sweet | - | タイトル (再試行・Go 版でパッケージ化) | `b2014_ensemble_sweet` |
| 113 | 2014 | suezou | Re;Lord ～ヘルフォルトの魔女とぬいぐるみ～ - 体験版 Ver. | Escude | タイトル | `b2014_escude` |
| 114 | 2014 | suezou | b2014_guilty | - | タイトル (再試行・パッチ済み Wine) | `b2014_guilty` |
| 115 | 2014 | suezou | ＰＲＥＴＴＹ×Ｃ∧ＴＩＯＮ | KiriKiri | タイトル (終了確認ダイアログまで操作) | `b2014_hibiki_works` |
| 116 | 2014 | suezou | しゃぶらレンタル　体験版 | - | タイトル | `b2014_kaguya` |
| 117 | 2014 | suezou | b2014_pulltop_latte | - | タイトル (再試行・パッチ済み Wine) | `b2014_pulltop_latte` |
| 118 | 2014 | suezou | b2014_silkys_plus | - | タイトル (メニュー start.exe ではなくゲーム本体を起動) | `b2014_silkys_plus` |
| 119 | 2014 | suezou | 奪われた学園～罠に落ちた優等生～　体験版 | KiriKiri | タイトル | `b2014_undermoon` |
| 120 | 2014 | suezou | b2014_waffle | - | メニュー (解像度選択→確認をクリックで通過) | `b2014_waffle` |
| 121 | 2014 | suezou | こすぷれえっち 体験版 | - | タイトル (MS 別名フォント) | `b2014_wendybell` |
| 122 | 2014 | suezou | G.I.B. ガールズ・イン・ブラック  WEB体験版 | YU-RIS | タイトル | `b2014_whirlpool` |
| 123 | 2014 | suezou | 春風センセーション！体験版 | CatSystem2 | タイトル (再試行: 待機延長) | `b2014_windmill_oasis` |
| 124 | 2015 | suezou | お兄ちゃん、教えて……～義妹とヒミツの保健実習～　体験版　Ver1.00 | KiriKiri | タイトル | `b2015_ammolite` |
| 125 | 2015 | suezou | 愛娘は、マゾ性隷～娘だからって遠慮しないでね？～　体験版　Ver1.00 | KiriKiri | タイトル | `b2015_appetite` |
| 126 | 2015 | suezou | ClosedGAME体験版 | Majiro | タイトル | `b2015_empress` |
| 127 | 2015 | suezou | b2015_ensemble | - | タイトル (再試行・パッチ済み Wine) | `b2015_ensemble` |
| 128 | 2015 | suezou | 巨乳大魔王のドスケベクエスト ～完全敗北した少年勇者クンＵＣ～　【体験版】 | Softpal | タイトル | `b2015_erectlip` |
| 129 | 2015 | suezou | 淫らに喘ぐ七人の夜這いする孕女　体験版 | AdvHD | タイトル | `b2015_guilty` |
| 130 | 2015 | suezou | ろーらいず!!! 体験版 | SiglusEngine | タイトル (TZ=Asia/Tokyo) | `b2015_hamham` |
| 131 | 2015 | suezou | トラベリングスターズ 体験版02 Ver1.00 | BGI/Ethornell | タイトル (ログに例外あり) | `b2015_hooksoft` |
| 132 | 2015 | suezou | ハラミタマ　体験版 | - | タイトル | `b2015_kaguya` |
| 133 | 2015 | suezou | b2015_lump_of_sugar | BGI/Ethornell | タイトル (BGI インストーラ: メニューをクリック) | `b2015_lump_of_sugar` |
| 134 | 2015 | suezou | 極煌戦姫ミストルティア 体験版 | KiriKiri | タイトル | `b2015_mille_feuille` |
| 135 | 2015 | suezou | b2015_minato_carnival | - | タイトル (QLIE 起動設定の「起動」をクリック) | `b2015_minato_carnival` |
| 136 | 2015 | suezou | ALIA’s CARNIVAL! Flowering Sky　体験版(WEB.ver) | KiriKiri | タイトル (以前の dead は自動操作で終了を押したため) | `b2015_nanawind` |
| 137 | 2015 | suezou | おしおき生意気ギャル ～エロいオシオキをしたら自分からエッチを求めてきた～ 体験版 | KiriKiri | タイトル | `b2015_pachipachi_soft` |
| 138 | 2016 | suezou | よめがみ My Sweet Goddess! リコＨシーン体験版 | KiriKiri | タイトル (Wine qasf パッチ: WMV オープニングの無限ループ修正) | `b2016_alcot` |
| 139 | 2016 | suezou | 満淫オーク電車～くっ！　任務じゃなければこんな場所…～　体験版　Ver1.00 | KiriKiri | タイトル | `b2016_apadash` |
| 140 | 2016 | suezou | アナル＊ティーチャー～感度良肛！？　逆レッスン～　体験版　Ver1.00 | KiriKiri | タイトル | `b2016_appetite` |
| 141 | 2016 | suezou | b2016_blackcyc | - | タイトル (待機延長) | `b2016_blackcyc` |
| 142 | 2016 | suezou | 間宮くんちの五つ子事情 体験版 | KiriKiri | タイトル | `b2016_cube` |
| 143 | 2016 | suezou | お嬢様はイイナリみたいです！？ 体験版 | - | タイトル | `b2016_evoll` |
| 144 | 2016 | suezou | リプキス　体験版 | NeXAS | タイトル | `b2016_giga` |
| 145 | 2016 | suezou | はらかつ！ ～気になるあの子と子作りエッチ～ Trial Version: 1.0.0.0 [data ver:1.00] | - | 本編 | `b2016_inter_heart_glossy` |
| 146 | 2016 | suezou | 小悪魔発育チュ～！（体験版） | FVP | タイトル (再試行・Go 版でパッケージ化) | `b2016_kaguya` |
| 147 | 2016 | suezou | b2016_silkys_sakura | - | タイトル (メニュー start.exe ではなくゲーム本体を起動) | `b2016_silkys_sakura` |
| 148 | 2016 | suezou | b2016_waffle | - | タイトル (解像度選択→はい をクリック) | `b2016_waffle` |
| 149 | 2017 | suezou | いつまでも俺は母に恋してる！  - 体験版２ - | - | タイトル | `b2017_anim` |
| 150 | 2017 | suezou | 催眠アプリで寝取りハメ～これまでの恨み、ヤり返してやる！　体験版　Ver1.00 | KiriKiri | タイトル | `b2017_appetite` |
| 151 | 2017 | suezou | なちゅらるばけーしょん | - | タイトル (再試行) | `b2017_hibiki_works` |
| 152 | 2017 | suezou | はらかつ２　～子作りエッチでお悩み解決～ Trial Version: 1.0.0.0 [data ver:1.00] | - | 本編テキスト | `b2017_inter_heart_glossy` |
| 153 | 2017 | suezou | ニュートンと林檎の樹　体験版＋ | CatSystem2 | タイトル (Go 版: CatSystem2 インストーラを自動操作) | `b2017_laplacian` |
| 154 | 2017 | suezou | 女装千年王国　体験版 | YU-RIS | タイトル | `b2017_no_strike` |
| 155 | 2017 | suezou | b2017_onomatope_raspberry | ShiinaRio | タイトル (Go 版: ShiinaRio の SETUP.exe を自動インストール) | `b2017_onomatope_raspberry` |
| 156 | 2017 | suezou | ことのは・アムリラート | KiriKiri | タイトル (OP動画再生後) | `b2017_sukerasparo` |
| 157 | 2017 | suezou | 透けちゃって！！～九死に一生を得て濃密セカンドライフ～ Trial Version: 1.0.0.0 [data ver:1.00] | - | 本編テキスト | `b2017_sweet_heart` |
| 158 | 2017 | suezou | あなたに恋する恋愛ルセット　【体験版】 | Softpal | タイトル | `b2017_unisonshift` |
| 159 | 2017 | suezou | b2017_waffle | - | タイトル (解像度選択→確認をクリックで通過) | `b2017_waffle` |
| 160 | 2018 | suezou | わんこの嫁入り ～ようこそ！いぬのしっぽへ～ 体験版 version 1.00 | - | タイトル | `b2018_alumi_soft` |
| 161 | 2018 | suezou | P.S.あなたを心から愛しています～最期まで気づけなかった妻の苦しみ～　体験版 | KiriKiri | タイトル | `b2018_appetite` |
| 162 | 2018 | suezou | 彼女が異種族だった場合～クールな先輩ダークエルフとの甘やかし同棲日誌～体験版 | YU-RIS | タイトル | `b2018_casket` |
| 163 | 2018 | suezou | 恋はそっと咲く花のように　体験版 | FVP | タイトル (再試行・Go 版でパッケージ化) | `b2018_ensemble` |
| 164 | 2018 | suezou | 爆乳催眠女教師　【体験版】 | Softpal | タイトル | `b2018_erectlip` |
| 165 | 2018 | suezou | 催眠学園１年生体験版 | - | タイトル | `b2018_fudegaki_soft` |
| 166 | 2018 | suezou | 俺の上であがく六人の伽女　体験版２ | AdvWin32/WillPlus | タイトル | `b2018_guilty` |
| 167 | 2018 | suezou | ノットフィクション！ Trial Version: 1.0.0.0 [data ver:1.00] | - | タイトル | `b2018_hachimitsu_soft` |
| 168 | 2018 | suezou | LoveHolic　体験版 | - | タイトル | `b2018_kaguya` |
| 169 | 2018 | suezou | b2018_mirai | - | タイトル (QLIE 起動設定の「起動」をクリック) | `b2018_mirai` |
| 170 | 2018 | suezou | グランギニョルの夜 体験版Ver.01 | - | 本編テキスト (待機延長) | `b2018_tinkerbell` |
| 171 | 2018 | suezou | 僕と彼女のご奉仕同棲　体験版 | AdvWin32/WillPlus | タイトル | `b2018_tokohana` |
| 172 | 2019 | suezou | いつまでも息子のままじゃいられない！５ - 体験版 - | - | タイトル | `b2019_anim` |
| 173 | 2019 | suezou | 女体化パニック！３～お、俺は男だ！　でも、悔しいけど気持ちいいっ……のか！？　体験版 | KiriKiri | タイトル | `b2019_appetite` |
| 174 | 2019 | suezou | アイドル裏営業～汚されたステージ衣装～体験版 | - | タイトル | `b2019_channel_ubai` |
| 175 | 2019 | suezou | VenusBlood FRONTIER International SLG Trial 日本語版 | KiriKiri | タイトル | `b2019_dualtail` |
| 176 | 2019 | suezou | アイキス 豪華体験版 | NeXAS | タイトル | `b2019_giga` |
| 177 | 2019 | suezou | どっちのｉが好きですか？　Ｈシーン体験版　Ver1.00 | BGI | タイトル | `b2019_hooksoft` |
| 178 | 2019 | suezou | お姉さん先生　体験版 | - | タイトル | `b2019_kaguya` |
| 179 | 2019 | suezou | WhiteBlue　web体験版ver2.0 | - | タイトル | `b2019_lilim_darkness` |
| 180 | 2019 | suezou | b2019_marshmallow_soft | - | タイトル (待機延長) | `b2019_marshmallow_soft` |
| 181 | 2019 | suezou | モウソウスピーカー～Ｈな妄想が聴こえてくるよ～　体験版 | KiriKiri | タイトル | `b2019_sator` |
| 182 | 2019 | suezou | b2019_sukerasparo | BGI/Ethornell | タイトル (BGI インストーラ: メニューをクリック) | `b2019_sukerasparo` |
| 183 | 2019 | suezou | おにギュＨ＿ＨＣＢ体験版 | KiriKiri | タイトル | `b2019_tinkle_position` |
| 184 | 2020 | suezou | 墓多 DYING ZOMBIES -体験版- | KiriKiri | タイトル | `b2020_akabeesoft3` |
| 185 | 2020 | suezou | 優来（ゆら）があいつに抱かれる理由（わけ） 体験版 | KiriKiri | タイトル | `b2020_at_sakura` |
| 186 | 2020 | suezou | アナベル・メイドガーデン体験版 | QLIE | タイトル (ログに例外あり) | `b2020_barista_lab` |
| 187 | 2020 | suezou | はじカノ -君がいる部屋- 体験版 | - | タイトル | `b2020_caramel_box_strawberry` |
| 188 | 2020 | suezou | 悪堕ち 魔導処女～絶対支配の触手調教～　体験版 | KiriKiri | タイトル | `b2020_chaos_r` |
| 189 | 2020 | suezou | 爆乳淫辱霊媒師　【体験版】 | Softpal | タイトル | `b2020_erectlip` |
| 190 | 2020 | suezou | アイシング-love coating- 体験版 Version: 1.0.0.1 [data ver:1.00] | Unity | タイトル | `b2020_hachimitsu_soft` |
| 191 | 2020 | suezou | リトルシックガール　～桃蜜は妹の香り～　体験版 | KiriKiri | タイトル | `b2020_lass_pixy` |
| 192 | 2020 | suezou | b2020_marshmallow_soft | - | タイトル (待機延長) | `b2020_marshmallow_soft` |
| 193 | 2020 | suezou | shin-neko ～再会した妹との新たな関係～　【体験版】 | Softpal | タイトル | `b2020_milimili` |
| 194 | 2020 | suezou | 世話焼き奥さんで人の頼みを断れない亜衣さんにお願いして中出しハメ放題のドスケベ妻になってもらった 体験版 | KiriKiri | タイトル (再試行: 待機延長) | `b2020_pin_point` |
| 195 | 2020 | suezou | ティーリオ傭兵団の初仕事 体験版 v1.2.0 | - | 本編 (SRPG) | `b2020_rikuzendo` |
| 196 | 2020 | suezou | b2020_shiba_soft | - | タイトル (待機延長) | `b2020_shiba_soft` |
| 197 | 2020 | suezou | b2020_sukerasomero | BGI/Ethornell | タイトル (BGI インストーラ: -click でメニュー→開始) | `b2020_sukerasomero` |
| 198 | 2021 | suezou | 愛する彼女は画面の中、他の男の上で腰をふる 体験版 | KiriKiri | タイトル | `b2021_at_sakura` |
| 199 | 2021 | suezou | b2021_bishop | - | タイトル (ランチャー startup.exe から起動) | `b2021_bishop` |
| 200 | 2021 | suezou | 神聖昂燐ダクリュオン～正しい天使の育てかた～　体験版 | KiriKiri | タイトル | `b2021_chaos_r` |
| 201 | 2021 | suezou | b2021_dessert_soft | - | タイトル (再試行・パッチ済み Wine) | `b2021_dessert_soft` |
| 202 | 2021 | suezou | アイドル姉妹をエッチにプロデュース！ 体験版 | KiriKiri | タイトル | `b2021_kelp` |
| 203 | 2021 | suezou | 魔法少女消耗戦線DEADΩAEGIS_体験版 | - | タイトル | `b2021_metalogiq` |
| 204 | 2021 | suezou | ちんかもツインズ２！　～夏だ！海だ！ふたごの妹おっぱい対決だ！～　【体験版】 | Softpal | タイトル | `b2021_milimili` |
| 205 | 2021 | suezou | 売国王姫～堕落のメス豚母娘～体験版 | KiriKiri | タイトル (再試行) | `b2021_pin_point` |
| 206 | 2021 | suezou | プリンセス☆シスターズ！～四姉妹は全員あなたの許嫁～　web体験版ver1.0 | - | タイトル | `b2021_princess_sugar` |
| 207 | 2021 | suezou | 人妻LovelyMoment | - | タイトル | `b2021_stick` |
| 208 | 2021 | suezou | 淫妖蟲 穢 ～凌蝕桃源退魔録～体験版 Version01 | - | タイトル | `b2021_tinkerbell` |
| 209 | 2021 | suezou | けもの道☆ガーリッシュスクエア　体験版 | YU-RIS | タイトル | `b2021_whirlpool` |
| 210 | 2022 | suezou | 黒ギャル子ちゃんとしっぽり温泉旅行 ～Hしまくり三泊四日の旅～　【体験版】 | Softpal | タイトル | `b2022_amuse_craft_erotica` |
| 211 | 2022 | suezou | 愛妻Ｘ交姦妊活スワッピング - 体験版 - | - | タイトル | `b2022_anim` |
| 212 | 2022 | suezou | 愛する恋人を大嫌いな旧友に寝取られた件 体験版 | KiriKiri | タイトル | `b2022_at_sakura` |
| 213 | 2022 | suezou | BstTrial | Unity | タイトル | `b2022_ba_ku` |
| 214 | 2022 | suezou | あやかし遊郭～花魁道中～体験版 | YU-RIS | タイトル | `b2022_casket` |
| 215 | 2022 | suezou | 進軍ゴブリン軍団～人類総苗床化作戦～ 体験版 | KiriKiri | タイトル | `b2022_chaos_r` |
| 216 | 2022 | suezou | 華は短し、踊れよ乙女 - 体験版 | AdvWin32/WillPlus | タイトル | `b2022_ensemble` |
| 217 | 2022 | suezou | 南国プリズン体験版 | - | タイトル | `b2022_evoll` |
| 218 | 2022 | suezou | リンパにATATA! ～メス牡蠣ミルクどぴゅらっしゅ♡～ | KiriKiri | タイトル | `b2022_hending` |
| 219 | 2022 | suezou | 羞恥隷嬢学園 | - | タイトル | `b2022_lunaprism` |
| 220 | 2022 | suezou | 触災の島～Island of the Dead～体験版　Ver1.00 | KiriKiri | タイトル | `b2022_syoku` |
| 221 | 2023 | suezou | 最強の人妻ヒロインが寝取られるまで～お願い！夫にはバラさないで…～　体験版　Ver1.00 | KiriKiri | タイトル | `b2023_apadash` |
| 222 | 2023 | suezou | 洗脳航路～エイリアンの苗床に導かれるエリート女軍人たち～　体験版　Ver1.00 | KiriKiri | タイトル | `b2023_appetite` |
| 223 | 2023 | suezou | 堕とされた義姉 体験版 | KiriKiri | タイトル | `b2023_at_sakura` |
| 224 | 2023 | suezou | 神聖昂燐エストランジェ体験版 | KiriKiri | タイトル | `b2023_chaos_r` |
| 225 | 2023 | suezou | b2023_cyc_rose | - | タイトル (Go 版: 自己解凍を C: に置いて展開) | `b2023_cyc_rose` |
| 226 | 2023 | suezou | 爆乳遊廓 ～癒され太夫孕ませの夜鶴～　【体験版】 | Softpal | タイトル | `b2023_erectlip` |
| 227 | 2023 | suezou | 夜勤病棟 リメイク 体験版 | - | タイトル | `b2023_fg_remake` |
| 228 | 2023 | suezou | 瑠璃桜 | KiriKiri | タイトル | `b2023_lump_of_sugar_all` |
| 229 | 2023 | suezou | 非道狩～ヒトカリ～ 体験版 | Softpal | タイトル | `b2023_turumiku_another` |
| 230 | 2023 | suezou | モラトリアム 体験版 | - | タイトル | `b2023_wendybell` |
| 231 | 2024 | suezou | 性調教済だった愛する彼女 体験版 | KiriKiri | タイトル | `b2024_at_sakura` |
| 232 | 2024 | suezou | ちょっとナマイキなトイレの花子さん ～えっちぃ学園のおばけとエロエロ搾精性活～ 【体験版】 | Softpal | 本編テキスト | `b2024_milimili` |
| 233 | 2024 | suezou | VenusBlood RAGNAROK International_EN AdvTrial | Unity | 本編 (ゲーム開始後のマップ) | `b2024_ninetail` |
| 234 | 2024 | suezou | 狙われた優等生　身代わりの代償　体験版 | EntisGLS | タイトル | `b2024_syrup_mm` |
| 235 | 2024 | suezou | ムジナ 体験版Ver02 | - | タイトル (kernelbase + ntdll パッチ) | `b2024_tinkerbell` |
| 236 | 2025 | suezou | お姉ちゃんのあまエロレッスン２ ～お隣の巨乳お姉ちゃんにイッパイ甘やかされるめちゃエロライフ～  - 体験版 - | FVP | タイトル (再試行・Go 版でパッケージ化) | `b2025_anim` |
| 237 | 2025 | suezou | 学生会長・紫藤喜那の淫鬱 体験版 | KiriKiri | タイトル | `b2025_at_sakura` |
| 238 | 2025 | suezou | のーぶるバトラー TRIAL EDITION | AdvWin32/WillPlus | タイトル | `b2025_ensemble_sweet` |
| 239 | 2025 | suezou | 爆乳淫乱ビーチ　体験版 | Softpal | タイトル | `b2025_erectlip` |
| 240 | 2025 | suezou | 虜ノ焔 ～淫欲の炎に満ちた地下闘技場～ 体験版 | FVP | タイトル (再試行: 待機延長) | `b2025_guilty` |
| 241 | 2025 | suezou | Vanilla Android　シコ猿DT大学生の俺と美少女アンドロイドがいろんなプレイでハメパコミッション　体験版 | KiriKiri | タイトル | `b2025_monoceros_plus` |
| 242 | 2025 | suezou | 小日向柚月と初夜したいっ！　【体験版】 | Softpal | タイトル | `b2025_unisonshift` |
| 243 | 2026 | suezou | ママラブ！２～俺の彼女は俺のママ☆世話焼き巨乳凛桜ママ～ - 体験版 - | - | タイトル | `b2026_anim` |
| 244 | 2026 | suezou | ディメンション凸ラバース!! 【体験版】 | Softpal | タイトル | `b2026_crystalia` |
| 245 | 2026 | suezou | どいなかガールズゆーとぴあ Trial Version: 1.0.0.0 [data ver:1.00] | - | タイトル | `b2026_dessert_soft` |
| 246 | 2026 | suezou | Triptych ～トリプティック～ 体験版 | - | タイトル (待機延長) | `b2026_tinkerbell` |
| 247 | 2026 | suezou | OfficeLove -クールな上司と秘密のイキ抜き- Trial Edition | AdvWin32/WillPlus | タイトル | `b2026_tokohana` |
| 248 | 2026 | suezou | 友恵の探偵物語～のぞき魔な私の天職～体験版 | - | タイトル | `b2026_wendybell` |
| 249 | - | 手動 | 有閑夫人倶楽部 体験版 | AdvHD | 本編テキスト (icu シム) | `r1_advhd` |
| 250 | - | 手動 | 遥かなるニライカナイ 体験版 | Artemis | 本編 + 音声 | `r1_artemis` |
| 251 | - | 手動 | クダンノフォークロア 体験版 | BGI | 本編テキスト (独自Setup) | `r1_bgi_kudan` |
| 252 | - | 手動 | 果つることなき未来ヨリ 体験版 | CatSystem2 | 本編テキスト | `r1_cs2_hatsumira` |
| 253 | - | 手動 | 彼岸花の咲く夜に お試し版 | NScripter | 本編 (CreateInstall) | `r1_higanbana` |
| 254 | - | 手動 | KAG3 紹介デモ | KiriKiri2 | 本編テキスト | `r1_kag3` |
| 255 | - | 手動 | 先輩、好きです……押忍！ 体験版 | KiriKiri | 本編テキスト | `r1_krkr_apt306` |
| 256 | - | 手動 | あいれぼ 体験版 | Majiro | 本編テキスト (独自インストーラ) | `r1_majiro` |
| 257 | - | 手動 | Wind RP 体験版 | Musica | 本編テキスト | `r1_musica` |
| 258 | - | 手動 | ナルキッソス SIDE 2nd | NScripter | 本編テキスト | `r1_naru2` |
| 259 | - | 手動 | 天翼のアスクレイン 体験版 | QLIE | 本編テキスト | `r1_qlie` |
| 260 | - | 手動 | 5 -ファイブ- 体験版 | RealLive | 本編テキスト (SFX) | `r1_reallive_five` |
| 261 | - | 手動 | The Question | Ren'Py | 本編テキスト | `r1_renpy` |
| 262 | - | 手動 | でか乳巫女と巨乳シスター 体験版 | SakanaGL | タイトル (Managed=N) | `r1_sakana` |
| 263 | - | 手動 | うみねこのなく頃に咲 体験版 | PONScripter | 本編テキスト | `r1_saku` |
| 264 | - | 手動 | プリマドール 体験版 | SiglusEngine | 本編テキスト | `r1_siglus_primadoll` |
| 265 | - | 手動 | トイレの花子さん 体験版 | Softpal | 本編テキスト | `r1_softpal_hanako` |
| 266 | - | 手動 | 転性魔王さま 体験版 | Softpal | 本編テキスト | `r1_softpal_tenmao` |
| 267 | - | 手動 | トライアンソロジー 体験版 | NScripter | 本編 | `r1_trt` |
| 268 | - | 手動 | 転生した世界で… 体験版 | TyranoScript | 本編テキスト | `r1_tyrano` |
| 269 | - | 手動 | 旭光のマリアージュ 体験版 | Unity | 本編テキスト (vnbox Wine) | `r1_unity` |
| 270 | - | 手動 | YU-RIS サンプル | YU-RIS | 演出/動画 | `r1_yuris` |

<details><summary>弱い確認 (ロゴ・注意画面・起動ダイアログまで) 0 本</summary>

| 年 | タイトル | 止まったところ | ID |
|---|---|---|---|

</details>
<!-- TITLES:END -->

## DRM (DLsite / DMM) について

<!-- DRM:BEGIN -->
| 販売所 / 方式 | 製品版の仕組み | 体験版 | この環境での確認 |
|---|---|---|---|
| DLsite **PlayDRM** | オンライン認証 (アカウント、3〜5 台まで) | DRM なしで配布 | 体験版 157 本中から抽出して動作確認。製品版は購入が必要で未確認 |
| DLsite **シリアル番号発行型** | シリアルでオンライン認証 (アカウントに紐付かない) | DRM なし | 同上 |
| **ソフト電池** (DLsite / DMM) | 専用ランタイムで ID 認証、認証回数の上限あり | DRM なし (体験版は普通の exe) | 自前 Wine に fitudao 氏のソフト電池向け修正 (crypt32 / imagehlp / msi) を当て済み。製品版は未確認 |
| **DMM GAME PLAYER** (FANZA) | 専用クライアント経由で起動、起動ごとにオンライン確認 | — | DMM / FANZA はこの環境 (海外) からはログイン画面にしか行けず、体験版もクライアントも取得できない |
| exe プロテクタ (Enigma Protector 等) | 実行ファイルの暗号化・改ざん検知 | 体験版にも掛かっていることがある | 第6弾で検出し、Wine で起動することを確認 (下表の「痕跡」) |

- DLsite の作品ページから製品版の DRM 種別を読み取る: `scripts/tools/dlsite_drm.py` (「PlayDRM型」「ソフト電池」「シリアル番号発行型」の表記)。体験版 (`trial.dlsite.com`) にはどの方式も入っていなかった (exe・DLL の文字列と readme を確認)。
- `vnbox add` は配布物の exe / DLL から DRM・プロテクタの痕跡 (PlayDRM / ソフト電池 / DMM GAME PLAYER / StarForce / SecuROM / Themida / VMProtect / Enigma 等の文字列・PE セクション名) を探し、見つかれば `launch.ini` の `drm=` に記録してギャラリーに「DRM」と表示する。シグネチャは公開情報からの推測で、製品版での実地確認はまだ。
- 製品版の DRM を実際に確かめるには、購入済みの配布物 (または DMM / DLsite のアカウント) が必要。用意できれば `vnbox add` → 起動 → 認証画面までをこの手順で確認できる。
<!-- DRM:END -->

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
15. **ロケールを忘れた手動実行**: BGI は `GetSystemDefaultLangID` が日本語でないと何も言わずに終了する。手で wine を叩くときも `LANG=ja_JP.UTF-8` / `TZ=Asia/Tokyo` を付ける (vnbox はすべて自動で付ける)。
16. **InstallShield**: 新しめの setup.exe (署名 "InstallShield" がオーバーレイにある版) は中身がファイル名由来の鍵で XOR + ニブル入替されている。Wine でブートストラッパを動かすより、取り出して MSI を直接入れる方が確実。
17. **クリックの長さ**: Eushully AGE などフレームごとにマウス状態を見るゲームは、xdotool の一瞬のクリックを取りこぼす。配信の入力は「押して 60ms 待って離す」にした。
18. **XAML Islands**: Cyberworks TinkerBell の新しい版 (せをはやみ。) は C++/WinRT で `Windows.UI.Xaml.Hosting.DesktopWindowXamlSource` と TextBox を作る (文字入力欄用)。Wine には Windows.UI.Xaml の実装が無く、未捕捉の例外 (c0000409) で落ちる。何もしない代用品 `xamlstub32.dll` を Windows SDK のメタデータから生成して prefix に登録すると先へ進む (`tools/xamlstub/`、`scripts/verbs/xamlstub.sh`。`vnbox add` が exe 内の文字列から判定して `winetricks=vnbox-xamlstub` を付ける)。vtable は x86 stdcall の引数バイト数まで正確でないとスタックが壊れるので、手書きでなく .winmd から生成している。入力欄は表示されない (名前入力などは既定値のまま)。
19. **Electron 製 (TyranoScript v5 等)**: GPU プロセスが Wine + GPU なしで起動できず「Cannot initialize WebGL」。`--in-process-gpu --use-angle=swiftshader --enable-unsafe-swiftshader --ignore-gpu-blocklist` で動く。`app.asar` があれば `vnbox add` が起動引数に付ける。
20. **InstallShield 2010 以降**: setup.exe 末尾の `ISSetupStream` 形式 (属性 48 バイト + UTF-16 名、名前の UTF-8 から作る鍵で XOR + ニブル入替、その後 zlib) から MSI を取り出して `msiexec /qn` で入れる。旧形式 (`InstallShield`) と同じく Wine でブートストラッパを動かさない。
21. **入れ子の自己解凍**: zip の中身が ZIP 自己解凍 exe 1 つだけ、という配布がある (スペルマン)。exe をゲームとして起動すると何も出ないので、もう一段展開する。
11. **インストーラの自動操作**: MSI は `msiexec /qn`、それ以外 (CreateInstall / BGI / Majiro / CatSystem2 / InstallShield / 独自) は `wizard.sh` が小さいダイアログに Enter (効かなければ Space) を送り続けるだけで、今回の 8 種すべて完了した。WinRAR SFX は Linux 側の unrar で開けないことがあり、Wine で SFX 自体を動かして展開した。

22. **ウィンドウの大きさ**: WM が無いと最大化・全画面の要求が実行されず、Wine 側は 1920x1080 なのに X のウィンドウが小さいまま右下が切れていた (かけぬけ★青春スパーキング)。枠を描かない openbox を Xvfb ごとに動かす。`run.sh` は Wine 内から見たクライアント領域と X のウィンドウを比べ、X 側が小さければ `NG:size_mismatch` にする。
23. **自前で枠を描くウィンドウ**: Chromium 系 (NW.js / Electron) は WM_NCCALCSIZE でクライアント領域を枠いっぱいに広げる。Wine は WM が枠を描く前提でタイトルバー分を X ウィンドウから外すので、上 30px が欠けていた (RPG ツクール MZ 製)。win32u パッチでウィンドウ全体を出す。
24. **NW.js も Chromium**: RPG ツクール MZ の一部は GPU プロセス無しでは真っ黒のまま。Electron と同じ引数を `vnbox add` が NW.js (nw.dll) にも付ける。
25. **ダイアログの重なり**: 「フルスクリーンで表示しますか？」や「ディスプレイ設定を変更しますか？」がゲーム本体の大きなウィンドウの手前に出る。`pass:` は最大のウィンドウでなく、いちばん小さいダイアログ大のウィンドウに既定のボタンを押させ、続けて出るダイアログも数秒待つ。
26. **既定のボタンが無いインストーラ**: 「インストール」「同意する」「次へ」を Enter で押せないものは `tools/clickbtn` (Wine 内で BM_CLICK) がラベルで探して押す。ボタンを自前で描く BGI のメニュー (子ウィンドウなし) は、ウィンドウクラスと大きさで位置を決め打ちする。押下は PostMessage (モーダルダイアログを開くと SendMessage が戻らない)。
27. **prefix の hardlink**: 複製した prefix は base と hardlink を共有するので、`cp` で既存ファイルを上書きすると base まで書き換わる (DXVK や VC++ の DLL が base に入っていた)。`--remove-destination` で置き換え、winetricks の前は system32 等を自前のコピーにする。
28. **起動ランチャー**: BISHOP の startup.exe は作品によって「ゲーム開始」で C++ 例外を出して終わる。startup.ini の `[EXE] Boot=/Demo=` を直接起動する。Eushully の InstallLauncher.exe は隣の Inno の setup.exe を無人で実行すれば済む。
29. **Unity 6 (6000.x)**: wined3d では黒画面。DXVK で描画されるので `vnbox add` が UnityPlayer.dll の版を見て `vnbox-dxvk` を付ける。ただし入力が届かない作品がある (不倫旅館 体験版: マウスもキーボードも無反応)。Unity 6 は EnableMouseInPointer を呼ぶので WM_POINTER への変換を試作したが、この作品は通常のマウスメッセージに戻して処理しており効果がなかった。未解決。
30. **VR 専用**: DLsite の「VR」作品には OpenXR ヘッドセットが無いと何も描かないものがある (対象外)。

## 次にやること

- GPU のある環境ではハードウェアエンコード (VAAPI / NVENC) に切り替えて CPU を空ける。
- 本物の S3 / R2 で `store.sh` を通しで確認。複数サーバで同じ人のセーブを同時に書かないよう、プレイ中はロック (S3 上のロックファイル) を置く。
- 批評空間に届く環境 (国内の自宅サーバ等) で `vnbox meta -all` を実データで確認し、照合の外れ方を見る。
- GPU のある自宅サーバで動画のフレームレートを再計測。
- エンジン別にブラウザ内実行 (WASM) へ逃がせるもの: 吉里吉里 (krkrsdl2)、NScripter/PONScripter (onsyuri)、Ren'Py (web 書き出し)、TyranoScript (元々 HTML5)。それ以外は配信で。

# vnbox — Windows ノベルゲームを Wine で動かし、ウィンドウ単位でブラウザ配信する

Linux サーバ上で Windows 向けノベルゲームを Wine で動かし、Android などのブラウザから遊ぶための道具一式 (Go 製のコマンド `vnbox`)。
「Windows を起動してリモートデスクトップ」の代わりに、**1ゲーム = 1つの X ディスプレイ + 1つの音声シンク + 1本の WebRTC 接続** で、ゲームのウィンドウだけを配信する。

動作確認の結果 (690 本の一覧、対象外とその理由、分かった不具合と対策) は [VERIFICATION.md](VERIFICATION.md) にまとめてある。

## アーキテクチャ

```
 配布物 (zip/lzh/7z/rar/SFX/インストーラ/msi/フォルダ)
   │  vnbox add <name> <配布物>
   │    ├ 展開して動くもの → files/Games/<name>/ に置くだけ
   │    └ インストーラ     → ベース prefix の複製上でインストール (無人 or ダイアログ自動操作)
   │                          → 前後の差分 (ファイル + レジストリ) だけを取り出す
   ▼
 packages/<name>/          ← 1 ゲーム = 1 パッケージ (Wine/OS に依存しない中身だけ)
   launch.ini              起動 exe・引数・追加設定
   files/                  C:\ 以下に置くファイル
   system.reg.frag         HKLM の差分 (BGI などインストール先キーが要るもの)
   user.reg.frag           HKCU の差分
   meta.json / cover.jpg   ギャラリー用のメタデータ
   │
   │  vnbox store push-package / pull-package  ┌────────── S3 (S3 互換可) ──────────┐
   ├──────────────────────────────────────────▶│ packages/<name>.tar.zst   (本体)     │
   │                                           │ saves/<user>/<name>/<時刻>.tar.zst   │
   │                                           │ saves/<user>/<name>/latest.tar.zst   │
   ▼  vnbox serve / vnbox play <name>          └──────────────────────────────────────┘
 prefixes/run-<name>/      ← 実行用の Wine prefix        ▲ 起動前に最新セーブを戻す
   = ベース prefix (base-vnbox) のハードリンク複製       │ プレイ中 5 分ごと + 終了時にセーブを上げる
   + パッケージを流し込み                               │ (準備後に変わったファイル + レジストリ差分だけ)
   │
   ▼
 Wine (自前ビルド /opt/wine-vnbox, 新 WoW64)。1 ゲームごとに:
   Xvfb :N + openbox (枠なし) ──x11grab──▶ ffmpeg H.264 ──┐   WebRTC (映像・音声・入力のデータチャネル)
   PulseAudio null シンク ──monitor──▶ ffmpeg Opus ─────────┤ vnbox serve (HTTP :port) ◀──▶ ブラウザ (Android 等)
   xdotool ◀── データチャネル / POST /input (タップ・キー) ──┘
```

- **ベース prefix** (`prefixes/base-vnbox`): 64bit / Win10 / コードページ 932 / 日本語フォント置換 / ＭＳ ゴシック等の実体フォント / icu.dll / 仮想デスクトップ無し。各ゲームの prefix はこれのハードリンク複製なので、1 本あたりの追加ディスクはゲーム本体とセーブ分だけ。
- **パッケージ**はゲームの中身だけで、Wine や prefix を含まない。ベースや Wine を更新しても作り直さずに使える。
- **1 ゲーム = 1 X ディスプレイ + 1 音声シンク**。複数人・複数本を同時に動かしても映像・音声・入力は混ざらない。

## 準備

```bash
cd go && go build -o ../bin/vnbox ./cmd/vnbox && cd ..     # Go 1.24+
# ベース prefix (一度だけ)。自前 Wine (下記) 用の 64bit / Win10 のもの
WINE_FLAVOR=vnbox WINEARCH=win64 scripts/mkbase.sh prefixes/base-vnbox
# 従来の Wine (-wine classic) も使うなら: scripts/mkbase.sh (32bit / Win7) と WINEARCH=win64 scripts/mkbase.sh prefixes/base64
```

必要なもの: Wine (自前ビルドは `/opt/wine-vnbox`)、Xvfb、openbox、PulseAudio、ffmpeg (libx264 / libopus)、xdotool、7z / unrar / lha、winetricks、`ja_JP.UTF-8` ロケール、日本語フォント (IPA / Noto)。

## 使い方

```bash
bin/vnbox add mygame ~/Downloads/trial.zip -title "○○ 体験版"   # パッケージ化 (VNBOX_S3 があれば S3 にも上げる。-no-upload で抑止)
bin/vnbox add mygame2 ~/Downloads/setup.exe                       # インストーラでも同じ
bin/vnbox add mygame3 game.zip -exe 'Game/start.exe'              # 起動 exe を明示
bin/vnbox add mygame4 setup.zip -click "157,408"                  # インストーラの最初の画面 (BGI のメニュー等) でクリックする座標 (ウィンドウ基準)
bin/vnbox serve -addr :8080 -token <秘密の文字列>     # ポータル: ギャラリーから「遊ぶ」で起動・配信、「終了」でセーブを上げて停止
bin/vnbox play mygame -addr :8080 -token <...>      # 1 本だけ起動して配信 (ゲームを閉じるか Ctrl-C で終了、終了時にセーブ)
bin/vnbox meta mygame -fetch                        # 批評空間 (届かなければ VNDB) からブランド・発売日・点数・タグ・紹介・カバーを取得
bin/vnbox meta -all                                 # meta.json の無い全パッケージに対して取得
bin/vnbox meta-search 姫狩り                         # 候補 ID の一覧 → bin/vnbox meta mygame -id egs:12345 / -id vndb:v1195 で指定
bin/vnbox meta mygame -title T -engine E -thumb 画像 # ギャラリー表示を手で直す
bin/vnbox store list | history <name> | push-package <name> | pull-package <name> | save-push <name> | save-pull <name> [時刻]
bin/vnbox capture begin <name> / capture end <name> # 手動インストールの差分取り
bin/vnbox prepare <name> [-prefix dir]              # 実行用 prefix を準備するだけ
bin/vnbox detect <フォルダ> / extract <アーカイブ> <出力先> / regdiff diff|apply|show
```

- 共通の `-wine vnbox|classic` (既定 vnbox、環境変数 `VNBOX_FLAVOR`) で Wine を選ぶ。データの置き場所は `$VNBOX` (既定 `/home/user/vnbox`)。
- **認証**: `-token` (または `VNBOX_TOKEN`) を付けると、トークンを知らない人は一覧も配信も操作もできない (一度 `?token=` で開くと Cookie)。トークン無しで起動すると警告を出す。
- **インストーラの扱い**: MSI は `msiexec /qn`、Inno は `/VERYSILENT`、NSIS は `/S`。InstallShield は setup.exe に埋め込まれた MSI を Wine を使わずに取り出して入れる。それ以外 (CreateInstall / BGI / 独自) はダイアログの既定ボタンを押し続け、「インストール」「同意する」「次へ」などはラベルで探して押す (`tools/clickbtn`)。WinRAR 自己解凍は空の作業フォルダで Wine に展開させる。
- **起動 exe の推定が外れたら** `packages/<name>/launch.ini` の `target=` を直す (C:\ からの相対パス)。`args=`、`renderer=` (gl/vulkan)、`winetricks=` (winetricks の verb、または `vnbox-<名前>` で `scripts/verbs/<名前>.sh`) も書ける。エンジンに応じて `vnbox add` が自動で付けるもの: Unity 6 → DXVK、NW.js / Electron → Chromium の GPU 回避引数、Godot 4 → Vulkan、XAML Islands → 代用品 DLL など。
- 起動ダイアログ (画面モード選択など) は配信画面でそのまま操作すればよい。
- **テスト**: `cd go && go test ./...` (S3 部分は S3 互換のテストサーバ moto を自動で起動して確認。無ければスキップ)。

## ストリーミングの方式

Go 版 (`vnbox serve` / `vnbox play`) は **WebRTC (H.264 + Opus)** だけで配信する。映像・音声・入力を 1 本の接続にまとめ、別の線 (MJPEG や HTTP の音声) は持たない。

| 経路 | 方式 | 実装 |
|---|---|---|
| 映像 | ゲームウィンドウの矩形だけを `ffmpeg -f x11grab` で取り込み、libx264 (constrained baseline, zerolatency, 30fps, CRF 23, 上限 3Mbps) で H.264 にして WebRTC で送る。H.264 を復号できないブラウザには VP8。エンコーダはゲームごとに 1 つで、見ている人全員に配る (誰も見ていなければ 20 秒後に止める) | `go/internal/stream/rtc.go` |
| キーフレーム | 定期は 10 秒ごと。新しい視聴者が来たときと、視聴者がパケット欠落を知らせたとき (PLI / FIR) にエンコーダを起動し直して即座にキーフレームを出す (3 秒に 1 回まで) | 同上 |
| 音声 | ゲーム専用の PulseAudio null シンクの monitor を Opus 96kbps (20ms) にして WebRTC で送る。モバイルは最初のタップで消音を解除 | 同上 |
| 入力 | WebRTC のデータチャネルで JSON を受け、表示倍率を逆算した座標で `xdotool` がそのウィンドウへ送る (データチャネルが無いときは `POST /input`)。タッチ操作は下記 | `stream.go` / `page.html` |
| シグナリング | ページが offer を `POST rtc?codec=h264|vp8` に送り、ICE 収集を済ませた answer を返す 1 往復 (ポータルの認証の内側) | `rtc.go` |
| 繋がらないとき | 12 秒以内に繋がらなければ理由を画面に出し、10 秒後に繋ぎ直す (下の「ネットワーク設定」を参照) | `page.html` |

帯域の実測 (1280x720、同じ画面を同じ時間受信。MJPEG は以前の方式で、今は使わない):

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

## ゲームデータとセーブの保存 (S3)

`vnbox store` (と `serve` / `play` / `add`) がパッケージ (本体) とセーブを S3 または S3 互換ストレージ (Cloudflare R2 / MinIO / Wasabi 等) に置く。

```bash
# 環境変数、または scripts/store.conf (ひな形は scripts/store.conf.example)
VNBOX_S3=s3://my-bucket/vnbox
S3_ENDPOINT=https://<account>.r2.cloudflarestorage.com   # AWS S3 なら不要
VNBOX_USER=yashikota                                     # セーブの持ち主 (人ごとに分ける)
SAVE_INTERVAL=300                                        # プレイ中にセーブを上げる間隔 (秒)
# 認証は AWS 標準: AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_PROFILE
```

- **自動化**: `VNBOX_S3` が設定されていれば、`serve` / `play` は手元にパッケージが無ければ S3 から取得し、起動前に最新セーブを戻し、プレイ中 5 分ごとと終了時にセーブを上げる。内容が前回と同じなら上げない。
- **セーブに含めるもの**: 起動準備の後に変更・作成されたファイル (ゲームフォルダ内の `.sav` 類、`AppData`、`Documents` など。`windows/` と一時ファイルは除外) と、HKCU/HKLM のレジストリ差分。ゲームごとの保存場所を知らなくても拾える。サイズは通常 KB〜数 MB。
- **履歴**: `saves/<user>/<name>/<UTC時刻>.tar.zst` を消さずに積むので、任意の時点に戻せる (`vnbox store history <name>`、`save-pull <name> <時刻>`)。古いものは S3 のライフサイクルルールで期限切れにするとよい。
- サーバを捨てても `packages/` と `saves/` が S3 にあれば、新しいサーバでベース prefix を作って `vnbox serve` するだけで続きから遊べる。

## Go のパッケージ構成 (`go/`)

| パッケージ | 役割 |
|---|---|
| `cmd/vnbox` | コマンド本体 |
| `cmd/rtcprobe` | ブラウザ無しで WebRTC 接続して受信した H.264 / Opus を保存する確認用ツール |
| `internal/wine` | Wine の選択 (vnbox / classic)、環境変数、Xvfb・openbox・PulseAudio、prefix のハードリンク複製 |
| `internal/regdiff` | Wine の .reg を直接 diff / merge |
| `internal/detect` | エンジン・インストーラ・依存の判定、起動 exe の推定、PE アーキテクチャ |
| `internal/extract` | zip (CP932 / GBK 名を UTF-8 へ、zip slip 防止) / lzh / 7z / rar / SFX の展開 |
| `internal/install` | インストーラ判定 (MSI / Inno / NSIS / InstallShield / 名前不定のインストーラ)、無人インストール、ダイアログ自動操作、WinRAR SFX を Wine で展開、InstallShield setup.exe の埋め込み MSI を Wine を使わずに取り出す (`isx.go`) |
| `internal/capture` | インストール前後の差分をパッケージ化 |
| `internal/add` | 配布物 1 つ → パッケージ |
| `internal/pkg` | パッケージの読み書き、実行用 prefix の準備 |
| `internal/store` | S3 (AWS SDK v2): パッケージとセーブの保存・取得・履歴 |
| `internal/stream` | 配信: WebRTC (H.264 / VP8 + Opus、データチャネル入力、pion/webrtc)、タッチ操作のページ |
| `internal/session` | 1 本のゲームの起動〜終了 (1x1 ウィンドウ時の Managed=N 再起動、定期・終了時のセーブ) |
| `internal/meta` | 批評空間 (SQL フォーム) / VNDB (Kana API) からのメタデータ取得、タイトルの照合 (「体験版」等を除いて正規化)、カバー画像の保存 |
| `internal/portal` | ギャラリー・詳細画面、サムネイル (プレイ中はライブ)、トークン認証、複数セッションの管理 |
| `internal/x11` | X ウィンドウの列挙・入力 (xdotool)、インストーラのダイアログ自動操作 |

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
go/                      vnbox 本体 (上記)
bin/vnbox                ビルドしたコマンド
scripts/
  mkbase.sh              ベース prefix 作成 (env.sh / base.reg.in / fonts-ja.reg を使う)
  env.sh                 mkbase.sh と run.sh の共通環境 (ja_JP.UTF-8, TZ=Asia/Tokyo, Wine の選択, Xvfb, PulseAudio)
  base.reg.in            ベースのレジストリ調整 (仮想デスクトップ無し, XRandR モード変更無効, クラッシュダイアログ無効)
  fonts-ja.reg           MS ゴシック/明朝/メイリオ/游 → IPA/Noto の置換 (gen_fonts_reg.py で生成)
  verbs/                 launch.ini の winetricks=vnbox-<名前> で使う手順 (icu.dll シム / MS 別名フォント / DXVK / VC++ / .NET / XAML 代用品)
  wm/openbox-rc.xml      ゲームごとの X に付ける枠なし openbox の設定
  store.conf.example     S3 の設定のひな形
  run.sh                 動作確認用: 操作列 INPUT (click/key/shot/sleep/pass) で起動してスクショ・ログを results/ へ
  pearch.sh / stopall.sh exe の 32/64bit 判定 / 確認中のゲーム・Xvfb を全部止める
  tools/                 一括の動作確認 (batch6 / recheck6 / verdict / sheet / record6) と候補集め・台帳からの md 生成
tools/                   Wine 内で動かす補助 (clickbtn: ラベルでボタンを押す、xamlstub: XAML Islands の代用品)
recipes/add_hints.tsv    インストーラでクリックが要ったタイトルの座標メモ
packages/<name>/         パッケージ
prefixes/                ベース prefix と実行用 prefix
results/verified.tsv     起動確認済みタイトルの台帳 (VERIFICATION.md の一覧はここから生成)
results/excluded.tsv     対象外のタイトルと理由
wine-build/              自前 Wine のパッチ (third_party/ は fitudao 氏のパッチ、LGPL-2.1)
VERIFICATION.md          動作確認の記録
```

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
| `wine-build/vnbox-win32u-mouse-in-pointer.patch` | win32u / user32: EnableMouseInPointer が未実装 (失敗を返す) だった。Unity 6 の入力システムはこれを呼んだ後、マウスを WM_POINTER でしか受け取らない。上流 (Wine 11.3 以降, MR 10120) と同じく、通常のマウスメッセージはそのまま配りつつ、その前に対応する WM_POINTER* を送り、GetPointerInfo でそのフレームを返す | 化けきつねたいじ 体験版 (Unity 6): 「はじめる」が押せない → 押せる |

後から足したパッチ (kernelbase / ntdll / qasf / winegstreamer / wined3d / quartz / win32u / user32) は、該当 DLL だけを再ビルドして `/opt/wine-vnbox` に差し替えた (元のファイルは `wine-build/orig-dlls/`)。Wine を作り直すときは `wine-build/` のパッチを全部当てて `--enable-archs=i386,x86_64` でビルドする。

## 次にやること

- GPU のある環境ではハードウェアエンコード (VAAPI / NVENC) に切り替えて CPU を空ける。
- 本物の S3 / R2 で `vnbox store` を通しで確認。複数サーバで同じ人のセーブを同時に書かないよう、プレイ中はロック (S3 上のロックファイル) を置く。
- 批評空間に届く環境 (国内の自宅サーバ等) で `vnbox meta -all` を実データで確認し、照合の外れ方を見る。
- GPU のある自宅サーバで動画のフレームレートを再計測。
- ベース prefix の作成 (`mkbase.sh`) と動作確認の操作列 (`run.sh`) も `vnbox` のサブコマンドにする。
- エンジン別にブラウザ内実行 (WASM) へ逃がせるもの: 吉里吉里 (krkrsdl2)、NScripter/PONScripter (onsyuri)、Ren'Py (web 書き出し)、TyranoScript (元々 HTML5)。それ以外は配信で。

# 動作確認の記録

vnbox で実際に体験版を動かして確かめた結果。「動作」= タイトル画面以降まで (多くは本編のテキスト送りまで) スクリーンショットを目視で確認したもの。
台帳は `results/verified.tsv` (確認済み) と `results/excluded.tsv` (対象外と理由)。下の「集計」と「起動確認済みタイトル一覧」は台帳から `vnbox ledger md` で生成する。

## 確認の手順

確認はすべて `vnbox` で行う。ゲームは `vnbox play` / `serve` と同じセッションの処理 (prefix の準備、専用の X と音声シンク、1x1 ウィンドウなら Managed=N で起動し直し) で起動するので、確認で通ったものはそのまま遊べる。

- 候補集め: `vnbox candidates suezou` (suezou ミラーの全ページのリンク) / `vnbox candidates dlsite pro|maniax` (DLsite の体験版ありの PC ゲーム。作品ページから製品版の DRM も読む) → `vnbox candidates pick` (未試行の作品フォルダ・年とブランドで分散・サイズで絞る)。
- 一括確認: `vnbox verify <一覧> -j 3` が DL → `vnbox add` → `vnbox probe` (自前 Wine で NG なら従来 Wine でもう一度) → 最後の 2 枚の一覧画像 (`results/batch/v_<id>.png`) → 結果 TSV (`id, 通った Wine, 判定, 起動 exe, エンジン, 出典, 製品版 DRM, 配布物の DRM 痕跡`)。
- 一次判定 (`probe` が出す OK / NG:<理由>): ゲームのウィンドウが最後まで残っている、Wine のクライアント領域より X のウィンドウが小さくない (画面が切れていない)、Wine がクラッシュを報告していない、最後のスクショが 400x300 相当以上で中央 80% にコントラストがある (真っ黒・真っ白でない)。
- **全件の一覧画像を目視**して、タイトル画面以降まで出ているものだけを `vnbox ledger record` で台帳に記録する。NG のものはスクショとログ (`results/v_<id>_<wine>/`) を見て、操作列 (`-input`) や `-verbs` / `-click` を変えて `vnbox verify` し直すか、パッケージを直して `vnbox probe` する。
- 1 本だけ確かめる (使い捨ての prefix `prefixes/probe-<name>` で動かし、終わったら消す): `vnbox probe <name> -input "shot:t25 pass:5 sleep:10 shot:a" -wait 25s` → `results/<name>/` にスクショ。操作列は click:x,y / key:K / sleep:N / shot:名前 / pass:N (起動ダイアログを既定ボタンで抜ける)。

第1〜6弾は、当時のシェルスクリプト (`run.sh` / `batch6.sh` など。Wine を直接起動していた) で確かめた。今は上の `vnbox` のコマンドに置き換えて削除した。

## Wine 11.20 で全件確認し直し (2026-10-21〜22)

自前 Wine を 11.0 (安定版) から 11.20 (開発版の最新) に上げ、690 本を同じ手順で配布元からやり直した。自前 Wine は GitHub Actions でビルドして Releases に置いたもの (`wine-11.20-vnbox-f216c497b0`) を `vnbox install-wine` で入れて使った。結果は `results/batch/summary_1120.tsv` (やり直し分 `summary_rerun1120.tsv` をまとめたもの)。

- **690 本すべてが一次判定 OK、一覧画像を全件目視してタイトル画面以降まで動作** (686 本が自前 Wine、4 本が従来 Wine。従来 Wine の 4 本は 11.0 のときも同じ: ユユカナ、`s2010_akabeesoft_lavender`、`dl_RJ01728288`、`dl_RJ01734456`)。KAG3 紹介デモは以前と同じく配布物の `Config.tjs` の修正が要る。
- パッチ: 9 本を 11.20 に載せ替え。`quartz-videowindow-unlocked` は 11.20 の IVideoWindow (全画面エミュレーションが入った) に合わせて書き直し。`win32u-mouse-in-pointer` (11.3 で本家に入った) と `win32u-own-frame-visible` (11.20 に同じ処理がある) は外した。化けきつねたいじ (mouse-in-pointer が効いた Unity 6) と RPG ツクール MZ の作品 (own-frame-visible が効いた) はパッチ無しの 11.20 で動作。
- 11.20 で出た退行と対処:
  - **Electron 製 2 本** (梨乃ねね 子種を乞う村 ほか) が成人確認の後にメモリを 12GB まで使って落ちた。11.20 ではゲーム同梱の `vulkan-1.dll` より Wine 内蔵のものが読まれ、Chromium (ANGLE) が同梱の SwiftShader を使えずにホストの lavapipe で描こうとするため。ベース prefix で `vulkan-1` を `native,builtin` にして解決。
  - **お嬢様はイイナリみたいです！？** (DirectDraw): メニューをクリックすると画面の大半が黒くなる (11.0 では起きない)。`renderer=gdi` で正常。確認用一覧に `renderer` 列を足して指定。
- その他 (Wine とは無関係): 操作列のタイミング (プリマドール、Wind、生命のスペア)、自動の Enter がタイトルの「終了」を選んでいた (Magus Tale)、ニュートンと林檎の樹の起動メニュー、確認中にディスクが一杯になった数本 (同時に動かす本数を減らしてやり直して OK)。

## Go 版 (sandbox 込み) で全件確認し直し (2026-10-20〜21)

第1〜6弾で確認した 690 本を、上の手順 (`vnbox verify results/batch/recheck_all.tsv`) で配布元からの DL からやり直した。ゲームとインストーラは bubblewrap の中 (ネットワークなし、他のゲーム・パッケージ・資格情報は見えない) で動かした。結果は `results/batch/summary_go.tsv` (再試行分は `summary_ng*.tsv` / `summary_clicked.tsv` / `summary_clicks.tsv` をまとめたもの)。

- **689 本がタイトル画面以降まで動作** (一覧画像を全件目視)。残り 1 本 (KAG3 紹介デモ) は以前と同じく配布物の `Config.tjs` を直す必要がある (Wine とは無関係)。
- 1 回目の一括確認では 669 本 OK / 21 本 NG。NG はどれも Wine や sandbox が原因ではなく、vnbox 側の判定・自動操作の不具合だったので直した:
  - 60 秒のロック待ちのログを「固まった」と判定していた (QLIE / BGI の 3 本): 判定から外した
  - 名前が「インストーラ」の exe、BGI の 800x450 のインストールメニュー、LiberalInstaller の独自ボタン (`TLSFlatButton`) を扱えなかった
  - 1 つの大きな exe の作品をインストーラとして扱った結果、何もインストールされず失敗していた: 何も入らなければフォルダのまま登録する
  - BGI のメニューを 2 回目に押す位置が作品によってはアンインストールだった (ダイヤミック・デイズ): インストール後はメニューを閉じる
  - ウィンドウ名が空のゲーム (雪音の仇討ち物語) を「落ちた」と判定していた
  - SakanaGL の黒画面 (Managed=N を自動で付ける)、起動待ちが足りない作品の操作列
- 一次判定が OK でも目視で違ったもの (インストーラ画面のまま・ランチャーのまま・確認ダイアログのまま) は、上の修正の後の版でやり直して通ることを確認した。

## 機能の確認

- パッケージ化: zip 版・WinRAR 自己解凍版・Inno Setup 版・LiberalInstaller 版を `vnbox add` で追加して起動。
- 配信: `vnbox play` で S3 からの取得 → 起動 → 認証 → WebRTC の映像・音声・入力 → 終了時のセーブ保存。`vnbox serve` でポータルからの起動・停止。2026-10-20 に WebRTC のみの構成で、Chromium から H.264/VP8 映像・Opus 音声・データチャネル入力を確認。
- S3 (互換のテストサーバ moto): パッケージの往復 (内容が完全一致)、セーブの保存 → 新しい prefix へ復元 → 追加保存 → 再復元 → 過去の版への巻き戻し、起動時の自動取得・自動保存。**本物の AWS S3 / R2 では未確認**。

## 第1弾: エンジン別の代表作 (Wine 11.0 / Ubuntu 24.04 / GPU なし・ソフトウェア描画 / 2026-10-18)

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

## 第2弾: suezou ミラーの体験版を年代別に一括テスト (2006〜2026)

`scripts/tools/suezou_crawl.py` (今は `vnbox candidates suezou`) でミラー全 195 ページから 4,009 件のリンクを集め、年度ごとにブランドが重ならないよう抽出。
`scripts/tools/batch.sh` (シェル版。今は削除済みで、第6弾以降は `batch6.sh` + `vnbox add`) が DL → 展開 / 無人インストール → エンジン判定 → 起動 → スクショ を自動で回す (vnbox Wine 使用)。

第2弾 (51 本) と第3弾 (67 本、2 並列) の結果は `results/batch/summary.tsv`、各タイトルの経過スクショは `results/batch/<id>.png`。
判定は `scripts/tools/verdict.sh` (今は `vnbox probe` の判定。プロセス生存 / 最終画面が十分大きい / 画面中央にコントラスト / クラッシュ痕跡なし) で一次判定し、**全件スクショを目視**して確定した。

## 集計

<!-- STATS:BEGIN -->
**タイトル画面以降まで目視で確認できたもの: 690 本** (一覧は下の「起動確認済みタイトル一覧」)。
- 第1〜3弾: 102 本 (第1弾 22 本 + suezou ミラー 80 本)
- 第4弾 (2026-10-19): suezou ミラーから新たに 108 本、ムジナ 1 本、未解決分の解消 59 本
- 第6弾 (2026-10-19〜20、Go 版 `vnbox add` でバッチ): 候補 423 本 (suezou 240 / DLsite 183) から **419 本** (suezou 240 / DLsite 179)。対象外 4 本、弱い確認 0 本、未確定 0 本

対象外 (`results/excluded.tsv`、計 15 本) は理由付き: 配布物が紹介動画・音声のみ、配布ファイルの破損、VR 専用、GPU 必須 (D3D12 SM6 専用の UE 5.8)。
弱い確認 (0 本): 

第6弾の DLsite 分の製品版 DRM (作品ページの表記): なし 124 / PlayDRM 51 / シリアル番号 2 / ソフト電池 2 (体験版にはどれも入っていない)

第6弾のエンジン別 (`vnbox add` の判定):

| エンジン | 本数 |
|---|---|
| 判定外 (独自エンジン等) | 126 |
| KiriKiri | 72 |
| Unity | 39 |
| NW.js/Tyrano | 30 |
| NeXAS | 20 |
| YU-RIS | 19 |
| AdvHD | 15 |
| SiglusEngine | 15 |
| BGI/Ethornell | 11 |
| Softpal | 11 |
| LiveMaker | 9 |
| Electron | 6 |
| Escude | 6 |
| WolfRPG | 6 |
| AdvWin32/WillPlus | 4 |
| Artemis | 4 |
| CatSystem2 | 4 |
| Eushully AGE | 4 |
| RealLive | 4 |
| EntisGLS | 3 |
| Kaguya | 3 |
| ShiinaRio | 3 |
| HTML5 | 2 |
| AliceSoft System4 | 1 |
| Majiro | 1 |
| RenPy | 1 |
<!-- STATS:END -->

第4弾の時点で最後まで残った 2 本の原因:
- **Hello,good-bye (lumpofsugar 2010, BGI)**: BGI が D3D9 の `Caps & D3DCAPS_READ_SCANLINE` を必須にしており、Wine は 0 を返していた → wined3d パッチ。インストーラのメニュー「インストール」は `-click 157,408` (`recipes/add_hints.tsv`)。
- **姫狩りダンジョンマイスター (eushully 2009)**: InstallShield 12 のブートストラッパが Wine 上で %TEMP% への MSI 展開を途中 (209/243MB) で止め、壊れた MSI で失敗していた → setup.exe の末尾に難読化して埋め込まれた MSI を直接取り出す (`internal/install/isx.go`) と `msiexec /qn` で入る。

## 起動確認済みタイトル一覧

<!-- TITLES:BEGIN -->
タイトル画面以降まで目視で確認: **690 本** (ロゴ・注意画面止まりの 0 本は下の折りたたみに別記)。
年 = 配布ミラーの掲載年度 / DLsite の登録年 (「-」は第1弾の手動テスト分)。エンジンの「-」は自動判定できなかったもの。

| # | 年 | 出典 | タイトル | エンジン | 確認できたところ | ID |
|---|---|---|---|---|---|---|
| 1 | 2006 | suezou | その横顔を見つめてしまう ～a profile完全版～ web体験版 | KiriKiri | タイトル | `b2006_akabeesoft` |
| 2 | 2006 | suezou | b2006_alcot | - | タイトル (再試行・パッチ済み Wine) | `b2006_alcot` |
| 3 | 2006 | suezou | EXTRAVAGANZA～蟲愛でる少女～体験版 | - | タイトル | `b2006_blackcyc` |
| 4 | 2006 | suezou | 『オイラは番台２』体験版 | - | タイトル (全画面 640x480。X の解像度は変えられないので画面の左上に描画) | `b2006_interheart` |
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
| 15 | 2006 | suezou | こんな娘がいたら僕はもう～web体験版第2弾～ | KiriKiri | 本編 (冒頭の会話) | `s2006_akabeesoft_conboku` |
| 16 | 2006 | suezou | 放課後２-白濁のレッスン-体験版 | - | タイトル | `s2006_bishop_hkg2` |
| 17 | 2006 | suezou | すくーるヘブン　体験版 Ver2 - 体験版 | - | タイトル | `s2006_bishop_sh` |
| 18 | 2006 | suezou | angel breath - エンジェルブレス体験版 | NeXAS | 本編 (冒頭の文章) | `s2006_giga_ab` |
| 19 | 2006 | suezou | チアフル！ ～cheerfull!～ 【 体験版 】 | NeXAS | タイトル | `s2006_giga_cheerfull` |
| 20 | 2006 | suezou | ダンジョンクルセイダーズ（プロローグ体験版） | Kaguya | タイトル | `s2006_kaguya_dungeon` |
| 21 | 2006 | suezou | 人妻コスプレ喫茶２(体験版) | Kaguya | 本編 (会話。冒頭は背景が黒い演出) | `s2006_kaguya_hitocos2` |
| 22 | 2006 | suezou | 妹巫女・萌 | NeXAS | タイトル | `s2006_lune_maimiko` |
| 23 | 2006 | suezou | くのいち・咲夜－体験版－ | - | タイトル (起動直後に名前入力ダイアログ) | `s2006_lune_sakuya` |
| 24 | 2006 | suezou | 遥かに仰ぎ、麗しの - 体験版 | AdvWin32/WillPlus | タイトル | `s2006_pulltop_haruka` |
| 25 | 2006 | suezou | 逃亡者毒島音露第３話（体験版） | - | タイトル | `s2006_waffle_tobosya3` |
| 26 | 2006 | suezou | 逃亡者 毒島音露 第4話 体験版 | - | タイトル (LZH を 7z で展開: lha が Shift_JIS 名を小文字化して CAB が別フォルダに分かれていた。手動で確認) | `s2006_waffle_tobosya4` |
| 27 | 2007 | suezou | b2007_akabeesoft | - | タイトル (Go 版: Inno Setup を無人インストール) | `b2007_akabeesoft` |
| 28 | 2007 | suezou | 必殺痴漢人　- TRIAL VERSION - | - | タイトル→メニュー | `b2007_anim` |
| 29 | 2007 | suezou | 姫奴隷 | - | タイトル | `b2007_bishop` |
| 30 | 2007 | suezou | かみぱに！体験版 | QLIE | タイトル | `b2007_clochette` |
| 31 | 2007 | suezou | あかね色に染まる坂 | - | タイトル | `b2007_feng` |
| 32 | 2007 | suezou | ハーレム×すくらっち体験版 | RealLive | タイトル | `b2007_hadashi` |
| 33 | 2007 | suezou | でるた！（体験版） | Kaguya | タイトル | `b2007_kaguya` |
| 34 | 2007 | suezou | 看護戦隊ナースレンジャー | - | タイトル | `b2007_lilim` |
| 35 | 2007 | suezou | b2007_lilim_darkness | - | タイトル (再試行・パッチ済み Wine) | `b2007_lilim_darkness` |
| 36 | 2007 | suezou | 風輪奸山　体験版 | - | タイトル | `b2007_lune` |
| 37 | 2007 | suezou | しおふきマーメイド体験版 | - | ゲーム開始 (名前入力) | `b2007_marine` |
| 38 | 2007 | suezou | リリカル♪りりっく体験版 | EntisGLS | コンフィグ画面 | `b2007_marmalade` |
| 39 | 2007 | suezou | 『Bullet Butlers』Web体験版　ver1.1 | - | 本編 | `b2007_propeller` |
| 40 | 2007 | suezou | 聖炎天使エレアノール 体験版 | RealLive | タイトル | `b2007_sagaplanets` |
| 41 | 2007 | suezou | b2007_whirlpool | YU-RIS | タイトル (最終画面は暗転) | `b2007_whirlpool` |
| 42 | 2007 | suezou | 虐襲弐　～巫女ノ祭壇～　体験版 | - | タイトル | `s2007_anim_gyaku2` |
| 43 | 2007 | suezou | 籠の中の戦乙女  -TRIAL VERSION- | - | タイトル | `s2007_anim_val` |
| 44 | 2007 | suezou | 姫さまっ、お手やわらかに！ -体験版- | NeXAS | 本編 | `s2007_giga_himesama` |
| 45 | 2007 | suezou | 虜ノ姫（体験版） | Kaguya | タイトル | `s2007_kaguya_toriko` |
| 46 | 2007 | suezou | 女神大戦 | - | タイトル | `s2007_lilim_megami` |
| 47 | 2007 | suezou | とらぶるサッキュバス | - | タイトル | `s2007_marine_succubus` |
| 48 | 2008 | suezou | ＷＬＯ-世界恋愛機構- web用体験版 | KiriKiri | タイトル | `b2008_akabeesoft` |
| 49 | 2008 | suezou | 息子の友達に犯されて  - TRIAL VERSION - | - | タイトル | `b2008_anim` |
| 50 | 2008 | suezou | Hime のち Honey - 体験版 | KiriKiri | タイトル | `b2008_asa_project` |
| 51 | 2008 | suezou | b2008_bishop | - | タイトル (ランチャー startup.exe から起動) | `b2008_bishop` |
| 52 | 2008 | suezou | b2008_blackcyc | - | タイトル (再試行・パッチ済み Wine) | `b2008_blackcyc` |
| 53 | 2008 | suezou | Venus Blood Chimera | KiriKiri | タイトル | `b2008_dualtail` |
| 54 | 2008 | suezou | ヴェルディア幻奏曲体験版 | Escude | 本編 | `b2008_escude` |
| 55 | 2008 | suezou | お姉さん×すくらっち体験版 | RealLive | タイトル | `b2008_hadashi` |
| 56 | 2008 | suezou | FairlyLife WEB体験版02 H_Only Ver1.00 | - | コンフィグ画面 | `b2008_hook` |
| 57 | 2008 | suezou | いたずら極―リアル―　体験版 | FVP | タイトル (再試行・Go 版でパッケージ化) | `b2008_interheart` |
| 58 | 2008 | suezou | 夏神（体験版０２） | - | タイトル | `b2008_kaguya` |
| 59 | 2008 | suezou | びんかんアスリート | - | タイトル | `b2008_marine` |
| 60 | 2008 | suezou | 魔法少女沙枝 Vol.2 体験版 | KiriKiri | タイトル (MSI) | `b2008_mille_feuille` |
| 61 | 2008 | suezou | 晴れハレはーれむ 体験版Ver1.00 | - | タイトル | `b2008_smee` |
| 62 | 2008 | suezou | G線上の魔王 | KiriKiri | タイトル | `s2008_akabeesoft_gsen` |
| 63 | 2008 | suezou | エンゲージリンクス　体験版 | ShiinaRio | タイトル | `s2008_alcot_engagekinks` |
| 64 | 2008 | suezou | 虐襲３　- TRIAL VERSION - | - | タイトル | `s2008_anim_gyaku3` |
| 65 | 2008 | suezou | 処女狩～オトメガリ　- TRIAL VERSION - | - | タイトル | `s2008_anim_otomegari` |
| 66 | 2008 | suezou | スズノネセブン！体験版 | CatSystem2 | タイトル (quartz VideoWindow パッチ: OP 動画後のデッドロック解消) | `s2008_clochette_suzunone7` |
| 67 | 2008 | suezou | 奴隷将校クラリス　－　白濁のグローリエ　－　ＡＤＶパート体験版 | KiriKiri | タイトル | `s2008_dualtail_clarice` |
| 68 | 2008 | suezou | さかあがりハリケーン 体験版 Ver.2 | NeXAS | 本編 (冒頭の文章) | `s2008_giga_sakahari` |
| 69 | 2008 | suezou | 輪罠Ⅱ 体験版 | - | タイトル (インストーラの「次へ」で止まったページを手で一度押して完了。clickbtn を実クリックに変更) | `s2008_guilty_wana2` |
| 70 | 2008 | suezou | ダンジョンクルセイダーズ２（体験版） | LiveMaker | タイトル | `s2008_kaguya_dc2` |
| 71 | 2008 | suezou | PrimaStella（体験版１） | - | タイトル | `s2008_kaguya_primastella` |
| 72 | 2008 | suezou | 昇龍戦姫　天夢　体験版 | - | タイトル | `s2008_lilim_tenmu` |
| 73 | 2008 | suezou | 淫夢学園－体験版 | - | タイトル | `s2008_lune_inmu` |
| 74 | 2008 | suezou | 天ツ風　～傀儡陣風帖～ | KiriKiri | タイトル | `s2008_ninetail_amatu` |
| 75 | 2008 | suezou | Coming×Humming!! -カミング・ハミング- （体験版） | RealLive | タイトル | `s2008_sagaplanets_cominghumming` |
| 76 | 2009 | suezou | 愛娘という名の玩具　- TRIAL VERSION - | - | タイトル | `b2009_anim` |
| 77 | 2009 | suezou | 美乳淫妹 遥「私にイヤらしいコスプレさせるなんて……この変態！」 | Softpal | タイトル | `b2009_crossover` |
| 78 | 2009 | suezou | 夏ノ雨 -体験版- | EntisGLS | タイトル | `b2009_cube` |
| 79 | 2009 | suezou | 魔法少女ラヴィリオン 体験版 | KiriKiri | タイトル | `b2009_dualtail` |
| 80 | 2009 | suezou | 姫狩りダンジョンマイスター 体験版 | AGE (Eushully) | InstallShield ペイロードを isx 展開 → MSI /qn | `b2009_eushully` |
| 81 | 2009 | suezou | さらさらささら（体験版） | - | タイトル | `b2009_kaguya` |
| 82 | 2009 | suezou | あい☆きゃん | - | タイトル | `b2009_lilim` |
| 83 | 2009 | suezou | キスと魔王と紅茶（体験版） | EntisGLS | タイトル | `b2009_marmalade` |
| 84 | 2009 | suezou | シグナルハート 体験版 Ver.01 | QLIE | 本編 | `b2009_purplesoftware` |
| 85 | 2009 | suezou | らぶでれーしょん！ Ver1.00 | FVP | タイトル (起動時の Save フォルダ作成確認を OK) | `b2009_smee` |
| 86 | 2009 | suezou | 義母散華 - TRIAL VERSION - | - | タイトル | `s2009_anim_hahasange` |
| 87 | 2009 | suezou | 教育指導 体験版 | - | タイトル (起動ランチャー経由。ランチャーが終了するので判定は dead) | `s2009_bishop_ks` |
| 88 | 2009 | suezou | 灰色の空に堕ちた翼　体験版 Ver.1.00 | KiriKiri | タイトル | `s2009_dualtail_haiironosora` |
| 89 | 2009 | suezou | 花と乙女に祝福を　体験版 | ShiinaRio | タイトル | `s2009_ensemble_hanaotome` |
| 90 | 2009 | suezou | 乙女恋心プリスター体験版 | Escude | タイトル | `s2009_escude_prister` |
| 91 | 2009 | suezou | キュアフル！ 体験版 | NeXAS | 本編 (システム設定画面) | `s2009_giga_curefull` |
| 92 | 2009 | suezou | 素直くーる体験版 | RealLive | タイトル | `s2009_hadashi_cool` |
| 93 | 2009 | suezou | HoneyComing re:coming 体験版 Ver1.00 | - | タイトル | `s2009_hook_hcrs` |
| 94 | 2009 | suezou | ちゅぱしてあげる　体験版 | - | タイトル | `s2009_kaguya_tyupasite` |
| 95 | 2009 | suezou | おしゃぶりアナウンサー　体験版 | - | タイトル | `s2009_marine_announcer` |
| 96 | 2009 | suezou | しろくまベルスターズ♪－体験版 | AdvWin32/WillPlus | タイトル | `s2009_pulltop_bellstars` |
| 97 | 2009 | suezou | 乱れて交わる俺と姫(体験版) | - | タイトル | `s2009_waffle_rankouhime` |
| 98 | 2010 | suezou | 置き場がない！ web体験版 | KiriKiri | タイトル | `b2010_akabeesoft` |
| 99 | 2010 | suezou | 贄姫 - TRIAL VERSION - | - | タイトル | `b2010_anim` |
| 100 | 2010 | suezou | 学園３～華麗なる悦辱～ 体験版 | - | タイトル | `b2010_bishop` |
| 101 | 2010 | suezou | JINKI EXTEND Re:VISION -SECOND IMPACT- | NeXAS | タイトル | `b2010_giga` |
| 102 | 2010 | suezou | Reunion～３日間だけのプチ同棲～　体験版 | KiriKiri | タイトル | `b2010_grand_cru` |
| 103 | 2010 | suezou | キラリ☆南国小麦色　体験版 | - | タイトル | `b2010_kaguya` |
| 104 | 2010 | suezou | Hello,good-bye 体験版 | BGI/Ethornell | wined3d READ_SCANLINE パッチ + BGI インストーラ -click 157,408 | `b2010_lumpofsugar` |
| 105 | 2010 | suezou | b2010_marine | Softpal | タイトル (起動設定ファイル警告を OK で通過) | `b2010_marine` |
| 106 | 2010 | suezou | 痴覚過敏 体験版 | EntisGLS | タイトル | `b2010_undermoon` |
| 107 | 2010 | suezou | b2010_waffle | - | タイトル (インストーラが Win10 非対応 → 従来 Wine (Win7) でインストール) | `b2010_waffle` |
| 108 | 2010 | suezou | 涼風のメルト -Where wishes are drawn to each other- WEB体験版 | YU-RIS | タイトル | `b2010_whirlpool` |
| 109 | 2010 | suezou | 光輪の町、ラベンダーの少女　体験版 | KiriKiri | タイトル (従来 Wine) | `s2010_akabeesoft_lavender` |
| 110 | 2010 | suezou | ク・リトル・リトル～魔女の使役る、蟲神の触手～TRIAL-VERSIONⅡ・ヨゾラの錘 | - | タイトル | `s2010_blackcyc_cthulhu` |
| 111 | 2010 | suezou | 小夜子・体験版 | - | タイトル | `s2010_blackcyc_sayoko` |
| 112 | 2010 | suezou | 戦女神VERITA体験版 | Eushully AGE | タイトル | `s2010_eushully_ikusa_verita` |
| 113 | 2010 | suezou | bitter smile. ～ビタースマイル～ 体験版２ | NeXAS | タイトル | `s2010_giga_bittersmile` |
| 114 | 2010 | suezou | さくらビットマップ体験版 [Web Download] Ver1.00 | - | タイトル | `s2010_hook_sakura` |
| 115 | 2010 | suezou | 姉です。　体験版 | - | タイトル | `s2010_kaguya_ane_death` |
| 116 | 2010 | suezou | PrismRhythm-プリズムリズム- Web特別体験版 | BGI/Ethornell | タイトル (独自メニューのインストーラ: vnbox add -click 195,298 で Install を押す) | `s2010_lumpofsugar_prismrhythm` |
| 117 | 2010 | suezou | 悪の女幹部　体験版 | - | タイトル | `s2010_lune_evilwoman_executive` |
| 118 | 2010 | suezou | 恋神　-ラブカミ-（Web用体験版） | AdvWin32/WillPlus | タイトル | `s2010_pulltop_lovekami` |
| 119 | 2010 | suezou | キサラギGOLD★STAR | RealLive | タイトル | `s2010_sagaplanets_kisaragi` |
| 120 | 2011 | suezou | NTR彼女はいじめられっ娘 - TRIAL VERSION - | - | タイトル | `b2011_anim_team_mm` |
| 121 | 2011 | suezou | 裏教師～背徳の淫悦授業～ 体験版 | - | タイトル | `b2011_bishop` |
| 122 | 2011 | suezou | b2011_blackcyc | - | タイトル (Go 版 vnbox add/serve で WinRAR SFX から) | `b2011_blackcyc` |
| 123 | 2011 | suezou | シオンの血族 | SiglusEngine | タイトル | `b2011_catwalk` |
| 124 | 2011 | suezou | Starless 体験版 | - | タイトル (独自インストーラ) | `b2011_empress` |
| 125 | 2011 | suezou | りとる†びっち ～Little Bitch Girls in Hog Farm～ 体験版 | Softpal | タイトル | `b2011_erectlip` |
| 126 | 2011 | suezou | ホチキス - 体験版 | NeXAS | タイトル | `b2011_giga` |
| 127 | 2011 | suezou | はっぴぃふぁくとりー　体験版 | KiriKiri | タイトル | `b2011_grand_cru` |
| 128 | 2011 | suezou | 『種憑け村』序章体験版・導ノ儀 | ShiinaRio | タイトル | `b2011_guilty` |
| 129 | 2011 | suezou | StrawberryNauts Ver1.00 | - | タイトル | `b2011_hook` |
| 130 | 2011 | suezou | アルバイトの先輩の女のコに仕事を教わっていたら始まっちゃうＨな関係。　体験版 | - | タイトル | `b2011_kaguya` |
| 131 | 2011 | suezou | b2011_lumpofsugar | BGI/Ethornell | タイトル (BGI インストーラ: メニューをクリック) | `b2011_lumpofsugar` |
| 132 | 2011 | suezou | b2011_marine | Softpal | タイトル (再試行・パッチ済み Wine) | `b2011_marine` |
| 133 | 2011 | suezou | ユユカナ 体験版 | KiriKiri | タイトル (従来 32bit Wine。新 WoW64 だと kernel32 認証エラー) | `b2011_nanawind` |
| 134 | 2011 | suezou | 姫様限定！～PrincessLimited～web体験版ver1.0 | - | タイトル | `b2011_princess_sugar` |
| 135 | 2011 | suezou | b2011_pulltop | - | タイトル (再試行・パッチ済み Wine) | `b2011_pulltop` |
| 136 | 2011 | suezou | ラブラブル体験版 Ver1.00 | - | タイトル | `b2011_smee` |
| 137 | 2011 | suezou | カミカゼ☆エクスプローラー！WEB体験版 | CatSystem2 | タイトル | `s2011_clochette_kamikaze` |
| 138 | 2011 | suezou | ひなたテラス -We don't abandon you.- web体験版２ | NeXAS | タイトル | `s2011_giga_hinataterrace` |
| 139 | 2011 | suezou | しゅ～てぃんぐ妹スター☆ ～おにいちゃん、覚悟しててよね♪～　体験版 | KiriKiri | タイトル | `s2011_grand_cru_imostar` |
| 140 | 2011 | suezou | SuGirlyWish 体験版 Ver1.02 | - | タイトル | `s2011_hook_sugirly_wish` |
| 141 | 2011 | suezou | With Ribbon Hシーン体験版 | KiriKiri | OP 動画 → タイトルロゴ | `s2011_hulotte_with_ribbon` |
| 142 | 2011 | suezou | さくら色カルテット（体験版） | - | タイトル | `s2011_kaguya_sakura` |
| 143 | 2011 | suezou | 学☆王 - THE ROYAL SEVEN STARS - 体験入学版 | BGI/Ethornell | タイトル (BGI の 960x540 インストーラメニューを clickbtn で操作。手動で確認) | `s2011_lumpofsugar_gackoh` |
| 144 | 2011 | suezou | おっぱい学園マーチングバンド部！　体験版 | NeXAS | タイトル (起動時の解像度ダイアログ → 画像処理テストの確認を閉じて) | `s2011_marine_marching` |
| 145 | 2011 | suezou | でりばらっ！-deliverance of strays- 体験版 | KiriKiri | タイトル (従来 Wine) | `s2011_parasol_delivara` |
| 146 | 2011 | suezou | すきま桜とうその都会　体験版２ | - | 本編 (冒頭の文章) | `s2011_propeller_sukima` |
| 147 | 2011 | suezou | 未来ノスタルジア・web体験版v2　- タイトルメニュー - | - | タイトル | `s2011_purplesoftware_miranosu` |
| 148 | 2011 | suezou | 都合のよいセックスフレンド？体験版 | LiveMaker | 起動時の解像度選択 → 確認画面 (ゲーム描画) | `s2011_waffle_sex_friend` |
| 149 | 2011 | suezou | 舞風のメルト -Where leads to feeling destination- WEB体験版 (ver1.01) | YU-RIS | タイトル | `s2011_whirlpool_maikaze` |
| 150 | 2012 | suezou | JOKER-死線の果ての道化師- | KiriKiri | タイトル | `b2012_akabeesoft3` |
| 151 | 2012 | suezou | 中の人などいない！　体験版 | KiriKiri2 | タイトル (quartz パッチ) | `b2012_alcot` |
| 152 | 2012 | suezou | 母娘乱館 体験版 | AliceSoft System4 | 本編テキスト | `b2012_alicesoft` |
| 153 | 2012 | suezou | 学園ＮＴＲ -僕の知らない彼女の淫顔- 体験版 | - | タイトル | `b2012_atelier_gh` |
| 154 | 2012 | suezou | b2012_bishop | - | タイトル (ランチャー startup.exe から起動) | `b2012_bishop` |
| 155 | 2012 | suezou | 美脚隷嬢優奈「脚もお尻もこんなに穢すなんて……絶対に許さない！」体験版 | KiriKiri | タイトル | `b2012_cross_over` |
| 156 | 2012 | suezou | b2012_erectlip | - | タイトル (再試行・Go 版でパッケージ化) | `b2012_erectlip` |
| 157 | 2012 | suezou | 星空へ架かる橋ＡＡ 円佳SPアフター体験版 | - | タイトル | `b2012_feng` |
| 158 | 2012 | suezou | つぼい君のスイッチ！体験版 | - | 本編 | `b2012_fudegaki_soft` |
| 159 | 2012 | suezou | キスベル　体験版 | NeXAS | タイトル | `b2012_giga` |
| 160 | 2012 | suezou | リヴォルバーガール☆ハンマーレディ　体験版 | KiriKiri | タイトル | `b2012_kai` |
| 161 | 2012 | suezou | 恋妹SWEET☆DAYS 体験版 | KiriKiri | タイトル | `b2012_parasol` |
| 162 | 2012 | suezou | 籠女の繭 体験版 | - | タイトル | `b2012_tinkerbell` |
| 163 | 2012 | suezou | MonsterParkⅡ | - | タイトル | `b2012_trois` |
| 164 | 2012 | suezou | b2012_waffle | - | タイトル (解像度選択→はい をクリック) | `b2012_waffle` |
| 165 | 2012 | suezou | 兄貴の妻は俺の肉妻 - TRIAL VERSION - | - | 本編 (冒頭の文章) | `s2012_anim_team_mm_anym` |
| 166 | 2012 | suezou | 特別授業3SLG 体験版 | - | タイトル (起動ランチャー startup.exe 経由。ランチャーが終了するので判定は dead) | `s2012_bishop_tjslg` |
| 167 | 2012 | suezou | LEWDNESS ～Vita sexualis～ | Majiro | タイトル | `s2012_empress_lewdness` |
| 168 | 2012 | suezou | 紅蓮華 - 体験版 | Escude | タイトル | `s2012_escude_gurenka` |
| 169 | 2012 | suezou | LOVELY QUEST 体験版 Ver1.00 | - | タイトル | `s2012_hook_lovelyquest` |
| 170 | 2012 | suezou | スペルマン（体験版） | - | タイトル (zip 内の ZIP 自己解凍を展開するよう修正) | `s2012_kaguya_sperman` |
| 171 | 2012 | suezou | デカくてエッチな俺の姉　体験版 | NeXAS | タイトル | `s2012_marine_deka_ane` |
| 172 | 2012 | suezou | Gears of Dragoon  -RPG体験版 ver.1.00- | KiriKiri | タイトル | `s2012_ninetail_gears_of_dragoon` |
| 173 | 2012 | suezou | むりやり！？オトメＤＡＹＳ Trial Version: 1.0.0.0 [data ver:1.00] | - | タイトル | `s2012_nyann_otomeday` |
| 174 | 2012 | suezou | 彼女と俺と恋人と。―体験版 | AdvWin32/WillPlus | タイトル | `s2012_pulltop_latte_kanojo` |
| 175 | 2012 | suezou | 或ル家族ノ姦系図 体験版Ver2 | - | 本編 (メニューはマウスを重ねると項目名が出る作り。クリックで本編の文章へ。手動で確認) | `s2012_tinkerbell_kankeizu` |
| 176 | 2012 | suezou | 露出変態遊戯 体験版 | - | タイトル | `s2012_wendybell_rosyutu` |
| 177 | 2013 | suezou | ランス01体験版 | AliceSoft System4 | タイトル | `b2013_alicesoft` |
| 178 | 2013 | suezou | 息子の友達に堕とされて　体験版 | YU-RIS | タイトル | `b2013_anim` |
| 179 | 2013 | suezou | 花嫁狩凌 ～絶対快楽の白濁淫毒～ 体験版 | KiriKiri | タイトル | `b2013_cross_over` |
| 180 | 2013 | suezou | VenusBlood-GAIA- WEB専用ADV体験版 | KiriKiri | タイトル | `b2013_dualtail` |
| 181 | 2013 | suezou | お嬢様はご機嫌ナナメ 体験版 | AdvHD | タイトル (InstallShield) | `b2013_ensemble` |
| 182 | 2013 | suezou | キスアト体験版 | NeXAS | タイトル | `b2013_giga` |
| 183 | 2013 | suezou | PriministAr（体験版） Ver1.00 | - | タイトル | `b2013_hooksoft` |
| 184 | 2013 | suezou | 妹のおかげでモテすぎてヤバい。 体験版 | KiriKiri | タイトル | `b2013_hulotte` |
| 185 | 2013 | suezou | b2013_inter_heart | - | 本編 (Go 版: 自己解凍を C: に置いて展開) | `b2013_inter_heart` |
| 186 | 2013 | suezou | 肉牝Ｒ３０（体験版） | - | タイトル | `b2013_kaguya` |
| 187 | 2013 | suezou | b2013_lumpofsugar | BGI/Ethornell | タイトル (BGI インストーラ: メニューをクリック) | `b2013_lumpofsugar` |
| 188 | 2013 | suezou | ナマイキデレーション体験版 WEB Ver. | KiriKiri | タイトル | `b2013_madosoft` |
| 189 | 2013 | suezou | ぜったいマジラブ！あまイチャはぷにんぐ！体験版 Ver 1.0 | - | タイトル | `b2013_tinkerbell` |
| 190 | 2013 | suezou | どらぺこ！体験版 | AliceSoft System4 | タイトル (起動時の注意書き → タイトル) | `s2013_alicesoft_drapeko` |
| 191 | 2013 | suezou | 沈黙の女学園　- TRIAL VERSION - | - | タイトル | `s2013_anim_chingaku` |
| 192 | 2013 | suezou | 管理人さんは未亡人 - TRIAL VERSION - | - | タイトル | `s2013_anim_kanrinin` |
| 193 | 2013 | suezou | 爆乳淫乱家庭教師　【体験版】 | Softpal | タイトル | `s2013_erectlip_katekyo` |
| 194 | 2013 | suezou | 催眠術３体験版 | - | タイトル | `s2013_fudegaki_soft_saimin3` |
| 195 | 2013 | suezou | 夜這いする七人の孕女～体験版2～ | AdvHD | タイトル | `s2013_guilty_yobai` |
| 196 | 2013 | suezou | ＬＯＶＥＬＹ×Ｃ∧ＴＩＯＮ２ | KiriKiri | タイトル | `s2013_hibiki_works_lovely_cation2` |
| 197 | 2013 | suezou | 巨乳家族催眠　体験版 | NeXAS | タイトル (ディスプレイ設定の確認ダイアログ 2 つを閉じて) | `s2013_lune_kazokusaimin` |
| 198 | 2013 | suezou | ココロ＠ファンクション！―えっちシーン体験版 | AdvHD | タイトル | `s2013_pulltop_cocoro` |
| 199 | 2013 | suezou | カルマルカ＊サークル 体験版 | SiglusEngine | タイトル (従来 Wine) | `s2013_sagaplanets_karumaruka` |
| 200 | 2013 | suezou | ひと肌脱ぎます！体験版 | - | タイトル | `s2013_wendybell_hitohada` |
| 201 | 2014 | suezou | 体験版 甘えてくれる恋人とのエッチなひととき編 | CatSystem2 | タイトル | `b2014_azarashi_soft` |
| 202 | 2014 | suezou | b2014_ensemble | - | タイトル (再試行・パッチ済み Wine) | `b2014_ensemble` |
| 203 | 2014 | suezou | b2014_ensemble_sweet | - | タイトル (再試行・Go 版でパッケージ化) | `b2014_ensemble_sweet` |
| 204 | 2014 | suezou | Re;Lord ～ヘルフォルトの魔女とぬいぐるみ～ - 体験版 Ver. | Escude | タイトル | `b2014_escude` |
| 205 | 2014 | suezou | b2014_guilty | - | タイトル (再試行・パッチ済み Wine) | `b2014_guilty` |
| 206 | 2014 | suezou | ＰＲＥＴＴＹ×Ｃ∧ＴＩＯＮ | KiriKiri | タイトル (終了確認ダイアログまで操作) | `b2014_hibiki_works` |
| 207 | 2014 | suezou | しゃぶらレンタル　体験版 | - | タイトル | `b2014_kaguya` |
| 208 | 2014 | suezou | b2014_pulltop_latte | - | タイトル (再試行・パッチ済み Wine) | `b2014_pulltop_latte` |
| 209 | 2014 | suezou | b2014_silkys_plus | - | タイトル (メニュー start.exe ではなくゲーム本体を起動) | `b2014_silkys_plus` |
| 210 | 2014 | suezou | 奪われた学園～罠に落ちた優等生～　体験版 | KiriKiri | タイトル | `b2014_undermoon` |
| 211 | 2014 | suezou | b2014_waffle | - | メニュー (解像度選択→確認をクリックで通過) | `b2014_waffle` |
| 212 | 2014 | suezou | こすぷれえっち 体験版 | - | タイトル (MS 別名フォント) | `b2014_wendybell` |
| 213 | 2014 | suezou | G.I.B. ガールズ・イン・ブラック  WEB体験版 | YU-RIS | タイトル | `b2014_whirlpool` |
| 214 | 2014 | suezou | 春風センセーション！体験版 | CatSystem2 | タイトル (再試行: 待機延長) | `b2014_windmill_oasis` |
| 215 | 2014 | suezou | s2014_atelier_gh_8mz | - | タイトル (7-Zip 自己解凍を展開し直して) | `s2014_atelier_gh_8mz` |
| 216 | 2014 | suezou | 放課後２～白濁のレッスン～DVD | - | タイトル | `s2014_bishop_af2_dvd` |
| 217 | 2014 | suezou | 牝教師2～淫従の螺旋～IE 体験版 | - | タイトル (起動ランチャー経由。ランチャーが終了するので判定は dead) | `s2014_bishop_mk2ie` |
| 218 | 2014 | suezou | サキガケ⇒ジェネレーション！ WEB体験版 | CatSystem2 | タイトル (従来 Wine) | `s2014_clochette_sakigake` |
| 219 | 2014 | suezou | 鳥籠の姫 ～絶対妊娠の姉妹悦辱～　体験版 | KiriKiri | タイトル | `s2014_cross_over_tori` |
| 220 | 2014 | suezou | 恋する彼女の不器用な舞台 体験版 | KiriKiri | タイトル (従来 Wine) | `s2014_cube_koikano` |
| 221 | 2014 | suezou | 学園の生贄――慰み者と化した巨乳不良少女 ～白濁に侵される褐色＆堕肉の狂宴～【体験版】 | Softpal | タイトル | `s2014_erectlip_furyou` |
| 222 | 2014 | suezou | 逆転魔女裁判－痴女な魔女に裁かれちゃう－　【体験版】 | Softpal | タイトル | `s2014_erectlip_gyokumajyo` |
| 223 | 2014 | suezou | 花嫁と魔王 - 体験版 Ver. | Escude | タイトル | `s2014_escude_hanaou` |
| 224 | 2014 | suezou | 3Ping Lovers!☆一夫二妻の世界へようこそ♪　体験版 | RealLive | タイトル | `s2014_hadashi_3pl` |
| 225 | 2014 | suezou | MeltyMoment 体験版 Ver1.00 | BGI/Ethornell | タイトル (従来 Wine) | `s2014_hooksoft_meltymoment` |
| 226 | 2014 | suezou | リベンジルーム　体験版 | NeXAS | タイトル (ディスプレイ設定の確認ダイアログ 2 つを閉じて) | `s2014_lune_revenge_room` |
| 227 | 2014 | suezou | 魔女こいにっき 体験版 (第一弾) | Softpal | 本編 (起動時の確認ダイアログ → タイトル → プロローグ、クリックで文章送り。手動で確認) | `s2014_qoobrand_majokoi` |
| 228 | 2014 | suezou | 鯨神のティアスティラ 体験版 | YU-RIS | タイトル | `s2014_whirlpool_tearstilla` |
| 229 | 2015 | suezou | お兄ちゃん、教えて……～義妹とヒミツの保健実習～　体験版　Ver1.00 | KiriKiri | タイトル | `b2015_ammolite` |
| 230 | 2015 | suezou | 愛娘は、マゾ性隷～娘だからって遠慮しないでね？～　体験版　Ver1.00 | KiriKiri | タイトル | `b2015_appetite` |
| 231 | 2015 | suezou | ClosedGAME体験版 | Majiro | タイトル | `b2015_empress` |
| 232 | 2015 | suezou | b2015_ensemble | - | タイトル (再試行・パッチ済み Wine) | `b2015_ensemble` |
| 233 | 2015 | suezou | 巨乳大魔王のドスケベクエスト ～完全敗北した少年勇者クンＵＣ～　【体験版】 | Softpal | タイトル | `b2015_erectlip` |
| 234 | 2015 | suezou | 淫らに喘ぐ七人の夜這いする孕女　体験版 | AdvHD | タイトル | `b2015_guilty` |
| 235 | 2015 | suezou | ろーらいず!!! 体験版 | SiglusEngine | タイトル (TZ=Asia/Tokyo) | `b2015_hamham` |
| 236 | 2015 | suezou | トラベリングスターズ 体験版02 Ver1.00 | BGI/Ethornell | タイトル (ログに例外あり) | `b2015_hooksoft` |
| 237 | 2015 | suezou | ハラミタマ　体験版 | - | タイトル | `b2015_kaguya` |
| 238 | 2015 | suezou | b2015_lump_of_sugar | BGI/Ethornell | タイトル (BGI インストーラ: メニューをクリック) | `b2015_lump_of_sugar` |
| 239 | 2015 | suezou | 極煌戦姫ミストルティア 体験版 | KiriKiri | タイトル | `b2015_mille_feuille` |
| 240 | 2015 | suezou | b2015_minato_carnival | - | タイトル (QLIE 起動設定の「起動」をクリック) | `b2015_minato_carnival` |
| 241 | 2015 | suezou | ALIA’s CARNIVAL! Flowering Sky　体験版(WEB.ver) | KiriKiri | タイトル (以前の dead は自動操作で終了を押したため) | `b2015_nanawind` |
| 242 | 2015 | suezou | おしおき生意気ギャル ～エロいオシオキをしたら自分からエッチを求めてきた～ 体験版 | KiriKiri | タイトル | `b2015_pachipachi_soft` |
| 243 | 2015 | suezou | 発情淫母～疼く熟体は、その欲情を抑えきれない～　体験版　Ver1.00 | KiriKiri | タイトル (従来 Wine) | `s2015_appetite_inbo` |
| 244 | 2015 | suezou | ここから夏のイノセンス！ WEB体験版 | CatSystem2 | タイトル | `s2015_clochette_kokonatsu` |
| 245 | 2015 | suezou | 恋する気持ちのかさねかた_Hシーン体験版 | AdvHD | タイトル | `s2015_ensemble_koikata` |
| 246 | 2015 | suezou | エッチでヘンタイ！ヤキモチお嬢様！！　ヘンタイ性癖解放Ver．体験版 | AdvHD | タイトル (従来 Wine) | `s2015_ensemble_sweet_yakimochi` |
| 247 | 2015 | suezou | 巨乳魔王の敗北 魔王の挑戦 | Softpal | タイトル | `s2015_erectlip_maou` |
| 248 | 2015 | suezou | 恋愛フェイズ 体験版 | NeXAS | タイトル | `s2015_giga_renaiphase` |
| 249 | 2015 | suezou | 嫁探しが捗りすぎてヤバい。 体験版 | KiriKiri | タイトル | `s2015_hulotte_yomeyaba` |
| 250 | 2015 | suezou | ＣＨＵ×ペット　体験版 | - | タイトル | `s2015_kaguya_chu_pet` |
| 251 | 2015 | suezou | 巨乳くのいち朱雀　体験版 | NeXAS | タイトル (ディスプレイ設定の確認ダイアログを閉じて) | `s2015_lune_kunoichi_suzaku` |
| 252 | 2015 | suezou | 姫様ＬＯＶＥライフ！web体験版ver1.0 | - | タイトル | `s2015_princess_sugar_himelove` |
| 253 | 2015 | suezou | 根雪の幻影　白花荘の人々　体験版第2弾 | - | タイトル | `s2015_silkys_plus_neyuki` |
| 254 | 2015 | suezou | ピュア×コネクト  Ver1.00 | BGI/Ethornell | タイトル (従来 Wine) | `s2015_smee_pure_connect` |
| 255 | 2015 | suezou | 巨乳ファンタジー３体験版 | - | 起動時の解像度選択 → 確認画面 (ゲーム描画) | `s2015_waffle_kyonyu3` |
| 256 | 2016 | suezou | よめがみ My Sweet Goddess! リコＨシーン体験版 | KiriKiri | タイトル (Wine qasf パッチ: WMV オープニングの無限ループ修正) | `b2016_alcot` |
| 257 | 2016 | suezou | 満淫オーク電車～くっ！　任務じゃなければこんな場所…～　体験版　Ver1.00 | KiriKiri | タイトル | `b2016_apadash` |
| 258 | 2016 | suezou | アナル＊ティーチャー～感度良肛！？　逆レッスン～　体験版　Ver1.00 | KiriKiri | タイトル | `b2016_appetite` |
| 259 | 2016 | suezou | b2016_blackcyc | - | タイトル (待機延長) | `b2016_blackcyc` |
| 260 | 2016 | suezou | 間宮くんちの五つ子事情 体験版 | KiriKiri | タイトル | `b2016_cube` |
| 261 | 2016 | suezou | お嬢様はイイナリみたいです！？ 体験版 | - | タイトル | `b2016_evoll` |
| 262 | 2016 | suezou | リプキス　体験版 | NeXAS | タイトル | `b2016_giga` |
| 263 | 2016 | suezou | はらかつ！ ～気になるあの子と子作りエッチ～ Trial Version: 1.0.0.0 [data ver:1.00] | - | 本編 | `b2016_inter_heart_glossy` |
| 264 | 2016 | suezou | 小悪魔発育チュ～！（体験版） | FVP | タイトル (再試行・Go 版でパッケージ化) | `b2016_kaguya` |
| 265 | 2016 | suezou | b2016_silkys_sakura | - | タイトル (メニュー start.exe ではなくゲーム本体を起動) | `b2016_silkys_sakura` |
| 266 | 2016 | suezou | b2016_waffle | - | タイトル (解像度選択→はい をクリック) | `b2016_waffle` |
| 267 | 2016 | suezou | 生命のスペア | KiriKiri | タイトル (従来 Wine) | `s2016_akabeesoft3_inosupe` |
| 268 | 2016 | suezou | りとる☆ついんず　～すぐにトリコにしてあげる♪～　体験版　Ver1.00 | KiriKiri | タイトル | `s2016_ammolite_little_twins` |
| 269 | 2016 | suezou | 夫に言えない義父との姦淫～嫌なのに忘れられなくなりそう～　体験版　Ver1.00 | KiriKiri | タイトル | `s2016_appetite_husband` |
| 270 | 2016 | suezou | わ、わたしを縛りたい？～僕と地味娘の秘密の関係～　体験版　Ver1.00 | KiriKiri | タイトル | `s2016_appetite_wawatasi` |
| 271 | 2016 | suezou | 恋するお嬢様はエッチな花嫁　体験版 | AdvHD | タイトル | `s2016_ensemble_sweet_hanayome` |
| 272 | 2016 | suezou | 珊海王の円環体験版 | Eushully AGE | 本編 (ロード画面) | `s2016_eushully_sankaio` |
| 273 | 2016 | suezou | AmenitysLife Ver1.00 | BGI/Ethornell | 本編 (ロード画面。従来 Wine) | `s2016_hooksoft_amenitys_life` |
| 274 | 2016 | suezou | アイドルパフパフマネージメント（体験版） | - | タイトル | `s2016_kaguya_idol_pafupafu` |
| 275 | 2016 | suezou | 巨乳大家族催眠「家族みんなでイキまくるセックスがやめられないのぉ」 | NeXAS | タイトル | `s2016_lune_dai_kazoku` |
| 276 | 2016 | suezou | 冥刻学園受胎偏・体験版 | NeXAS | タイトル | `s2016_lune_meikoku` |
| 277 | 2016 | suezou | お嬢様学園こすぷれハーレム部！「今日はどの服でエッチしよっか」 | NeXAS | タイトル | `s2016_marine_cospure_harem` |
| 278 | 2016 | suezou | 女装学園（孕）　体験版 | YU-RIS | タイトル | `s2016_no_strike_jyogaku` |
| 279 | 2017 | suezou | いつまでも俺は母に恋してる！  - 体験版２ - | - | タイトル | `b2017_anim` |
| 280 | 2017 | suezou | 催眠アプリで寝取りハメ～これまでの恨み、ヤり返してやる！　体験版　Ver1.00 | KiriKiri | タイトル | `b2017_appetite` |
| 281 | 2017 | suezou | なちゅらるばけーしょん | - | タイトル (再試行) | `b2017_hibiki_works` |
| 282 | 2017 | suezou | はらかつ２　～子作りエッチでお悩み解決～ Trial Version: 1.0.0.0 [data ver:1.00] | - | 本編テキスト | `b2017_inter_heart_glossy` |
| 283 | 2017 | suezou | ニュートンと林檎の樹　体験版＋ | CatSystem2 | タイトル (Go 版: CatSystem2 インストーラを自動操作) | `b2017_laplacian` |
| 284 | 2017 | suezou | 女装千年王国　体験版 | YU-RIS | タイトル | `b2017_no_strike` |
| 285 | 2017 | suezou | b2017_onomatope_raspberry | ShiinaRio | タイトル (Go 版: ShiinaRio の SETUP.exe を自動インストール) | `b2017_onomatope_raspberry` |
| 286 | 2017 | suezou | ことのは・アムリラート | KiriKiri | タイトル (OP動画再生後) | `b2017_sukerasparo` |
| 287 | 2017 | suezou | 透けちゃって！！～九死に一生を得て濃密セカンドライフ～ Trial Version: 1.0.0.0 [data ver:1.00] | - | 本編テキスト | `b2017_sweet_heart` |
| 288 | 2017 | suezou | あなたに恋する恋愛ルセット　【体験版】 | Softpal | タイトル | `b2017_unisonshift` |
| 289 | 2017 | suezou | b2017_waffle | - | タイトル (解像度選択→確認をクリックで通過) | `b2017_waffle` |
| 290 | 2017 | suezou | 少女と年の差、ふたまわり。 | KiriKiri | タイトル | `s2017_akabeesoft3_tonofuta` |
| 291 | 2017 | suezou | ゆけゆけ悪のモンむす軍団！～怪人娘に搾り取られるえっちで過酷な世界性服大作戦～　体験版　Ver1.00 | KiriKiri | タイトル | `s2017_appetite_evil_monmusu` |
| 292 | 2017 | suezou | 背徳の母娘丼～淫欲に沈むママといもうと～　体験版　Ver1.00 | KiriKiri | タイトル | `s2017_appetite_immorality_don` |
| 293 | 2017 | suezou | 粘液地獄　～巨大ヌルヌル生物に汚された女学生～ 体験版 | KiriKiri | タイトル | `s2017_atelier_gh_neneki` |
| 294 | 2017 | suezou | VenusBlood-BRAVE- ADV体験版 | Unity | 本編 (会話) | `s2017_dualtail_vbb` |
| 295 | 2017 | suezou | 人妻寝取り屋 Trial Version: 1.0.0.0 [data ver:1.00] | - | 本編 (会話) | `s2017_inter_heart_glossy_hitozuma_ntr` |
| 296 | 2017 | suezou | お姉さん×SHUFFLE！　体験版 | - | タイトル | `s2017_kaguya_sister_shuffle` |
| 297 | 2017 | suezou | s2017_marmalade_mashimaro | - | タイトル | `s2017_marmalade_mashimaro` |
| 298 | 2017 | suezou | 春音アリス＊グラム　体験版 | KiriKiri | タイトル | `s2017_nanawind_alice_gram` |
| 299 | 2017 | suezou | 金色ラブリッチェ体験版 | SiglusEngine | 本編 (OP の文章) | `s2017_sagaplanets_kinkoi` |
| 300 | 2017 | suezou | ハーレムゲーム２　普通じゃない主人公がハーレムを築くファンタジー（？） Trial Version: 1.0.0.0 [data ver:1.00] | - | 本編 | `s2017_sweet_heart_hg2` |
| 301 | 2017 | suezou | 俺嫁！？　体験版Ver.2 | LiveMaker | タイトル | `s2017_wendybell_oreyome` |
| 302 | 2018 | suezou | わんこの嫁入り ～ようこそ！いぬのしっぽへ～ 体験版 version 1.00 | - | タイトル | `b2018_alumi_soft` |
| 303 | 2018 | suezou | P.S.あなたを心から愛しています～最期まで気づけなかった妻の苦しみ～　体験版 | KiriKiri | タイトル | `b2018_appetite` |
| 304 | 2018 | suezou | 彼女が異種族だった場合～クールな先輩ダークエルフとの甘やかし同棲日誌～体験版 | YU-RIS | タイトル | `b2018_casket` |
| 305 | 2018 | suezou | 恋はそっと咲く花のように　体験版 | FVP | タイトル (再試行・Go 版でパッケージ化) | `b2018_ensemble` |
| 306 | 2018 | suezou | 爆乳催眠女教師　【体験版】 | Softpal | タイトル | `b2018_erectlip` |
| 307 | 2018 | suezou | 催眠学園１年生体験版 | - | タイトル | `b2018_fudegaki_soft` |
| 308 | 2018 | suezou | 俺の上であがく六人の伽女　体験版２ | AdvWin32/WillPlus | タイトル | `b2018_guilty` |
| 309 | 2018 | suezou | ノットフィクション！ Trial Version: 1.0.0.0 [data ver:1.00] | - | タイトル | `b2018_hachimitsu_soft` |
| 310 | 2018 | suezou | LoveHolic　体験版 | - | タイトル | `b2018_kaguya` |
| 311 | 2018 | suezou | b2018_mirai | - | タイトル (QLIE 起動設定の「起動」をクリック) | `b2018_mirai` |
| 312 | 2018 | suezou | グランギニョルの夜 体験版Ver.01 | - | 本編テキスト (待機延長) | `b2018_tinkerbell` |
| 313 | 2018 | suezou | 僕と彼女のご奉仕同棲　体験版 | AdvWin32/WillPlus | タイトル | `b2018_tokohana` |
| 314 | 2018 | suezou | 欲求不満なボクのママのＳＥＸレッスン～パパには言えない家庭内情事～　Trial | KiriKiri | タイトル | `s2018_appetite_boku_mother` |
| 315 | 2018 | suezou | 触手王子の肉嫁探し～母と姉弟の終わらない絶頂地獄～　体験版 | KiriKiri | タイトル | `s2018_appetite_shokushu_ouji` |
| 316 | 2018 | suezou | VenusBloodLagoonSlgTrial | Unity | タイトル (従来 Wine) | `s2018_dualtail_vbl` |
| 317 | 2018 | suezou | 乙女が結ぶ月夜の煌めき　体験版 | AdvHD | タイトル | `s2018_ensemble_otostuki` |
| 318 | 2018 | suezou | 巨乳エルフ母娘催眠体験版 | NeXAS | タイトル (起動時の表示設定ダイアログの奥にタイトルメニュー) | `s2018_lune_elf_saimin` |
| 319 | 2018 | suezou | 彼女が野球部の性処理マネージャーに…～体育会系セックスで寝取られて～体験版 | KiriKiri | タイトル (「フルスクリーンで表示しますか？」を閉じて) | `s2018_pin_point_yakyurare` |
| 320 | 2018 | suezou | 姫と乙女のヤキモチＬＯＶＥ　web体験版ver1.0 | - | タイトル | `s2018_princess_sugar_himeoto` |
| 321 | 2018 | suezou | バタフライシーカー 体験版 | - | タイトル | `s2018_silkys_plus_butterfly_seeker` |
| 322 | 2018 | suezou | 母娘×シャッフル！ 体験版Ver.2 | - | タイトル | `s2018_tinkerbell_shuffle` |
| 323 | 2018 | suezou | 親友が気づかぬ内に彼女を寝取るボク体験版 | - | 起動時の解像度選択 → 確認 (はい) → 注意書き・ロゴ (ゲーム描画) (従来 Wine) | `s2018_waffle_shinyu` |
| 324 | 2019 | suezou | いつまでも息子のままじゃいられない！５ - 体験版 - | - | タイトル | `b2019_anim` |
| 325 | 2019 | suezou | 女体化パニック！３～お、俺は男だ！　でも、悔しいけど気持ちいいっ……のか！？　体験版 | KiriKiri | タイトル | `b2019_appetite` |
| 326 | 2019 | suezou | アイドル裏営業～汚されたステージ衣装～体験版 | - | タイトル | `b2019_channel_ubai` |
| 327 | 2019 | suezou | VenusBlood FRONTIER International SLG Trial 日本語版 | KiriKiri | タイトル | `b2019_dualtail` |
| 328 | 2019 | suezou | アイキス 豪華体験版 | NeXAS | タイトル | `b2019_giga` |
| 329 | 2019 | suezou | どっちのｉが好きですか？　Ｈシーン体験版　Ver1.00 | BGI | タイトル | `b2019_hooksoft` |
| 330 | 2019 | suezou | お姉さん先生　体験版 | - | タイトル | `b2019_kaguya` |
| 331 | 2019 | suezou | WhiteBlue　web体験版ver2.0 | - | タイトル | `b2019_lilim_darkness` |
| 332 | 2019 | suezou | b2019_marshmallow_soft | - | タイトル (待機延長) | `b2019_marshmallow_soft` |
| 333 | 2019 | suezou | モウソウスピーカー～Ｈな妄想が聴こえてくるよ～　体験版 | KiriKiri | タイトル | `b2019_sator` |
| 334 | 2019 | suezou | b2019_sukerasparo | BGI/Ethornell | タイトル (BGI インストーラ: メニューをクリック) | `b2019_sukerasparo` |
| 335 | 2019 | suezou | おにギュＨ＿ＨＣＢ体験版 | KiriKiri | タイトル | `b2019_tinkle_position` |
| 336 | 2019 | suezou | わんこの嫁入り ～新たな二本のしっぽ～ 体験版 version 1.00 | LiveMaker | タイトル | `s2019_alumi_soft_inuyome2` |
| 337 | 2019 | suezou | メス堕ち！巨乳水着妻達とのラブエロスイミング！ - 体験版 - | - | タイトル (痕跡: enigmaprotector) | `s2019_anim_mesuochi_swimming` |
| 338 | 2019 | suezou | 厨二病が催眠術を使えたら～勘違い転校生のエッチな悪神召喚術～　体験版　Ver1.00 | KiriKiri | タイトル | `s2019_apadash_chuni_saimin` |
| 339 | 2019 | suezou | 兄嫁に誘われて～義姉がエロ過ぎて家庭内不倫～　体験版　Ver1.00 | KiriKiri | タイトル | `s2019_appetite_aniyome_furin` |
| 340 | 2019 | suezou | [2019/05/08 01][mode_title.ks - 164]ママの妹の童貞教育～ひきこもった僕は外出せずに中出しする～　体験版 | KiriKiri | タイトル | `s2019_appetite_dt_kyoiku` |
| 341 | 2019 | suezou | 孕女～精子を欲しがる淫らな教え子～ 体験版 | AdvHD | タイトル | `s2019_guilty_harame` |
| 342 | 2019 | suezou | イイナリ姉色 Trial Version: 1.0.0.0 [data ver:1.00] | - | 本編 (登場人物選択画面) | `s2019_inter_heart_aneiro2` |
| 343 | 2019 | suezou | 風雷戦姫　神夢　体験版 | - | タイトル | `s2019_lilim_shinmu` |
| 344 | 2019 | suezou | 若葉色のカルテット 体験版 | BGI/Ethornell | タイトル (独自インストーラメニュー -click 603,268 で「ゲームのインストール」。従来 Wine) | `s2019_lump_of_sugar_wakabairono_quartet` |
| 345 | 2019 | suezou | 巨乳女戦士・土下座催眠「ちくしょうっ……アタシはお前の思い通りになんか、ならないからな……！」 | NeXAS | タイトル | `s2019_lune_dogeza_saimin` |
| 346 | 2019 | suezou | josou jinjya_trial | YU-RIS | タイトル | `s2019_no_strike_jyosou_jinjya` |
| 347 | 2019 | suezou | 恋嵐スピリッチュ 体験版 | KiriKiri | タイトル ver.1.01 (本体はミラーから削除済みのため、公式ページが案内する DLsite の体験版 VJ012599 に suezou のパッチ 1.01 を当てて) | `s2019_parasol_renran_p` |
| 348 | 2019 | suezou | いつかのメモラージョよりぬき体験版 | KiriKiri | タイトル | `s2019_sukerasparo_itsukano` |
| 349 | 2020 | suezou | 墓多 DYING ZOMBIES -体験版- | KiriKiri | タイトル | `b2020_akabeesoft3` |
| 350 | 2020 | suezou | 優来（ゆら）があいつに抱かれる理由（わけ） 体験版 | KiriKiri | タイトル | `b2020_at_sakura` |
| 351 | 2020 | suezou | アナベル・メイドガーデン体験版 | QLIE | タイトル (ログに例外あり) | `b2020_barista_lab` |
| 352 | 2020 | suezou | はじカノ -君がいる部屋- 体験版 | - | タイトル | `b2020_caramel_box_strawberry` |
| 353 | 2020 | suezou | 悪堕ち 魔導処女～絶対支配の触手調教～　体験版 | KiriKiri | タイトル | `b2020_chaos_r` |
| 354 | 2020 | suezou | 爆乳淫辱霊媒師　【体験版】 | Softpal | タイトル | `b2020_erectlip` |
| 355 | 2020 | suezou | アイシング-love coating- 体験版 Version: 1.0.0.1 [data ver:1.00] | Unity | タイトル | `b2020_hachimitsu_soft` |
| 356 | 2020 | suezou | リトルシックガール　～桃蜜は妹の香り～　体験版 | KiriKiri | タイトル | `b2020_lass_pixy` |
| 357 | 2020 | suezou | b2020_marshmallow_soft | - | タイトル (待機延長) | `b2020_marshmallow_soft` |
| 358 | 2020 | suezou | shin-neko ～再会した妹との新たな関係～　【体験版】 | Softpal | タイトル | `b2020_milimili` |
| 359 | 2020 | suezou | 世話焼き奥さんで人の頼みを断れない亜衣さんにお願いして中出しハメ放題のドスケベ妻になってもらった 体験版 | KiriKiri | タイトル (再試行: 待機延長) | `b2020_pin_point` |
| 360 | 2020 | suezou | ティーリオ傭兵団の初仕事 体験版 v1.2.0 | - | 本編 (SRPG) | `b2020_rikuzendo` |
| 361 | 2020 | suezou | b2020_shiba_soft | - | タイトル (待機延長) | `b2020_shiba_soft` |
| 362 | 2020 | suezou | b2020_sukerasomero | BGI/Ethornell | タイトル (BGI インストーラ: -click でメニュー→開始) | `b2020_sukerasomero` |
| 363 | 2020 | suezou | 緊縛性奴会長 ～カレに知られたワタシの秘密～　【体験版】 | Softpal | タイトル | `s2020_amuse_craft_erotica_kinbaku` |
| 364 | 2020 | suezou | 恋人スワッピング 体験版 | KiriKiri | タイトル | `s2020_at_sakura_lover_swapping` |
| 365 | 2020 | suezou | あやかし遊郭～花魁アマビエ民譚集～体験版 | YU-RIS | タイトル (従来 Wine) | `s2020_casket_ayakashi03` |
| 366 | 2020 | suezou | 天冥のコンキスタ体験版 | Eushully AGE | タイトル | `s2020_eushully_tenmei` |
| 367 | 2020 | suezou | 放課後シンデレラ 体験版 Ver1.00 | BGI/Ethornell | タイトル (従来 Wine。ログにロック待ちの警告) | `s2020_hooksoft_houkago` |
| 368 | 2020 | suezou | 風雷戦姫　神夢　エクストラシナリオ　web体験版ver1.0 | - | タイトル | `s2020_lilim_lilim_pb2` |
| 369 | 2020 | suezou | ボクのカノジョはえっちな先生　体験版 | KiriKiri | タイトル | `s2020_monoceros_plus_kanosen` |
| 370 | 2020 | suezou | 最高に都合のいいパイズリ上手のマリーさん－体験版－ | KiriKiri | タイトル | `s2020_pajamas_ex_paizuri` |
| 371 | 2020 | suezou | 姫と乙女のヤキモチＬＯＶＥ-きらめき夏物語！-　web体験版ver2.0 | - | タイトル | `s2020_princess_sugar_himeoto_fd` |
| 372 | 2020 | suezou | かけぬけ★青春スパーキング！ 体験版 | KiriKiri | タイトル (WM なしでは右下が切れていた → openbox で全体表示) | `s2020_sagaplanets_kakenuke` |
| 373 | 2020 | suezou | まおかつ！　体験版 - Ver1.1 | Artemis | タイトル | `s2020_wonder_fool_maokatsu` |
| 374 | 2021 | suezou | 愛する彼女は画面の中、他の男の上で腰をふる 体験版 | KiriKiri | タイトル | `b2021_at_sakura` |
| 375 | 2021 | suezou | b2021_bishop | - | タイトル (ランチャー startup.exe から起動) | `b2021_bishop` |
| 376 | 2021 | suezou | 神聖昂燐ダクリュオン～正しい天使の育てかた～　体験版 | KiriKiri | タイトル | `b2021_chaos_r` |
| 377 | 2021 | suezou | b2021_dessert_soft | - | タイトル (再試行・パッチ済み Wine) | `b2021_dessert_soft` |
| 378 | 2021 | suezou | アイドル姉妹をエッチにプロデュース！ 体験版 | KiriKiri | タイトル | `b2021_kelp` |
| 379 | 2021 | suezou | 魔法少女消耗戦線DEADΩAEGIS_体験版 | - | タイトル | `b2021_metalogiq` |
| 380 | 2021 | suezou | ちんかもツインズ２！　～夏だ！海だ！ふたごの妹おっぱい対決だ！～　【体験版】 | Softpal | タイトル | `b2021_milimili` |
| 381 | 2021 | suezou | 売国王姫～堕落のメス豚母娘～体験版 | KiriKiri | タイトル (再試行) | `b2021_pin_point` |
| 382 | 2021 | suezou | プリンセス☆シスターズ！～四姉妹は全員あなたの許嫁～　web体験版ver1.0 | - | タイトル | `b2021_princess_sugar` |
| 383 | 2021 | suezou | 人妻LovelyMoment | - | タイトル | `b2021_stick` |
| 384 | 2021 | suezou | 淫妖蟲 穢 ～凌蝕桃源退魔録～体験版 Version01 | - | タイトル | `b2021_tinkerbell` |
| 385 | 2021 | suezou | けもの道☆ガーリッシュスクエア　体験版 | YU-RIS | タイトル | `b2021_whirlpool` |
| 386 | 2021 | suezou | ROLEPLAYER：小粥姉妹の粘膜ポトレ ぐりぐちゃLIVE！ | KiriKiri | タイトル | `s2021_akabeesoft2_roleplayer` |
| 387 | 2021 | suezou | いつまでも息子のママじゃいられない！２ - 体験版 - | - | タイトル (痕跡: enigmaprotector) | `s2021_anim_itumama2` |
| 388 | 2021 | suezou | 親友に子種を注がれる俺の妻 体験版 | KiriKiri | タイトル | `s2021_at_sakura_kodane` |
| 389 | 2021 | suezou | 愛する妻と１０人の過去男 体験版 | KiriKiri | タイトル | `s2021_at_sakura_tuma_10` |
| 390 | 2021 | suezou | リンカーベル・アカデミア～落第魔女とヒミツの儀式～体験版 | YU-RIS | タイトル | `s2021_casket_bellaca` |
| 391 | 2021 | suezou | リンカーベル・アカデミア～龍族のフィアンセ～Hシーン体験版 | YU-RIS | タイトル | `s2021_casket_bellaca02` |
| 392 | 2021 | suezou | VenusBlood HOLLOW International SRPG Trial ENG Ver. | KiriKiri | タイトル | `s2021_dualtail_vbhi` |
| 393 | 2021 | suezou | フユキス　体験版 - Ver1.0.0 | Artemis | タイトル (従来 Wine) | `s2021_giga_fuyukiss` |
| 394 | 2021 | suezou | 花鐘カナデ＊グラム Chapter1 -小桜結- 体験版 | KiriKiri | タイトル (startup.exe はインストーラを起動するメニューなので、ゲーム本体 kanade_c1trial.exe を直接起動。手動で確認) | `s2021_nanawind_kanade_gram_ch1` |
| 395 | 2021 | suezou | Jyosou Souseiki Trial Ver. | KiriKiri | タイトル | `s2021_no_strike_Trap_Genesis` |
| 396 | 2021 | suezou | ふゆから、くるる。 体験版 | - | タイトル | `s2021_silkys_plus_fuyukuru` |
| 397 | 2021 | suezou | s2021_smee_pureconnect_hd | BGI/Ethornell | タイトル (従来 Wine。run.sh が時間切れで判定は no_run) | `s2021_smee_pureconnect_hd` |
| 398 | 2022 | suezou | 黒ギャル子ちゃんとしっぽり温泉旅行 ～Hしまくり三泊四日の旅～　【体験版】 | Softpal | タイトル | `b2022_amuse_craft_erotica` |
| 399 | 2022 | suezou | 愛妻Ｘ交姦妊活スワッピング - 体験版 - | - | タイトル | `b2022_anim` |
| 400 | 2022 | suezou | 愛する恋人を大嫌いな旧友に寝取られた件 体験版 | KiriKiri | タイトル | `b2022_at_sakura` |
| 401 | 2022 | suezou | BstTrial | Unity | タイトル | `b2022_ba_ku` |
| 402 | 2022 | suezou | あやかし遊郭～花魁道中～体験版 | YU-RIS | タイトル | `b2022_casket` |
| 403 | 2022 | suezou | 進軍ゴブリン軍団～人類総苗床化作戦～ 体験版 | KiriKiri | タイトル | `b2022_chaos_r` |
| 404 | 2022 | suezou | 華は短し、踊れよ乙女 - 体験版 | AdvWin32/WillPlus | タイトル | `b2022_ensemble` |
| 405 | 2022 | suezou | 南国プリズン体験版 | - | タイトル | `b2022_evoll` |
| 406 | 2022 | suezou | リンパにATATA! ～メス牡蠣ミルクどぴゅらっしゅ♡～ | KiriKiri | タイトル | `b2022_hending` |
| 407 | 2022 | suezou | 羞恥隷嬢学園 | - | タイトル | `b2022_lunaprism` |
| 408 | 2022 | suezou | 触災の島～Island of the Dead～体験版　Ver1.00 | KiriKiri | タイトル | `b2022_syoku` |
| 409 | 2022 | suezou | ROLEPLAYER：とろろ姉妹の粘膜ポトレ ぐりぐちゃLIVE！ | KiriKiri | タイトル | `s2022_akabeesoft2_roleplayer_tororo` |
| 410 | 2022 | suezou | つまもみ ～欲求不満の奥さんとなし崩しオイルマッサージ～　【体験版】 | Softpal | タイトル | `s2022_amuse_craft_erotica_tsumamomi` |
| 411 | 2022 | suezou | 今夜もあいつに抱かれる彼女 体験版 | KiriKiri | タイトル | `s2022_at_sakura_konya_aitu` |
| 412 | 2022 | suezou | ネトラセ妻、美衣奈 体験版 | KiriKiri | タイトル | `s2022_at_sakura_ntr_tuma_miina` |
| 413 | 2022 | suezou | 恥辱の制服2 体験版 | - | タイトル (起動ランチャー経由。ランチャーが終了するので判定は dead) | `s2022_bishop_cs2` |
| 414 | 2022 | suezou | 牝教師3 IE 体験版 | - | タイトル (起動ランチャー経由。ランチャーが終了するので判定は dead) | `s2022_bishop_mk3ie` |
| 415 | 2022 | suezou | 乙女とふれあう、ひとつ屋根の下 TRIAL EDITION | AdvHD | タイトル | `s2022_ensemble_sweet_otoshita` |
| 416 | 2022 | suezou | 放課後シンデレラ２ 体験版 Ver1.01 | BGI/Ethornell | 本編 (Start → 体験版メニュー。ログにロック待ちの警告が出るが操作できる) | `s2022_hooksoft_houkago2` |
| 417 | 2022 | suezou | スタディ§ステディ２体験版 | EntisGLS | タイトル | `s2022_marmalade_studysteady2` |
| 418 | 2022 | suezou | 燦然可憐エクセルシフォン 体験版 | KiriKiri | タイトル | `s2022_mille_feuille_excelchiffon` |
| 419 | 2022 | suezou | 勇者を引退してよろず屋をオープンしたけど、なぜかアダルトグッズに人気が集中している件。体験版 | ShiinaRio | タイトル (kernelbase 空き容量パッチ) | `s2022_onomatope_raspberry_yorozuya` |
| 420 | 2023 | suezou | 最強の人妻ヒロインが寝取られるまで～お願い！夫にはバラさないで…～　体験版　Ver1.00 | KiriKiri | タイトル | `b2023_apadash` |
| 421 | 2023 | suezou | 洗脳航路～エイリアンの苗床に導かれるエリート女軍人たち～　体験版　Ver1.00 | KiriKiri | タイトル | `b2023_appetite` |
| 422 | 2023 | suezou | 堕とされた義姉 体験版 | KiriKiri | タイトル | `b2023_at_sakura` |
| 423 | 2023 | suezou | 神聖昂燐エストランジェ体験版 | KiriKiri | タイトル | `b2023_chaos_r` |
| 424 | 2023 | suezou | b2023_cyc_rose | - | タイトル (Go 版: 自己解凍を C: に置いて展開) | `b2023_cyc_rose` |
| 425 | 2023 | suezou | 爆乳遊廓 ～癒され太夫孕ませの夜鶴～　【体験版】 | Softpal | タイトル | `b2023_erectlip` |
| 426 | 2023 | suezou | 夜勤病棟 リメイク 体験版 | - | タイトル | `b2023_fg_remake` |
| 427 | 2023 | suezou | 瑠璃桜 | KiriKiri | タイトル | `b2023_lump_of_sugar_all` |
| 428 | 2023 | suezou | 非道狩～ヒトカリ～ 体験版 | Softpal | タイトル | `b2023_turumiku_another` |
| 429 | 2023 | suezou | モラトリアム 体験版 | - | タイトル | `b2023_wendybell` |
| 430 | 2023 | DLsite | 画面から出てきた推しVtuberとHな同棲生活 ～夢魔かわASMR配信者は地雷系？～ | YU-RIS | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01000407` |
| 431 | 2023 | DLsite | 【DLsite限定版】進軍ゴブリン軍団2 ～淫獣軍VS.INTRUDER～ | KiriKiri | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01000429` |
| 432 | 2023 | DLsite | 【DLsite限定版】せをはやみ。 | - | タイトル (XAML Islands 代用品 xamlstub で起動) | `dl_VJ01000456` |
| 433 | 2023 | DLsite | プリマドール 冬空花火／雪華文様 | SiglusEngine | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01000468` |
| 434 | 2023 | DLsite | 【DLsite限定版】性感エステ 灯 -AKARI- マキ（28） | KiriKiri | タイトル | `dl_VJ01000497` |
| 435 | 2023 | DLsite | 堕とされていくお姉ちゃんと、何もできないボク ～憧れの彼女は、淫落し痴態をさらけ出す～ | KiriKiri | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01000502` |
| 436 | 2023 | DLsite | VenusBlood GAIA International R-18版 | KiriKiri | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01000564` |
| 437 | 2023 | DLsite | 魔法戦士 FINAL IGNITION ～永遠の女神たち～ | - | タイトル (製品版 DRM: シリアル番号) | `dl_VJ01000625` |
| 438 | 2023 | DLsite | ヒラヒラヒヒル | Unity | タイトル以降 (言語選択→ロード画面) (製品版 DRM: PlayDRM) | `dl_VJ01000628` |
| 439 | 2023 | DLsite | 【デラックス版】調教カテイ ～性開発された肢体は元カレを忘れられない～ | KiriKiri | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01000639` |
| 440 | 2023 | DLsite | 【通常版】みことのり！ ～妹巫女と中出し除霊～ | SiglusEngine | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01000672` |
| 441 | 2023 | DLsite | 【限定版】みことのり！ スペシャルパック | SiglusEngine | タイトル | `dl_VJ01000673` |
| 442 | 2023 | DLsite | 居座りカノジョとのドキドキ同居生活 ～ねぇねぇ、おじさんしたいでしょ♪～ | KiriKiri | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01000679` |
| 443 | 2023 | DLsite | 神楽黎明記 ～紫の章～ 参 | - | タイトル | `dl_VJ01000787` |
| 444 | 2023 | DLsite | ALPHA-NIGHTHAWK | - | タイトル (Liar のインストーラを clickbtn で完了、起動対象を本体 exe に。手動で確認) (製品版 DRM: PlayDRM) | `dl_VJ01000803` |
| 445 | 2023 | DLsite | エッチなお姉ちゃんが、褐色巨乳の悪魔だった件 | NW.js/Tyrano | タイトル (製品版 DRM: ソフト電池) | `dl_VJ01000810` |
| 446 | 2023 | DLsite | 僕がいない間に変貌えられた妻の秘肉 ～ラブラブ新婚妻は他の男に抱かれ淫らに喘ぐ夢を見るか～ | - | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01000904` |
| 447 | 2023 | DLsite | 【多言語版】イイナリ妻色 ～止まらない腰使い～ | - | 本編 (登場人物選択画面) (製品版 DRM: PlayDRM) | `dl_VJ01000948` |
| 448 | 2023 | DLsite | 最強の人妻ヒロインが寝取られるまで ～お願い！夫にはバラさないで…～ | KiriKiri | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01001109` |
| 449 | 2023 | DLsite | 光装剣姫アークブレイバー 魔族篇胞 | - | タイトル (製品版 DRM: シリアル番号) | `dl_VJ01001323` |
| 450 | 2023 | suezou | あねつまみ２ ～エッチなお姉さん人妻にたっぷり甘えるめちゃシコLoveLife～  - 体験版 - | - | タイトル (痕跡: enigmaprotector) | `s2023_anim_anetumami2` |
| 451 | 2023 | suezou | 僕がいない間に変貌えられた妻の秘肉 - 体験版 - | - | タイトル | `s2023_anim_bokukae` |
| 452 | 2023 | suezou | 画面から出てきた推しVtuberとHな同棲生活 ～庶民派吸血鬼お嬢様は契約したい～体験版 | YU-RIS | タイトル | `s2023_casket_oshitona_vtuber02` |
| 453 | 2023 | suezou | 画面から出てきた推しVtuberとHな同棲生活 ～夢魔かわASMR配信者は地雷系？～体験版 | YU-RIS | タイトル | `s2023_casket_oshitona_vtuber03` |
| 454 | 2023 | suezou | 魔子宮聖女ビアンカ∽ノエイン～終焉なき破瓜と悦楽の煉獄～　体験版 | KiriKiri | タイトル | `s2023_chaos_r_bian_rr` |
| 455 | 2023 | suezou | 異世界行ってマッサージで生き残ろうとしたら美脚美女とイチャ×2ライフ～転移ボーナスが美味しすぎた件～体験版 | KiriKiri | タイトル | `s2023_cross_over_isekai_ma` |
| 456 | 2023 | suezou | s2023_hadashi_harem_shangrila | - | タイトル | `s2023_hadashi_harem_shangrila` |
| 457 | 2023 | suezou | ソープランドのはっしゃくさま | KiriKiri | タイトル | `s2023_hending_hassyakusama` |
| 458 | 2023 | suezou | リアルエロゲシチュエーション！DT　体験版 - Ver1.0.0 | Artemis | タイトル | `s2023_j_studio_reaero_dt` |
| 459 | 2023 | suezou | 巨乳オナホ妖怪と田舎ライフもHも満喫生活　【体験版】 | Softpal | タイトル | `s2023_milimili_onaho_yokai` |
| 460 | 2023 | suezou | リトルプリンセスＧＯ！ 体験版 | BGI/Ethornell | タイトル (独自インストーラメニュー: vnbox add -click 135,503 で「ゲームのインストール」。手動で確認) | `s2023_quince_soft_little_proncess_go` |
| 461 | 2024 | suezou | 性調教済だった愛する彼女 体験版 | KiriKiri | タイトル | `b2024_at_sakura` |
| 462 | 2024 | suezou | ちょっとナマイキなトイレの花子さん ～えっちぃ学園のおばけとエロエロ搾精性活～ 【体験版】 | Softpal | 本編テキスト | `b2024_milimili` |
| 463 | 2024 | suezou | VenusBlood RAGNAROK International_EN AdvTrial | Unity | 本編 (ゲーム開始後のマップ) | `b2024_ninetail` |
| 464 | 2024 | suezou | 狙われた優等生　身代わりの代償　体験版 | EntisGLS | タイトル | `b2024_syrup_mm` |
| 465 | 2024 | suezou | ムジナ 体験版Ver02 | - | タイトル (kernelbase + ntdll パッチ) | `b2024_tinkerbell` |
| 466 | 2024 | DLsite | 【限定版】エルフェンキング | YU-RIS | タイトル | `dl_VJ01000315` |
| 467 | 2024 | DLsite | いつまでもママといっしょ！ ～誘惑ママのおっぱいに甘えていっぱい射精したい！～ | - | タイトル (従来 Wine, 製品版 DRM: PlayDRM, 痕跡: enigmaprotector) | `dl_VJ01001032` |
| 468 | 2024 | DLsite | 新宿葬命 | - | 起動時の言語選択画面 (ゲーム描画) (製品版 DRM: PlayDRM) | `dl_VJ01001186` |
| 469 | 2024 | DLsite | GEARS of DRAGOON 3 ～竜刻のレガリア～ | Unity | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01001190` |
| 470 | 2024 | DLsite | エッチなお姉ちゃんが、褐色巨乳の悪魔だった件2 ～新たな悪魔ミリス登場♪～ | NW.js/Tyrano | タイトル (製品版 DRM: ソフト電池) | `dl_VJ01001309` |
| 471 | 2024 | DLsite | 【豪華版】ボクが当主になるために今日からメイドとHします2 | YU-RIS | タイトル (従来 Wine, 製品版 DRM: PlayDRM) | `dl_VJ01001385` |
| 472 | 2024 | DLsite | 【通常版】ボクが当主になるために今日からメイドとHします2 | YU-RIS | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01001386` |
| 473 | 2024 | DLsite | 快楽セックスで堕とされていた愛する妻 ～他の男の手で身も心もイキ狂っていく最愛妻 | KiriKiri | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01002065` |
| 474 | 2024 | DLsite | プリマドール 無名典礼 | SiglusEngine | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01002167` |
| 475 | 2024 | DLsite | Bullet Butlers | - | 本編 (冒頭の文章) (製品版 DRM: PlayDRM) | `dl_VJ01002202` |
| 476 | 2024 | DLsite | 竜翼のメロディア −Diva with the blessed dragonol− | YU-RIS | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01002327` |
| 477 | 2024 | DLsite | いな☆こい！ ～お稲荷さまとモテモテのたたり～ | YU-RIS | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01002338` |
| 478 | 2024 | DLsite | 僕の好きな人の好きな人は、女装した僕でした。 | AdvHD | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01002397` |
| 479 | 2024 | DLsite | 異世界娘と秘密のコンカフェえっち | - | タイトル | `dl_VJ01002468` |
| 480 | 2024 | DLsite | 【DLsite限定版】蛟の巫女 | - | タイトル | `dl_VJ01002476` |
| 481 | 2024 | DLsite | 妹Friend！ | - | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01002553` |
| 482 | 2024 | DLsite | 悠刻のファムファタル | Escude | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01002637` |
| 483 | 2024 | DLsite | 爆乳メイド調教 ～夜姦緊縛教育～ | Softpal | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01002655` |
| 484 | 2024 | DLsite | 【限定版】巨乳軍人支配催眠「私の任務は貴方にご奉仕する事です。色んな穴をご自由にお使い下さい」 | YU-RIS | タイトル | `dl_VJ01002863` |
| 485 | 2024 | DLsite | 【DLsite限定版】俺（♂）はメイド（♀）にさせられて、可憐なキューピッドに弄ばれる | - | タイトル | `dl_VJ01002980` |
| 486 | 2024 | DLsite | 幸乃下蛍の恋青日和 ～お義姉ちゃんとの秘密同棲～ | AdvHD | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01003020` |
| 487 | 2024 | DLsite | 夏色果実 ～えっちなカノジョと溶け合う心～ | KiriKiri | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01003070` |
| 488 | 2024 | DLsite | 淫紋闘技都市ソドム ～大観衆の前で強制受胎させられる聖戦姫～ | SiglusEngine | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01003134` |
| 489 | 2024 | DLsite | 【DLsite限定版】NEW BREEDER ～エッチな獣娘を狩って⾃分好みに調教！～ | KiriKiri | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01003347` |
| 490 | 2024 | DLsite | 変態エルフ姉妹と真面目オーク | - | 起動時の解像度選択 → 確認画面 (ゲーム描画) (製品版 DRM: PlayDRM) | `dl_VJ012484` |
| 491 | 2024 | DLsite | 隣人に壊されていく俺の妻 破格版 | - | 起動時の解像度選択 → ロゴ → 注意書き (ゲーム描画) (製品版 DRM: PlayDRM) | `dl_VJ013025` |
| 492 | 2024 | DLsite | 恋するあまいろホームステイ -留学生はわんこ系幼馴染- | AdvHD | タイトル (製品版 DRM: PlayDRM) | `dl_VJ015399` |
| 493 | 2024 | suezou | お姉ちゃんのあまエロレッスン ～世話焼き巨乳お姉ちゃん先生にたっぷり甘えるめちゃエロすくーるライフ～  - 体験版 - | - | タイトル (痕跡: enigmaprotector) | `s2024_anim_ane_amalesson` |
| 494 | 2024 | suezou | 友達ん棒で淫らな汁を溢れさせる母の熟れた蜜穴 - 体験版 - | - | タイトル (痕跡: enigmaprotector) | `s2024_anim_tomohaha` |
| 495 | 2024 | suezou | 淫欲の通学電車～嫌なのにされたい私は堕ちていく～　体験版 | KiriKiri | タイトル | `s2024_appetite_inyoku_train` |
| 496 | 2024 | suezou | 異世界チョロインは、いとも簡単に堕とされる～あれ、ボクの彼女…寝取られちゃいました？～　体験版 | KiriKiri | タイトル | `s2024_appetite_isekai_cyoroin` |
| 497 | 2024 | suezou | 家出少女・高邑緋雪の秘蜜 体験版 | KiriKiri | タイトル | `s2024_at_sakura_iede_takamura` |
| 498 | 2024 | suezou | トメフレＷ体験版 Trial Version: 1.0.2.0 [data ver:1.00] | - | タイトル | `s2024_dessert_soft_tomefure_w` |
| 499 | 2024 | suezou | 百千の定にかわたれし剋 体験版 | Eushully AGE | タイトル (InstallShield 2010+ の ISSetupStream から MSI を取り出して導入) | `s2024_eushully_hyakusen` |
| 500 | 2024 | suezou | セレクトオブリージュ　体験版 - Ver1.0.0 | Artemis | タイトル | `s2024_madosoft_selectoblige` |
| 501 | 2024 | suezou | GEARS of DRAGOON 3 RPGTrial | Unity | タイトル | `s2024_ninetail_gears_of_dragoon3` |
| 502 | 2024 | suezou | Jyosou Yuribatake Trial | KiriKiri | タイトル (言語選択で日本語をクリック) | `s2024_no_strike_yuribatake` |
| 503 | 2025 | suezou | お姉ちゃんのあまエロレッスン２ ～お隣の巨乳お姉ちゃんにイッパイ甘やかされるめちゃエロライフ～  - 体験版 - | FVP | タイトル (再試行・Go 版でパッケージ化) | `b2025_anim` |
| 504 | 2025 | suezou | 学生会長・紫藤喜那の淫鬱 体験版 | KiriKiri | タイトル | `b2025_at_sakura` |
| 505 | 2025 | suezou | のーぶるバトラー TRIAL EDITION | AdvWin32/WillPlus | タイトル | `b2025_ensemble_sweet` |
| 506 | 2025 | suezou | 爆乳淫乱ビーチ　体験版 | Softpal | タイトル | `b2025_erectlip` |
| 507 | 2025 | suezou | 虜ノ焔 ～淫欲の炎に満ちた地下闘技場～ 体験版 | FVP | タイトル (再試行: 待機延長) | `b2025_guilty` |
| 508 | 2025 | suezou | Vanilla Android　シコ猿DT大学生の俺と美少女アンドロイドがいろんなプレイでハメパコミッション　体験版 | KiriKiri | タイトル | `b2025_monoceros_plus` |
| 509 | 2025 | suezou | 小日向柚月と初夜したいっ！　【体験版】 | Softpal | タイトル | `b2025_unisonshift` |
| 510 | 2025 | DLsite | のーぶる・バトラー | AdvHD | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01004020` |
| 511 | 2025 | DLsite | 俺の電脳SEX性活 ‐ビッチな君となんちゃって恋愛‐ | - | タイトル | `dl_VJ01004064` |
| 512 | 2025 | DLsite | 早乙女結衣の淫らな隠しごと ～隠されたアイドルのビッチな裏事情～ | KiriKiri | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01004136` |
| 513 | 2025 | DLsite | 顔のない月 -待宵の双椿- | BGI/Ethornell | タイトル (配布物の BGI インストーラ NSM_MYST_trial_Install.exe を自前描画メニューのクリックで実行。手動で確認) (製品版 DRM: PlayDRM) | `dl_VJ01004381` |
| 514 | 2025 | DLsite | 爆乳淫乱ビーチ | Softpal | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01004424` |
| 515 | 2025 | DLsite | 【豪華版】家族は（俺以外）サキュバスでした ～二人の姉と母との性活で精液を搾り取られます～ | YU-RIS | タイトル | `dl_VJ01004582` |
| 516 | 2025 | DLsite | 女王社長の僕争奪戦 ～カラダもリードも譲らない！ で、どっちのが好きなのっ！？～ | KiriKiri | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01004598` |
| 517 | 2025 | DLsite | 神楽新風記 ～護の章～ | - | タイトル | `dl_VJ01004765` |
| 518 | 2025 | DLsite | 俺だけに傅く孕メイドたち ～精子を貪る五人の欲心女～ | AdvHD | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01004836` |
| 519 | 2025 | DLsite | 【限定版】魔法少女コノカ －異種触手・無限煉獄－ | SiglusEngine | タイトル | `dl_VJ01005112` |
| 520 | 2025 | DLsite | 【限定版】魔法少女アリサ ～異種触手・無限アクメ受胎～ | SiglusEngine | タイトル | `dl_VJ01005116` |
| 521 | 2025 | DLsite | ママ×カノEX ～領主貴族に嫁ぎたくない娘の為に、お母さんがエッチな手ほどきいたします～ | - | タイトル (従来 Wine, 製品版 DRM: PlayDRM) | `dl_VJ01005173` |
| 522 | 2025 | DLsite | 仄暗き時の果てより | - | タイトル (製品版 DRM: PlayDRM) | `dl_VJ010622` |
| 523 | 2025 | suezou | Role player：いくら姉妹の粘膜ポトレ　ぐりぐちゃLIVE！　体験版 | KiriKiri | タイトル | `s2025_akabeesoft2_roleplayer_ikura` |
| 524 | 2025 | suezou | 淫獄の放課後2 体験版 | - | タイトル (BISHOP 起動ランチャーの「ゲーム開始」で設定を書いてから本体が起動) | `s2025_bishop_ingk_houkago2` |
| 525 | 2025 | suezou | s2025_bishop_rs | - | タイトル (BISHOP の起動ランチャーが「ゲーム開始」で落ちるので、startup.ini の demo.exe を直接起動。手動で確認) | `s2025_bishop_rs` |
| 526 | 2025 | suezou | CRACK≡TRICK! | Softpal | タイトル | `s2025_crystalia_crack_trick` |
| 527 | 2025 | suezou | VenusBloodVALKYRIE_SLGTrial | Unity | タイトル | `s2025_dualtail_vbv` |
| 528 | 2025 | suezou | 廃村少女［弐］　～陰り誘う秘姫の匣～　体験版 | Escude | タイトル | `s2025_escude_haison2` |
| 529 | 2025 | suezou | s2025_j_studio_galsfiction | - | タイトル | `s2025_j_studio_galsfiction` |
| 530 | 2025 | suezou | PRIDE　BLUE　web体験版ver2.0 | - | タイトル | `s2025_lilim_darkness_pride_blue` |
| 531 | 2025 | suezou | バカップル・サプリメント体験版 | EntisGLS | タイトル | `s2025_marmalade_bakaple` |
| 532 | 2025 | suezou | ちょっとナマイキなトイレの花子さん ～えっちぃ学園のおばけとエロエロ搾精性活～ 【体験版】 | NeXAS | 本編 (OP) | `s2025_milimili_hanako` |
| 533 | 2025 | suezou | 最高に都合の良いパイズリ上手のマリーさん - MOTION EDITION - 体験版 | KiriKiri | タイトル | `s2025_poison_extasy_paizuri_me` |
| 534 | 2025 | suezou | 狙われた女神天使エンゼルティアーFHD版（体験版） | EntisGLS | タイトル | `s2025_syrup_mm_angel_tear_fhd` |
| 535 | 2026 | suezou | ママラブ！２～俺の彼女は俺のママ☆世話焼き巨乳凛桜ママ～ - 体験版 - | - | タイトル | `b2026_anim` |
| 536 | 2026 | suezou | ディメンション凸ラバース!! 【体験版】 | Softpal | タイトル | `b2026_crystalia` |
| 537 | 2026 | suezou | どいなかガールズゆーとぴあ Trial Version: 1.0.0.0 [data ver:1.00] | - | タイトル | `b2026_dessert_soft` |
| 538 | 2026 | suezou | Triptych ～トリプティック～ 体験版 | - | タイトル (待機延長) | `b2026_tinkerbell` |
| 539 | 2026 | suezou | OfficeLove -クールな上司と秘密のイキ抜き- Trial Edition | AdvWin32/WillPlus | タイトル | `b2026_tokohana` |
| 540 | 2026 | suezou | 友恵の探偵物語～のぞき魔な私の天職～体験版 | - | タイトル | `b2026_wendybell` |
| 541 | 2026 | DLsite 同人 | まぐわれ異世界珍道中 | NW.js/Tyrano | タイトル | `dl_RJ01529614` |
| 542 | 2026 | DLsite 同人 | 借金バニー～カジノで負けて肉体返済～ | Unity | タイトル | `dl_RJ01582557` |
| 543 | 2026 | DLsite 同人 | 精霊商店アルワール | - | タイトル | `dl_RJ01598948` |
| 544 | 2026 | DLsite 同人 | HEART OVERLOAD! 欲望でつながる戦術部隊 | NW.js/Tyrano | タイトル (従来 Wine。描画領域 1280x750 に対し X 側 1280x720) | `dl_RJ01604592` |
| 545 | 2026 | DLsite 同人 | ideal -eve's room- | Unity | 起動時の音量設定画面 (ゲーム描画) (従来 Wine) | `dl_RJ01610703` |
| 546 | 2026 | DLsite 同人 | 雪音の仇討ち物語 | NW.js/Tyrano | タイトル (ウィンドウ名が空のため画面全体のスクショで確認) | `dl_RJ01612664` |
| 547 | 2026 | DLsite 同人 | アリナと淫魔の呪い | NW.js/Tyrano | タイトル | `dl_RJ01618221` |
| 548 | 2026 | DLsite 同人 | スペースパトロール パニカ | NW.js/Tyrano | タイトル | `dl_RJ01618747` |
| 549 | 2026 | DLsite 同人 | まもの博物館 | NW.js/Tyrano | タイトル | `dl_RJ01623688` |
| 550 | 2026 | DLsite 同人 | 【Live2Dおさわりゲーム】ギルド受付のバニーさんはノルマに悩んでいる | Unity | タイトル | `dl_RJ01636335` |
| 551 | 2026 | DLsite 同人 | ～孕ませアトリエ～ 見習い錬金術師のボクと…ドスケベ爆乳女錬金術師×3による…秘密の子作り孕ませアトリエ♪ | - | タイトル | `dl_RJ01641249` |
| 552 | 2026 | DLsite 同人 | 戦闘補習3～武闘派冒険者、魔術を学ぶ～ | NW.js/Tyrano | タイトル | `dl_RJ01642779` |
| 553 | 2026 | DLsite 同人 | 測り難きは下心 | NW.js/Tyrano | 本編 (マップ) | `dl_RJ01648691` |
| 554 | 2026 | DLsite 同人 | どこでもHな世界～地味子のバイト日記～ | NW.js/Tyrano | タイトル (zip のファイル名が GBK: 文字コードを自動判定して展開。手動で確認) | `dl_RJ01650072` |
| 555 | 2026 | DLsite 同人 | エリア404 | - | タイトル (試験操作の Enter で OFF=終了が選ばれて終了) | `dl_RJ01655345` |
| 556 | 2026 | DLsite 同人 | DIVINE BREAKER | Unity | 本編 (キャラクター選択) | `dl_RJ01659367` |
| 557 | 2026 | DLsite 同人 | 都合の良いショタにされた俺と、逃がしてくれないおねえさん【アーリーアクセス版】 | Unity | 起動直後の更新履歴画面 (ゲーム描画) | `dl_RJ01660695` |
| 558 | 2026 | DLsite 同人 | あの娘もうんこしたい! for 水着×爆乳 | LiveMaker | タイトル | `dl_RJ01669710` |
| 559 | 2026 | DLsite 同人 | おんにゃのこっち！ | - | タイトル | `dl_RJ01670163` |
| 560 | 2026 | DLsite 同人 | とける風花とシロうさぎ | KiriKiri | タイトル (製品版 DRM: PlayDRM) | `dl_RJ01677912` |
| 561 | 2026 | DLsite 同人 | フーちゃんいん・ざ・らぼ！ | Unity | タイトル | `dl_RJ01678959` |
| 562 | 2026 | DLsite 同人 | なくしたスマホの位置情報は、あの館を示している ―Last Known Location― | Unity | タイトル | `dl_RJ01682755` |
| 563 | 2026 | DLsite 同人 | オナクラの地雷女に沼って人生詰んでる | - | タイトル | `dl_RJ01683321` |
| 564 | 2026 | DLsite 同人 | 逆討伐 ～爆乳魔法使を屈服させ、性奴隷へ〜 | Unity | タイトル | `dl_RJ01684055` |
| 565 | 2026 | DLsite 同人 | 秘密 女騎士NTR | NW.js/Tyrano | タイトル | `dl_RJ01686418` |
| 566 | 2026 | DLsite 同人 | ウルフ小隊戦記 | WolfRPG | タイトル | `dl_RJ01688457` |
| 567 | 2026 | DLsite 同人 | 胎環リゾート 白砂のシークレット・ビーチ- YUI - | Unity | タイトル | `dl_RJ01688926` |
| 568 | 2026 | DLsite 同人 | 不倫旅館 ～息子の友だちのママ～ | Unity | タイトル → 画面左の ⋮ で設定画面、クレジットも開く (Unity 6、DXVK と mouse-in-pointer パッチでクリック可。体験版では「過ちに耽る」は灰色で選べない) | `dl_RJ01690707` |
| 569 | 2026 | DLsite 同人 | アクメコア運搬兵 | Unity | タイトル → Start で体験版の案内 (従来 Wine) (従来 Wine) | `dl_RJ01691731` |
| 570 | 2026 | DLsite 同人 | Forgotten Village | - | タイトル | `dl_RJ01693441` |
| 571 | 2026 | DLsite 同人 | カンビオンの揺籠 | Unity | タイトル | `dl_RJ01695481` |
| 572 | 2026 | DLsite 同人 | サモンスピリッツ | NW.js/Tyrano | タイトル | `dl_RJ01696742` |
| 573 | 2026 | DLsite 同人 | THE TOWER ～コントラクト～ | Unity | タイトル → OPTION で設定画面 (Unity 6、DXVK と mouse-in-pointer パッチ) | `dl_RJ01696828` |
| 574 | 2026 | DLsite 同人 | かんもく:場面緘黙女子 | NW.js/Tyrano | 本編 | `dl_RJ01698597` |
| 575 | 2026 | DLsite 同人 | とろぴかる・ちぇいす！長年追っていた指名手配犯をヌーディストビーチで発見したクール系女刑事 | NW.js/Tyrano | タイトル (NW.js に Chromium のソフトウェア描画の引数) | `dl_RJ01699647` |
| 576 | 2026 | DLsite 同人 | 星直しのブレイブロード -淫欲の街と堕落の聖女候補- | NW.js/Tyrano | タイトル | `dl_RJ01699765` |
| 577 | 2026 | DLsite 同人 | 魔法少女は触手に負けられない | - | タイトル (.NET 8 Desktop ランタイム + DXVK) | `dl_RJ01700416` |
| 578 | 2026 | DLsite 同人 | お仕置きLesson | Unity | 起動時の利用規約画面 (ゲーム描画) (従来 Wine) | `dl_RJ01700539` |
| 579 | 2026 | DLsite 同人 | ラッキィ・ホール-ハズレは淫魔の搾精穴- | NW.js/Tyrano | タイトル | `dl_RJ01701172` |
| 580 | 2026 | DLsite 同人 | 政治家寝取られ | - | タイトル | `dl_RJ01701406` |
| 581 | 2026 | DLsite 同人 | 【Live2D】逃亡中～ミッションをクリアしてザーメンの放出を阻止せよ！～ | NW.js/Tyrano | タイトル | `dl_RJ01701742` |
| 582 | 2026 | DLsite 同人 | いとこのお姉ちゃんに睡眠●シミュレーション | NW.js/Tyrano | タイトル | `dl_RJ01702392` |
| 583 | 2026 | DLsite 同人 | ドット勇者 | HTML5 | 本編 (ゲーム開始 → 拠点画面。HTML5: Linux の Chromium で開く) | `dl_RJ01702800` |
| 584 | 2026 | DLsite 同人 | 爆乳お母さんと夏の都会性活 | LiveMaker | タイトル | `dl_RJ01703015` |
| 585 | 2026 | DLsite 同人 | 【人外魔境ダイスレギオン】アーリーアクセス版 | Unity | 本編 (ステージ選択) | `dl_RJ01703385` |
| 586 | 2026 | DLsite 同人 | 決着はバトルファックで -初回特典付き- | NW.js/Tyrano | 本編 (マップと会話) | `dl_RJ01703802` |
| 587 | 2026 | DLsite 同人 | 魔王ですが、スライムしか作れません ～死に戻り魔王軍の放置経営～ | - | 起動時の言語選択 (Godot 4。D3D12 指定を --rendering-driver vulkan に切り替え。手動で確認) | `dl_RJ01704479` |
| 588 | 2026 | DLsite 同人 | リノ&ハミー | WolfRPG | タイトル | `dl_RJ01705134` |
| 589 | 2026 | DLsite 同人 | 痴女マゾ舐めプ傭兵"S" | - | タイトル (従来 Wine) | `dl_RJ01705199` |
| 590 | 2026 | DLsite 同人 | 3D無限ぶっかけシミュ ～媚薬精液をすやすや魔女にぶっかけ！～ | Unity | タイトル | `dl_RJ01706267` |
| 591 | 2026 | DLsite 同人 | ショタおねRPG 爆乳お姉さんたちと催〇でドスケベ三昧 爆乳(クソデカボイン)の桃源郷(パラダイス) | NW.js/Tyrano | 本編 (主人公の名前入力) | `dl_RJ01706991` |
| 592 | 2026 | DLsite 同人 | 戦術バトルは不○火舞に任せなさい♪ | Electron | タイトル | `dl_RJ01706994` |
| 593 | 2026 | DLsite 同人 | Alice In Distress RE | Electron | タイトル | `dl_RJ01707379` |
| 594 | 2026 | DLsite 同人 | 感覚遮断落とし穴アニメシミュレーター | - | タイトル | `dl_RJ01707935` |
| 595 | 2026 | DLsite 同人 | こんな安楽椅子探偵は嫌だ！ | YU-RIS | タイトル | `dl_RJ01708454` |
| 596 | 2026 | DLsite 同人 | ひつじのやつ2 | - | タイトル | `dl_RJ01709084` |
| 597 | 2026 | DLsite 同人 | あかりのコーデ | - | タイトル | `dl_RJ01710833` |
| 598 | 2026 | DLsite 同人 | 化けきつねたいじ | Unity | 本編 (「はじめる」をクリック → 冒頭の文章。Unity 6、DXVK と mouse-in-pointer パッチ。手動で確認) | `dl_RJ01711994` |
| 599 | 2026 | DLsite 同人 | 魔胚受胎 ～古代触手と異種交配の実験記録～ | Electron | 本編 (難易度選択。手動で確認) | `dl_RJ01712071` |
| 600 | 2026 | DLsite 同人 | デカノモス | Unity | タイトル | `dl_RJ01714047` |
| 601 | 2026 | DLsite 同人 | たりない天使さま | LiveMaker | タイトル | `dl_RJ01714826` |
| 602 | 2026 | DLsite 同人 | 梨乃ねね ～子種を乞う村～ | - | タイトル (成人確認で「はい」) | `dl_RJ01715225` |
| 603 | 2026 | DLsite 同人 | エッチになってく妹は俺のもの | WolfRPG | タイトル以降 (注意書き→ロード画面) | `dl_RJ01720064` |
| 604 | 2026 | DLsite 同人 | 騎士見習いと村娘 | - | タイトル | `dl_RJ01720376` |
| 605 | 2026 | DLsite 同人 | Moeのファンサービスリフト♡～つり出し | NW.js/Tyrano | タイトル | `dl_RJ01720493` |
| 606 | 2026 | DLsite 同人 | ダーク・デザイア ～隷属おっぱい揉みゲーム～ | LiveMaker | 本編 (ゲーム画面) | `dl_RJ01720600` |
| 607 | 2026 | DLsite 同人 | アイドルダークダンジョン | HTML5 | 本編 (タイトル → 会話とイベント画面。HTML5: Linux の Chromium で開く) | `dl_RJ01720685` |
| 608 | 2026 | DLsite 同人 | 魔法戦士 NANAMI 前編 | NW.js/Tyrano | タイトル | `dl_RJ01720958` |
| 609 | 2026 | DLsite 同人 | むかしグラビアアイドルだった団地妻 | YU-RIS | タイトル | `dl_RJ01721024` |
| 610 | 2026 | DLsite 同人 | 調教パンク2072 | Unity | 起動時の言語選択 (Unity、DXVK でロック待ちが解消) | `dl_RJ01721615` |
| 611 | 2026 | DLsite 同人 | むぎゅっと☆搾精ナイトメア！ | NW.js/Tyrano | タイトル | `dl_RJ01722380` |
| 612 | 2026 | DLsite 同人 | ポンコツ魔法使いセリスのHな冒険記 ～Hな呪いで精液を求める身体になった魔法使い～ | WolfRPG | タイトル | `dl_RJ01724124` |
| 613 | 2026 | DLsite 同人 | 奴隷都市のリセル 18禁版 | - | タイトル | `dl_RJ01724237` |
| 614 | 2026 | DLsite 同人 | 人妻のオナサポを受けて白濁生汁ピュッピュッピュッしなさい！ | Unity | タイトル | `dl_RJ01724681` |
| 615 | 2026 | DLsite 同人 | 父と娘仲直りH旅行～夏休みは南の島のリゾートで!! | Unity | 本編 (オープニングの選択画面。Unity 6、mouse-in-pointer パッチでクリック可) | `dl_RJ01726603` |
| 616 | 2026 | DLsite 同人 | 監禁部屋から逃げれない1 | NW.js/Tyrano | タイトル | `dl_RJ01727123` |
| 617 | 2026 | DLsite 同人 | あの夏、誰も来ないバス停で。 | Unity | タイトル | `dl_RJ01728288` |
| 618 | 2026 | DLsite 同人 | もんむすのペット 甘サド捕食系サキュバス編 | Unity | 本編 (はじめから → 会話。Unity 6、mouse-in-pointer パッチでクリック可) | `dl_RJ01728421` |
| 619 | 2026 | DLsite 同人 | 小さな侵入者 | - | タイトル (Unreal、DXVK でロック待ちが解消) | `dl_RJ01728560` |
| 620 | 2026 | DLsite 同人 | ドキドキ♡ガールズドッジ！ | Unity | タイトル | `dl_RJ01728583` |
| 621 | 2026 | DLsite 同人 | 真依と秘密のマッサージ屋さん | - | タイトル → FreeMode (Unreal、DXVK で描画が詰まらない) | `dl_RJ01729003` |
| 622 | 2026 | DLsite 同人 | 人狼さんはちんちんがお好き。 | WolfRPG | 本編 (マップと会話) | `dl_RJ01729227` |
| 623 | 2026 | DLsite 同人 | すやすやまりちゃんシミュレーター | Unity | 本編 (ゲーム画面) (従来 Wine) | `dl_RJ01729974` |
| 624 | 2026 | DLsite 同人 | 大魔法少女カサネ | NW.js/Tyrano | タイトル | `dl_RJ01730521` |
| 625 | 2026 | DLsite 同人 | あなたと彼女の初えっち | Unity | タイトル | `dl_RJ01730784` |
| 626 | 2026 | DLsite 同人 | 近所に住む(自称)女神のエルちゃんを、家に呼んだらついて来た件 | LiveMaker | タイトル (Electron: GPU を本体プロセスで・SwiftShader) | `dl_RJ01731003` |
| 627 | 2026 | DLsite 同人 | ヤリステナイメスブター 世界を救った後の ボクの日常『性』活 | NW.js/Tyrano | タイトル | `dl_RJ01731048` |
| 628 | 2026 | DLsite 同人 | Bondage Sandbox ep 1: Android in distress | - | 起動時のテーマ選択画面 (Unreal、DXVK でロック待ちが解消) | `dl_RJ01731343` |
| 629 | 2026 | DLsite 同人 | Alto Recipeおまとめパック | WolfRPG | タイトル | `dl_RJ01731780` |
| 630 | 2026 | DLsite 同人 | 灰払い傭兵記 | - | 本編 (初めから → 保存先の選択) | `dl_RJ01732086` |
| 631 | 2026 | DLsite 同人 | セーター越しの乳首弄りでイカサレちゃう女の子 | Unity | 本編 (会話) | `dl_RJ01732951` |
| 632 | 2026 | DLsite 同人 | 彼女をマッサージした後 | Unity | 本編 (H レベル画面) | `dl_RJ01733667` |
| 633 | 2026 | DLsite 同人 | 私、絶対イキません！ | Unity | タイトル (従来 Wine) (従来 Wine) | `dl_RJ01734456` |
| 634 | 2026 | DLsite 同人 | 【ver 1.0.6】転生の間へようこそ | Electron | 本編 (導入の説明画面) | `dl_RJ01735938` |
| 635 | 2026 | DLsite 同人 | 巨乳美人お姉ちゃんは変態野郎なんかに絶対に負けない！ | Unity | タイトル | `dl_RJ01736920` |
| 636 | 2026 | DLsite 同人 | 気弱な二人 | - | タイトル | `dl_RJ01739180` |
| 637 | 2026 | DLsite 同人 | 【VR】精密少女模拟器 ～生理反应×极致自定义～ | - | 本編 (3D の部屋。体験版の残り時間が進む。Unity + DXVK、起動に数分。手動で確認) | `dl_RJ01739908` |
| 638 | 2026 | DLsite 同人 | 社畜淫魔のノルマ生活 -快楽抽出シミュレーション- | Unity | タイトル | `dl_RJ01740259` |
| 639 | 2026 | DLsite 同人 | 淫らに染めて ～叔母が堕ちるまで～ | NW.js/Tyrano | タイトル (NW.js に Chromium のソフトウェア描画の引数を付けて。手動で確認) | `dl_RJ01741863` |
| 640 | 2026 | DLsite 同人 | 同じ階に住む美巨乳団地妻「改」 | Electron | タイトル | `dl_RJ01744242` |
| 641 | 2026 | DLsite 同人 | 幻想の系譜 ～はじまりの巫女編～ | Unity | タイトル (Inno の Foo_Setup_0.2.3.exe を無人実行、ユーザー領域 AppData\\Local\\Programs へのインストール。手動で確認) | `dl_RJ01744611` |
| 642 | 2026 | DLsite 同人 | 新人アイドル、枕営業します。～星川あゆの物語～ 体験版 | - | タイトル (Unreal Engine 5、最新 VC++ ランタイムを入れて llvmpipe で描画) | `dl_RJ01746276` |
| 643 | 2026 | DLsite 同人 | 魔法少女ネム&モコ 悪墜ち魔法少女の誘惑 | RenPy | タイトル | `dl_RJ01746340` |
| 644 | 2026 | DLsite 同人 | 女魔法使いセーラは俺から逃げられない！ ～便利なエロアイテムと孕むまでヤれる封鎖村～ | NW.js/Tyrano | 本編 (マップと会話) | `dl_RJ01746541` |
| 645 | 2026 | DLsite 同人 | 巨乳美人のティふぁが変態プレイを受け続けて脱糞するまで | Unity | タイトル | `dl_RJ01746806` |
| 646 | 2026 | DLsite 同人 | 原○カードクワルドトライアド Vol.2 | Electron | 本編 (マップと会話) | `dl_RJ01747016` |
| 647 | 2026 | DLsite 同人 | Last12 - 勇者の恥ずかしい育児日記 - | NW.js/Tyrano | タイトル (自前で枠を描く NW.js のウィンドウ全体: win32u パッチ) | `dl_RJ357058` |
| 648 | 2026 | DLsite 同人 | ドレスアップナイト -True Knight’s Refrain- | - | タイトル | `dl_RJ365650` |
| 649 | 2026 | DLsite | 宿りし乙女の誓いと魔法 | AdvHD | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01004594` |
| 650 | 2026 | DLsite | Hakoniwa－ハコニワ－ | - | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01005334` |
| 651 | 2026 | DLsite | 【DLsite限定版】友恵の探偵物語 ～のぞき魔な私の天職～ | - | タイトル | `dl_VJ01005807` |
| 652 | 2026 | DLsite | 姫と艶欲のインペリウム | Escude | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01006026` |
| 653 | 2026 | DLsite | 【限定版】モザイクの天使【オナサポボイスドラマ＆デジタル原画・設定資料集付き】 | SiglusEngine | タイトル | `dl_VJ01006176` |
| 654 | 2026 | DLsite | 【限定版】モザイクの天使【デジタル原画・設定資料集付き】 | SiglusEngine | タイトル | `dl_VJ01006181` |
| 655 | 2026 | DLsite | 娼姫レティシア ～今宵、王女は春を売る～ リマスター豪華特典版 | SiglusEngine | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01006508` |
| 656 | 2026 | DLsite | 背徳の母乳 ～そんなに出しちゃダメぇ～ | KiriKiri | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01006612` |
| 657 | 2026 | DLsite | サヤガタリ -妖刀の約束- | SiglusEngine | タイトル (製品版 DRM: PlayDRM) | `dl_VJ01006640` |
| 658 | 2026 | DLsite | 【特典同梱版】FORTUNE×WORLD ～僕らがゲームを作る理由～ | SiglusEngine | 本編 (会話) (製品版 DRM: PlayDRM) | `dl_VJ01006708` |
| 659 | 2026 | DLsite | 【通常版】FORTUNE×WORLD ～僕らがゲームを作る理由～ | SiglusEngine | 本編 (製品版 DRM: PlayDRM) | `dl_VJ01006709` |
| 660 | 2026 | suezou | 養花師の背徳愛～純真天使は抱かれて色めく～ 【体験版】 | Softpal | タイトル | `s2026_amuse_craft_erotica_youkashi` |
| 661 | 2026 | suezou | ご奉仕メイドはエッチなお姉さん ～俺だけの巨乳メイドにたっぷり甘やかされるめちゃエロご主人様ライフ～  - 体験版 - | - | タイトル (痕跡: enigmaprotector) | `s2026_anim_gohousi_maid` |
| 662 | 2026 | suezou | 超狂指導日誌 体験版 | - | タイトル (BISHOP 起動ランチャーの「ゲーム開始」で設定 bsxx.ini を書いてから本体が起動。手動で確認) | `s2026_bishop_tks` |
| 663 | 2026 | suezou | リビドーアバンちゅ～る TRIAL EDITION | AdvHD | タイトル | `s2026_ensemble_sweet_ribichu` |
| 664 | 2026 | suezou | 闇夜のPARENTE 幻燐の姫将軍 Remastered 体験版 | - | 本編前のゲーム開始設定画面 (Inno の setup.exe を無人実行、Unity 6 + DXVK) | `s2026_eushully_parente` |
| 665 | 2026 | suezou | s2026_lump_of_sugar_tayutama_zero | - | タイトル (ログにロック待ちの警告が出るが表示・動作) | `s2026_lump_of_sugar_tayutama_zero` |
| 666 | 2026 | suezou | Jyosou Yuriarasi Trial | KiriKiri | タイトル (言語選択で日本語をクリック) | `s2026_no_strike_jyosou_yuriarashi` |
| 667 | 2026 | suezou | 姉妹X催眠２ - MOTION EDITION - 体験版 | KiriKiri | タイトル | `s2026_poison_motion_sxs2_me` |
| 668 | 2026 | suezou | 姉妹X催眠 - MOTION EDITION - 体験版 | KiriKiri | タイトル (従来 Wine) | `s2026_poison_motion_sxs_me` |
| 669 | - | 手動 | 有閑夫人倶楽部 体験版 | AdvHD | 本編テキスト (icu シム) | `r1_advhd` |
| 670 | - | 手動 | 遥かなるニライカナイ 体験版 | Artemis | 本編 + 音声 | `r1_artemis` |
| 671 | - | 手動 | クダンノフォークロア 体験版 | BGI | 本編テキスト (独自Setup) | `r1_bgi_kudan` |
| 672 | - | 手動 | 果つることなき未来ヨリ 体験版 | CatSystem2 | 本編テキスト | `r1_cs2_hatsumira` |
| 673 | - | 手動 | 彼岸花の咲く夜に お試し版 | NScripter | 本編 (CreateInstall) | `r1_higanbana` |
| 674 | - | 手動 | KAG3 紹介デモ | KiriKiri2 | 本編テキスト (配布物の Config.tjs の config_version を "3.32 stable rev. 2" に直す必要あり。Wine とは無関係) | `r1_kag3` |
| 675 | - | 手動 | 先輩、好きです……押忍！ 体験版 | KiriKiri | 本編テキスト | `r1_krkr_apt306` |
| 676 | - | 手動 | あいれぼ 体験版 | Majiro | 本編テキスト (独自インストーラ) | `r1_majiro` |
| 677 | - | 手動 | Wind RP 体験版 | Musica | 本編テキスト | `r1_musica` |
| 678 | - | 手動 | ナルキッソス SIDE 2nd | NScripter | 本編テキスト | `r1_naru2` |
| 679 | - | 手動 | 天翼のアスクレイン 体験版 | QLIE | 本編テキスト | `r1_qlie` |
| 680 | - | 手動 | 5 -ファイブ- 体験版 | RealLive | 本編テキスト (SFX) | `r1_reallive_five` |
| 681 | - | 手動 | The Question | Ren'Py | 本編テキスト | `r1_renpy` |
| 682 | - | 手動 | でか乳巫女と巨乳シスター 体験版 | SakanaGL | タイトル (Managed=N) | `r1_sakana` |
| 683 | - | 手動 | うみねこのなく頃に咲 体験版 | PONScripter | 本編テキスト | `r1_saku` |
| 684 | - | 手動 | プリマドール 体験版 | SiglusEngine | 本編テキスト | `r1_siglus_primadoll` |
| 685 | - | 手動 | トイレの花子さん 体験版 | Softpal | 本編テキスト | `r1_softpal_hanako` |
| 686 | - | 手動 | 転性魔王さま 体験版 | Softpal | 本編テキスト | `r1_softpal_tenmao` |
| 687 | - | 手動 | トライアンソロジー 体験版 | NScripter | 本編 | `r1_trt` |
| 688 | - | 手動 | 転生した世界で… 体験版 | TyranoScript | 本編テキスト | `r1_tyrano` |
| 689 | - | 手動 | 旭光のマリアージュ 体験版 | Unity | 本編テキスト (vnbox Wine) | `r1_unity` |
| 690 | - | 手動 | YU-RIS サンプル | YU-RIS | 演出/動画 | `r1_yuris` |

<details><summary>弱い確認 (ロゴ・注意画面・起動ダイアログまで) 0 本</summary>

| 年 | タイトル | 止まったところ | ID |
|---|---|---|---|

</details>
<!-- TITLES:END -->

## DRM (DLsite / DMM) について

<!-- DRM:BEGIN -->
| 販売所 / 方式 | 製品版の仕組み | 体験版 | この環境での確認 |
|---|---|---|---|
| DLsite **PlayDRM** | オンライン認証 (アカウント、3〜5 台まで) | DRM なしで配布 | 製品版が PlayDRM の作品の体験版 50 本が動作 (第6弾)。製品版は購入が必要で未確認 |
| DLsite **シリアル番号発行型** | シリアルでオンライン認証 (アカウントに紐付かない) | DRM なし | 体験版 2 本が動作。製品版は未確認 |
| **ソフト電池** (DLsite / DMM) | 専用ランタイムで ID 認証、認証回数の上限あり | DRM なし (体験版は普通の exe) | 体験版 2 本が動作。自前 Wine に fitudao 氏のソフト電池向け修正 (crypt32 / imagehlp / msi) を当て済み。製品版は未確認 |
| **DMM GAME PLAYER** (FANZA) | 専用クライアント経由で起動、起動ごとにオンライン確認 | — | DMM / FANZA はこの環境 (海外) からはログイン画面にしか行けず、体験版もクライアントも取得できない |
| exe プロテクタ (Enigma Protector 等) | 実行ファイルの暗号化・改ざん検知 | 体験版にも掛かっていることがある | 第6弾で検出し、Wine で起動することを確認 (下表の「痕跡」) |

- DLsite の作品ページから製品版の DRM 種別を読み取る: `vnbox candidates dlsite` (「PlayDRM型」「ソフト電池」「シリアル番号発行型」の表記)。体験版 (`trial.dlsite.com`) にはどの方式も入っていなかった (exe・DLL の文字列と readme を確認)。
- `vnbox add` は配布物の exe / DLL から DRM・プロテクタの痕跡 (PlayDRM / ソフト電池 / DMM GAME PLAYER / StarForce / SecuROM / Themida / VMProtect / Enigma 等の文字列・PE セクション名) を探し、見つかれば `launch.ini` の `drm=` に記録してギャラリーに「DRM」と表示する。シグネチャは公開情報からの推測で、製品版での実地確認はまだ。
- 製品版の DRM を実際に確かめるには、購入済みの配布物 (または DMM / DLsite のアカウント) が必要。用意できれば `vnbox add` → 起動 → 認証画面までをこの手順で確認できる。
<!-- DRM:END -->

## 分かったこと (落ちる原因と対策)

1. **仮想デスクトップは不要、むしろ害**: NScripter (DirectDraw) で描画がデスクトップ原点にずれた。ウィンドウ単位にしたら解消し、配信とも相性が良い。
2. **ロケール**: `ja_JP.UTF-8` を locale-gen した上で prefix を作るとコードページ 932 になる。Shift-JIS のアーカイブ名は展開時に UTF-8 へ変換しないと Wine から見えない (`internal/extract`)。
3. **32bit / 64bit を exe で切り替える**: 古い VN は 32bit prefix (Win7)、Ren'Py 8 や新しいエンジンは 64bit prefix (Win10)。`requires Windows 10` で起動拒否されるものがある。
4. **icu.dll**: Win10 標準だが Wine に無い。AdvHD は起動前に c0000135 で落ちる → 公式 ICU への転送 DLL をベースに標準装備。
5. **インストーラ**: Wine 上で普通にインストールし、prefix の差分 (レジストリはテキストファイル同士の diff) を取るだけで DoNotInstaller 相当になる。ゲーム固有キーは `HKLM\Software\<ブランド>\<タイトル>` 系 (BGI はこれが無いと起動しなかった)。Wine 自身が書く MMDevices / winepulse / MenuFiles / Console / デバイス列挙は除外済み。
6. **WM なし環境の注意**: キー入力は毎回 `windowfocus` が必要。D3D/GL のウィンドウは `import -window` 直撮りだと黒いことがあるので画面から矩形を切り出す。仮想画面は 720p ゲーム + 枠より大きくしないと「解像度が足りない」と縮小される (QLIE)。
7. **動画**: DirectShow (quartz + winegstreamer, i386 の GStreamer プラグイン必須) で MPEG-1 は再生されるが、SampleGrabber 方式の NScripter ではタイトル動画が約 8fps。LAV Filters でも同値。GPU 環境で再確認が必要。
8. **起動ダイアログ**: CatSystem2 (画面モード選択) や QLIE (起動設定) はレシピの操作列で自動通過できる。Softpal の「初回起動時のウインドウモード」ダイアログはクリックも Enter も効かず、**フォーカスを当てて Space** で進む (インストーラのダイアログ自動操作が自動でフォールバック)。
9. **1x1 ウィンドウ**: フルスクリーン化を WM 任せにするゲーム (SakanaGL, FVP 系 ANIM 作品) は、WM なしだと X ウィンドウが 1x1 のまま。`X11 Driver\Managed=N` (Wine 自身がウィンドウを管理) で解決。`vnbox serve`・`play`・`probe` が 1x1 を検出すると自動でこの設定にして起動し直す。全体に掛けると Unity などが黒画面になるので、検出時だけ切り替える。
10. **タイムゾーン**: SiglusEngine 系の一部は「日本語版 Windows 判定」をタイムゾーンで行い、UTC だと起動を拒否する。vnbox は `TZ=Asia/Tokyo` を既定にした。
12. **MS フォントの実体**: こすぷれえっち (2014) は `EnumFontFamiliesEx` で「ＭＳ ゴシック」を名前で探し、無いと CRT の不正パラメータ例外で落ちた。Wine の置換 (Replacements) は列挙に出ない。IPA フォントの name テーブルを書き換えた「ＭＳ ゴシック / Ｐゴシック / 明朝 / Ｐ明朝 / UI Gothic」を prefix の Fonts に登録して解決 (`vnbox-msfonts`、ベースに標準装備)。
13. **従来 32bit Wine が要るもの**: ユユカナ (2011, nanawind) は「Authentication failed : unrecognized kernel32 module」。新 WoW64 では 32bit の kernel32 が syswow64 から読まれるためで、WineHQ の従来 32bit prefix では動いた。NG 時は `vnbox verify` が従来 Wine (`-wine classic`) で再試行する。
14. **起動の遅いゲーム**: BGI は起動に 40 秒ほど黒画面のことがある。1x1 ウィンドウ検出は 20 秒待ってから Managed=N に切り替えるようにした。
15. **ロケールを忘れた手動実行**: BGI は `GetSystemDefaultLangID` が日本語でないと何も言わずに終了する。手で wine を叩くときも `LANG=ja_JP.UTF-8` / `TZ=Asia/Tokyo` を付ける (vnbox はすべて自動で付ける)。
16. **InstallShield**: 新しめの setup.exe (署名 "InstallShield" がオーバーレイにある版) は中身がファイル名由来の鍵で XOR + ニブル入替されている。Wine でブートストラッパを動かすより、取り出して MSI を直接入れる方が確実。
17. **クリックの長さ**: Eushully AGE などフレームごとにマウス状態を見るゲームは、xdotool の一瞬のクリックを取りこぼす。配信の入力は「押して 60ms 待って離す」にした。
18. **XAML Islands**: Cyberworks TinkerBell の新しい版 (せをはやみ。) は C++/WinRT で `Windows.UI.Xaml.Hosting.DesktopWindowXamlSource` と TextBox を作る (文字入力欄用)。Wine には Windows.UI.Xaml の実装が無く、未捕捉の例外 (c0000409) で落ちる。何もしない代用品 `xamlstub32.dll` を Windows SDK のメタデータから生成して prefix に登録すると先へ進む (`tools/xamlstub/`、`vnbox-xamlstub`。`vnbox add` が exe 内の文字列から判定して `winetricks=vnbox-xamlstub` を付ける)。vtable は x86 stdcall の引数バイト数まで正確でないとスタックが壊れるので、手書きでなく .winmd から生成している。入力欄は表示されない (名前入力などは既定値のまま)。
19. **Electron 製 (TyranoScript v5 等)**: GPU プロセスが Wine + GPU なしで起動できず「Cannot initialize WebGL」。`--in-process-gpu --use-angle=swiftshader --enable-unsafe-swiftshader --ignore-gpu-blocklist` で動く。`app.asar` があれば `vnbox add` が起動引数に付ける。
20. **InstallShield 2010 以降**: setup.exe 末尾の `ISSetupStream` 形式 (属性 48 バイト + UTF-16 名、名前の UTF-8 から作る鍵で XOR + ニブル入替、その後 zlib) から MSI を取り出して `msiexec /qn` で入れる。旧形式 (`InstallShield`) と同じく Wine でブートストラッパを動かさない。
21. **入れ子の自己解凍**: zip の中身が ZIP 自己解凍 exe 1 つだけ、という配布がある (スペルマン)。exe をゲームとして起動すると何も出ないので、もう一段展開する。
11. **インストーラの自動操作**: MSI は `msiexec /qn`、それ以外 (CreateInstall / BGI / Majiro / CatSystem2 / InstallShield / 独自) は小さいダイアログに Enter (効かなければ Space) を送り続けるだけで、今回の 8 種すべて完了した。WinRAR SFX は Linux 側の unrar で開けないことがあり、Wine で SFX 自体を動かして展開した。

22. **ウィンドウの大きさ**: WM が無いと最大化・全画面の要求が実行されず、Wine 側は 1920x1080 なのに X のウィンドウが小さいまま右下が切れていた (かけぬけ★青春スパーキング)。枠を描かない openbox を Xvfb ごとに動かす。`vnbox probe` は Wine 内から見たクライアント領域と X のウィンドウを比べ、X 側が小さければ `NG:size_mismatch` にする。
23. **自前で枠を描くウィンドウ**: Chromium 系 (NW.js / Electron) は WM_NCCALCSIZE でクライアント領域を枠いっぱいに広げる。Wine は WM が枠を描く前提でタイトルバー分を X ウィンドウから外すので、上 30px が欠けていた (RPG ツクール MZ 製)。win32u パッチでウィンドウ全体を出す。
24. **NW.js も Chromium**: RPG ツクール MZ の一部は GPU プロセス無しでは真っ黒のまま。Electron と同じ引数を `vnbox add` が NW.js (nw.dll) にも付ける。
25. **ダイアログの重なり**: 「フルスクリーンで表示しますか？」や「ディスプレイ設定を変更しますか？」がゲーム本体の大きなウィンドウの手前に出る。`pass:` は最大のウィンドウでなく、いちばん小さいダイアログ大のウィンドウに既定のボタンを押させ、続けて出るダイアログも数秒待つ。
26. **既定のボタンが無いインストーラ**: 「インストール」「同意する」「次へ」を Enter で押せないものは `tools/clickbtn` (Wine 内で BM_CLICK) がラベルで探して押す。ボタンを自前で描く BGI のメニュー (子ウィンドウなし) は、ウィンドウクラスと大きさで位置を決め打ちする。押下は PostMessage (モーダルダイアログを開くと SendMessage が戻らない)。
27. **prefix の hardlink**: 複製した prefix は base と hardlink を共有するので、`cp` で既存ファイルを上書きすると base まで書き換わる (DXVK や VC++ の DLL が base に入っていた)。`--remove-destination` で置き換え、winetricks の前は system32 等を自前のコピーにする。
28. **起動ランチャー**: BISHOP の startup.exe は作品によって「ゲーム開始」で C++ 例外を出して終わる。startup.ini の `[EXE] Boot=/Demo=` を直接起動する。Eushully の InstallLauncher.exe は隣の Inno の setup.exe を無人で実行すれば済む。
29. **Unity 6 (6000.x)**: wined3d では黒画面。DXVK で描画されるので `vnbox add` が UnityPlayer.dll の版を見て `vnbox-dxvk` を付ける。また Unity 6 の入力システムは EnableMouseInPointer を呼んだ後マウスを WM_POINTER でしか受け取らず、Wine 11.0 では何をクリックしても反応しなかった。通常のマウスメッセージを WM_POINTER に置き換えるだけでは駄目で、上流 (Wine 11.3 以降) と同じく両方を配る win32u パッチで直った。
30. **VR 専用**: DLsite の「VR」作品には OpenXR ヘッドセットが無いと何も描かないものがある (対象外)。

31. **LZH の名前**: lhasa (lha) は全部大文字の MS-DOS 名を小文字にするので、Shift_JIS の 2 バイト目が英大文字の文字まで変わり、同じフォルダのファイルが別名のフォルダに分かれた (逃亡者4 の MSI が CAB を見つけられない)。LZH は 7z で展開する。
32. **中国語版 Windows で作られた zip**: ファイル名が GBK。CP932 として読むと「素材」が「ﾋﾘｲﾄ」に化け、画像が読めず背景が黒い。CP932 で読めない、または大半が半角カナに化けるなら GBK として展開する。
33. **Godot 4**: D3D12 指定の作品は Wine 内蔵 vkd3d が DXIL を読めずに落ちる。`--rendering-driver vulkan` で描ける。
34. **D3D12 専用の Unreal Engine 5.8**: D3D11 / Vulkan を「プロジェクトが対応していない」で拒否する。vkd3d-proton と機能レベル上書きでも GPU なし (lavapipe) では進まないので対象外 (GPU 必須)。
35. **並列の vnbox add**: 同じ名前の hash から X ディスプレイ番号を決めていたため、2 本のインストーラが同じ画面に出て互いのボタンを押していた。空いているディスプレイを使う。

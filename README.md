# dirdoc

Directory Document

AI エージェント向けに、ディレクトリ（DSD）とファイル（PFD）の説明を各ディレクトリの `.dirdoc/` に保存し、`dirdoc [PATH]` で Markdown または JSON として表示する CLI です。保存形式は既定では JSON を zstd で圧縮したもので、JSON や TOML（PR の画面でも差分が読める）も選べます。

仕様は [docs/DESIGN.md](docs/DESIGN.md) を参照してください。設計書のロードマップの Phase 1〜4 を実装しています。

## インストール

[Releases](https://github.com/SYZD-research/dirdoc/releases) から、使うプラットフォームのアーカイブ（Linux・macOS・Windows・各種 BSD など）をダウンロードして展開し、`dirdoc` を PATH の通った場所に置きます。Go があれば次でも入れられます。

```bash
go install github.com/SYZD-research/dirdoc/cmd/dirdoc@latest
```

クローンした手元のソースから入れるときは、リポジトリ直下で `go install ./cmd/dirdoc` を実行します。

## 使い方

```bash
dirdoc init                         # dirdoc.toml を作る（--git で git diff の設定も）
dirdoc export src/ > /tmp/src.toml  # 編集用の TOML を出す（説明が無ければひな形）
dirdoc compile src/ /tmp/src.toml   # 検証して src/.dirdoc/.dirdoc_dsd に保存する
dirdoc src/                         # ディレクトリの概要・ツリー・各ファイルの目次
dirdoc src/main.rs                  # ファイルの詳細
dirdoc 'src/main.rs#main'           # 1 つの関数
dirdoc search tty                   # 全文検索（.dirdoc/ は圧縮されていることがあるので grep ではなくこれを使う）
dirdoc check                        # 検証（CI では --strict。--stale で古い記述、--undocumented で記述の無いシンボルも）
dirdoc status                       # どのディレクトリ・ファイルに説明があるか（stale・none なども）
dirdoc touch src/main.rs            # コードは変えたが説明はそのままでよいとき、stale の表示を消す
dirdoc edit src/main.rs             # $EDITOR で編集して compile（人間向け）
dirdoc rm 'src/main.rs#old'         # エントリ（PATH だけなら文書全体）を消す
dirdoc mv src/main.rs src/app.rs    # ファイルを説明ごと移動し、参照を書き換える
dirdoc convert --to toml            # 保存形式を一括で変換する（--to を省くと dirdoc.toml の形式にそろえる）
dirdoc guide                        # AGENTS.md / CLAUDE.md に貼るエージェント向けの手順
```

Go・Rust・TypeScript・JavaScript・Java・Kotlin・Python・シェルスクリプトのファイルはコードも解析し、目次に行番号とシグネチャを付け、説明の無いシンボルを一覧にします。メソッドは `Pool.spawn`（Rust では `Pool::spawn`）のような ID で結びつきます。ファイルやエントリの表示には、どこから参照されているか（被参照）も出ます。

一部だけ更新するときは `--merge` を使います。

```bash
dirdoc compile src/main.rs --merge - <<'EOF'
[[functions]]
name = "check_stdout()"
summary = "標準出力がTTYかを判定する"
EOF
```

### 保存形式

`dirdoc.toml` の `[storage] format` で、`zstd`（既定。JSON を圧縮したもの）・`json`・`toml` から選びます。`toml` は `export` が出す TOML に生成した情報を足したもので、GitHub などの PR の画面でも説明の差分がそのまま読めます。どの形式も読めるので混在してもかまいませんが、形式を変えたら `dirdoc convert` で既存のファイルをそろえてください（`dirdoc check` は形式の違うファイルを `storage-format` として知らせます）。

```toml
[storage]
format = "toml"
```

### 端末での表示

標準出力が端末のとき、表示・`search`・`status`・`check`・`export`・`cat`・`guide` の出力は `man` のようにページャー（`$DIRDOC_PAGER`、`$PAGER`、どちらも無ければ `less`）で開きます。1 画面に収まる出力はそのまま表示します（`LESS` を設定していなければ `LESS=FRX` を渡します）。表示と `guide`、`search`・`check`・`status` には色も付きます（見出しは太字、参照は青、コードは緑など。文字そのものは変わりません）。

```bash
dirdoc src/ --no-pager              # ページャーを使わない（DIRDOC_PAGER=cat でも同じ）
dirdoc src/ --color=never           # 色を付けない（NO_COLOR を設定しても同じ。--color=always でパイプにも付ける）
```

パイプやファイルへの出力は変わりません。エージェントが端末（PTY）の上でコマンドを実行する環境では、ページャーが入力を待って止まらないように `DIRDOC_PAGER=cat` を設定してください。

### git と MCP

```bash
dirdoc init --git                   # git diff で TOML を表示し、.dirdoc_* をエントリ単位でマージする（クローンごとに実行）
claude mcp add dirdoc -- dirdoc mcp # MCP クライアントから show / search / compile などをツールとして使う
```

マージで衝突したファイルは、競合マーカー付きの TOML になります。編集してマーカーを消し、表示されたとおり `dirdoc compile <対象> <そのファイル>` で保存し直してください。

## 開発

```bash
go test ./...
go test ./internal/cli -update    # 出力を変えたときにゴールデンファイルを更新する
go test ./internal/source -run '^$' -fuzz FuzzParseTree -fuzztime 60s
go test ./internal/analyze -run '^$' -fuzz FuzzFile -fuzztime 60s
```

### リリース

`v` で始まるタグを push すると、GitHub Actions（`.github/workflows/release.yml`）がテストのあと、Go が対応するほぼすべてのプラットフォーム向けにビルドし、アーカイブとチェックサムを Release に載せます。`-` を含むタグ（`v0.3.0-rc.1` など）はプレリリースになります。

```bash
git tag v0.3.0
git push origin v0.3.0
```

ビルドしないのは、cgo が要る android（386・amd64・arm）と ios、JavaScript のホストが要る js/wasm だけです。

このリポジトリ自体も dirdoc で記述しています。`dirdoc`、`dirdoc internal/`、`dirdoc internal/model/id.go` などで読めます。

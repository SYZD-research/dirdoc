# dirdoc

Directory Document

AI エージェント向けに、ディレクトリ（DSD）とファイル（PFD）の説明を各ディレクトリの `.dirdoc/` に保存し、`dirdoc [PATH]` で Markdown または JSON として表示する CLI です。保存形式は JSON を zstd で圧縮したものです。

仕様は [docs/DESIGN.md](docs/DESIGN.md) を参照してください。現在の実装は Phase 1（MVP）です。

## インストール

```bash
go install ./cmd/dirdoc
```

モジュールパスは仮に `dirdoc` にしています。公開するときは go.mod を `github.com/<owner>/dirdoc` などに変えてください。

## 使い方

```bash
dirdoc init                         # dirdoc.toml を作る（--git で git diff の設定も）
dirdoc export src/ > /tmp/src.toml  # 編集用の TOML を出す（説明が無ければひな形）
dirdoc compile src/ /tmp/src.toml   # 検証して src/.dirdoc/.dirdoc_dsd に保存する
dirdoc src/                         # ディレクトリの概要・ツリー・各ファイルの目次
dirdoc src/main.rs                  # ファイルの詳細
dirdoc 'src/main.rs#main'           # 1 つの関数
dirdoc search tty                   # 全文検索（圧縮されているので grep は使えない）
dirdoc check                        # 検証（CI では --strict）
dirdoc guide                        # AGENTS.md / CLAUDE.md に貼るエージェント向けの手順
```

一部だけ更新するときは `--merge` を使います。

```bash
dirdoc compile src/main.rs --merge - <<'EOF'
[[functions]]
name = "check_stdout()"
summary = "標準出力がTTYかを判定する"
EOF
```

## 開発

```bash
go test ./...
go test ./internal/cli -update    # 出力を変えたときにゴールデンファイルを更新する
go test ./internal/source -run '^$' -fuzz FuzzParseTree -fuzztime 60s
```

このリポジトリ自体も dirdoc で記述しています。`dirdoc`、`dirdoc internal/`、`dirdoc internal/model/id.go` などで読めます。

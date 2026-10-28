# dirdoc 設計書（v0.3）

> ステータス: Phase 1〜3 を実装済み（2026-10-26）。決定の経緯は §20 に記録する。
> 実装中に決めた細部（§7 のルート行と制御文字、§9.1 の compile の出力、§18 の構成とテスト）も反映してある。
> Phase 2・3 で決めたこと（本文テンプレートの形、コード解析を Go に限ったこと、マージの衝突の表し方など）は §20.2 にまとめた。

## 0. 決定済み事項

| 項目 | 決定 | 備考 |
|---|---|---|
| 正（Source of Truth） | `.dirdoc_*`（zstd 圧縮 JSON）。git で管理するのもこれだけ | 元ファイルは一時的なもの |
| 編集フロー | `export` でテキスト化 → 編集 → `compile` で検証して書き戻す | `--merge` で部分更新、標準入力も可 |
| 元ファイルの記法 | TOML（JSON も入力可） | Tree は TOML 文字列の中に独自記法で書く |
| Overview | compile 時に Types / Functions / Variables から生成 | Go・Rust・TypeScript・JavaScript・Java・Kotlin・Python・シェルのファイルはコードも解析し、行番号・シグネチャ・記述の無いシンボルを足す（§10.6） |
| 実装言語 | Go | zstd は純 Go 実装を使い、cgo 不要 |
| 型の記述 | PFD に任意の `types` セクションを追加する | 元の仕様からの追加。メソッドは functions に `Pool::spawn()`（Rust）や `Pool.spawn()` のように、コード解析と同じ ID で書く（§10.6） |
| 古さの検知 | PFD に対象ファイルのハッシュを記録する | 表示時に 1 行の注意を出すだけ。`check` は `--stale` を付けたときだけ報告する（§10.5） |
| DSD 配列のキー名 | 仕様どおり `dsd` | |
| summary の長さ | DSD 200 / PFD 400 / エントリ 120 文字 | 超えたら警告するだけ。設定で変えられる |
| 表示の既定の詳細度 | ディレクトリを指定したときは各ファイルの目次まで | 本文は `--full` かファイル指定で読む |
| 本文テンプレート | dirdoc.toml の `[template]` に必須の見出しを書く | export のひな形に入り、compile と check が警告する（§17） |
| メッセージの言語 | CLI のメッセージは英語 | `guide` は日本語と英語の両方を用意する |

## 1. 目的と非目的

### 目的

- AI エージェントが、コードを開く前に「このディレクトリ／ファイルに何があるか」を少ないトークンで把握できるようにする。
- 段階的に読めるようにする。ディレクトリの概要 → ファイルの目次 → 個々の関数の詳細、と必要な分だけ読めばよい。
- 決まった構造で書かせ、機械的に検証できるようにする（ID の重複、リンク切れ、実ファイルとの食い違いを検出する）。

### 非目的

- dirdoc 自身が LLM を呼んで説明文を生成すること。書くのはエージェント（や人間）で、dirdoc はフォーマット・検証・表示に専念する。決定的に、オフラインで動き、特定のエージェントに依存しない。
- godoc / rustdoc のような API リファレンス生成の代替。ソースコード中のコメントからは生成しない。
- document 本文の書式を決めること。書き方は AGENTS.md などでプロジェクトが決める（§17）。

### 全体の流れ

```text
             export                       compile
.dirdoc_*  ─────────▶  元ファイル (TOML)  ─────────▶  .dirdoc_*
(zstd JSON)              ↑ 編集                    検証・正規化・Overview 生成
    │
    │ dirdoc [PATH]
    ▼
Markdown / JSON    ← 表示時に名前の補完・参照の解決・実ファイルとの照合を行う
```

## 2. 用語

| 用語 | 意味 |
|---|---|
| 対象（target） | 記述される実体。ディレクトリまたはファイル |
| 所有ディレクトリ | `.dirdoc/` を直下に持つディレクトリ。その `.dirdoc/` が記述する範囲 |
| DSD | Directory Structure Document。ディレクトリ 1 つにつき最大 1 つ |
| PFD | Program File Document。ファイル 1 つにつき最大 1 つ |
| エントリ | DSD の `dsd[]`、PFD の `types[]` / `functions[]` / `variables[]` の各要素。`id` を持つ |
| 元ファイル（source） | 人間／エージェントが書く TOML。`compile` の入力で、`export` の出力。保存はしない |
| プロジェクトルート | `dirdoc.toml` がある最も近い祖先。無ければ `.git` がある最も近い祖先。どちらも無ければカレントディレクトリ |
| 参照（ref） | エントリやドキュメントを指す文字列。`src/main.rs#check_stdout` など（§6） |

## 3. 配置規則

以降の例は、Rust で書かれた架空の CLI アプリ `myapp` を使う（dirdoc 自体は言語に依存しない）。

```text
myapp/
├── dirdoc.toml                  # 任意。プロジェクト設定とルートの目印を兼ねる
├── .dirdoc/
│   └── .dirdoc_dsd              # myapp/ の DSD
├── Cargo.toml
└── src/
    ├── .dirdoc/
    │   ├── .dirdoc_dsd          # src/ の DSD
    │   └── main.rs.dirdoc_pfd   # src/main.rs の PFD
    ├── main.rs
    ├── cli/
    │   ├── args.rs
    │   └── commands.rs
    └── agent/
        ├── .dirdoc/
        │   ├── .dirdoc_dsd
        │   └── pool.rs.dirdoc_pfd
        ├── pool.rs
        └── runner.rs
```

- **R1** `.dirdoc/` は所有ディレクトリを記述する。
- **R2** DSD のファイル名は `.dirdoc_dsd` で固定する。
- **R3** PFD のファイル名は `<対象ファイル名>.dirdoc_pfd`。対象は**所有ディレクトリ直下のファイルに限る**。サブディレクトリのファイルは、そのサブディレクトリの `.dirdoc/` に置く。
  - 理由: 対応関係が一意になる。ディレクトリを移動・リネームしても、記述が一緒に付いてくる。
- **R4** `.dirdoc/` は記述したいディレクトリにだけ作ればよい。DSD だけ、PFD だけでもよい。
- **R5** `.dirdoc/` 内で上記の命名に当てはまらないファイルは無視する（`check` で警告）。

## 4. 保存形式（コンテナ）

- 中身は UTF-8 の JSON オブジェクト 1 つ。
- 既定では zstd の単一フレームに圧縮して保存する。読むときは**メモリ内で解凍**し、一時ファイルは作らない。
- 圧縮されているかどうかは先頭バイトで自動判定する。
  - `28 B5 2F FD`（zstd のマジックナンバー）→ 解凍して JSON として読む
  - 空白を除いた先頭が `{` → 非圧縮 JSON として読む
  - それ以外 → エラー
- `dirdoc.toml` で `storage.compression = "none"` にすると、非圧縮（整形済み JSON）で保存する。読み込みは常に両方に対応するので、混在してもよい。
- **差分を出さない書き込み**: 書き込む前に既存ファイルを解凍して比べ、JSON が同じなら書き込まない。圧縮ライブラリを更新して出力バイト列が変わっても、git に無意味な差分が出ない。
- 出力内容は決定的にする。キー順を固定し、タイムスタンプなどは含めない。
- 書き込みは一時ファイルに書いてから rename する（途中で落ちてもファイルが壊れない）。
- 解凍後のサイズに上限を設ける（既定 16 MiB）。信頼できないリポジトリに仕込まれた zstd bomb への対策。
- 改行は LF に正規化する。パス区切りは常に `/` を使う。

## 5. スキーマ（保存される JSON）

### 5.1 共通

- 先頭の 2 フィールドで種別とバージョンを示す。
  - `format`: `"dirdoc/dsd"` または `"dirdoc/pfd"`
  - `version`: スキーマのメジャーバージョン（整数。現在は `1`）。知らないメジャーバージョンは読み込みエラーにして、dirdoc の更新を促す。
- 知らないフィールドは読み込み時に保持し、書き戻すときも消さない。新しい dirdoc が書いたファイルを古い dirdoc で `--merge` しても、情報が落ちない。
- `x-` で始まるキーはユーザー拡張用。検証せず、そのまま保持する。
- 保存するのは「正規化した記述内容」と「compile が生成する情報（Overview と対象ファイルのハッシュ）」だけ。名前の補完や逆引き（被参照）など、ファイルシステムや他のドキュメントに依存する情報は表示時に計算する（§10）。

### 5.2 DSD

```json
{
  "format": "dirdoc/dsd",
  "version": 1,
  "summary": "アプリ本体のソースコード。CLI とエージェント実行系からなる。",
  "tree": {
    "name": ".", "type": "dir", "dsd": "src",
    "children": [
      { "name": "agent", "type": "dir", "note": "エージェントの起動と管理" },
      { "name": "cli", "type": "dir", "dsd": "cli", "note": "引数解析とサブコマンド" },
      { "name": "main.rs", "type": "file", "note": "エントリポイント" }
    ]
  },
  "dsd": [
    { "id": "src", "name": "ソースルート", "document": "クレートのルート。…" },
    { "id": "cli", "document": "clap の derive でコマンドを定義する。…" }
  ]
}
```

| フィールド | 型 | 必須 | 説明 |
|---|---|---|---|
| `summary` | string | 推奨 | ディレクトリ自体の短い要約。空のとき・長すぎるときは警告（§12） |
| `tree` | TreeNode | 任意 | ルートノードは所有ディレクトリ自身（`name` は常に `"."`） |
| `dsd` | Entry[] | 任意 | ディレクトリごとのドキュメント |

**TreeNode**

| フィールド | 型 | 説明 |
|---|---|---|
| `name` | string | 名前。ルートは `"."` |
| `type` | `"dir"` \| `"file"` | 種別 |
| `dsd` | string | 同じ DSD 内のエントリ ID（任意） |
| `note` | string | 1 行メモ（任意） |
| `more` | bool | 列挙していない子がある（記法の `...`）。任意 |
| `children` | TreeNode[] | 子ノード（`dir` のみ） |

**DSD エントリ**

| フィールド | 型 | 必須 | 説明 |
|---|---|---|---|
| `id` | string | ✓ | ドキュメント内で一意（§6.1）。Tree から指すので省略できない |
| `name` | string | | 省略すると表示時に補完する。Tree でこの ID を指すノードの名前（ルートなら所有ディレクトリ名）を使う。指すノードが無いときや、複数のノードが違う名前で指しているときは `id` を使う |
| `document` | string | | Markdown の本文 |

1 つのエントリを複数のノードから指してもよい（`v1/` と `v2/` で同じ説明を共有する、など）。

### 5.3 PFD

```json
{
  "format": "dirdoc/pfd",
  "version": 1,
  "target": "main.rs",
  "target_hash": "sha256:9f2c…",
  "summary": "CLIのエントリポイント。引数を解析して各サブコマンドへ委譲する。",
  "overview": {
    "types": [
      { "id": "Config", "name": "Config", "summary": "コマンドライン引数と設定ファイルから組み立てる実行時設定" }
    ],
    "functions": [
      { "id": "main", "name": "main()", "summary": "プロセスのエントリポイント" },
      { "id": "check_stdout", "name": "check_stdout()", "summary": "標準出力がTTYかを判定する" }
    ],
    "variables": [
      { "id": "MAX_AGENTS", "name": "MAX_AGENTS", "summary": "同時に処理するエージェント数の上限（既定 8）" }
    ]
  },
  "types": [
    { "id": "Config", "name": "Config", "summary": "コマンドライン引数と設定ファイルから組み立てる実行時設定", "document": "…" }
  ],
  "functions": [
    { "id": "main", "name": "main()", "summary": "プロセスのエントリポイント", "document": "…" },
    { "id": "check_stdout", "name": "check_stdout()", "summary": "標準出力がTTYかを判定する", "document": "…" }
  ],
  "variables": [
    { "id": "MAX_AGENTS", "name": "MAX_AGENTS", "summary": "同時に処理するエージェント数の上限（既定 8）" }
  ]
}
```

| フィールド | 型 | 必須 | 説明 |
|---|---|---|---|
| `target` | string | ✓ | 対象のファイル名。PFD のファイル名と一致しなければエラー（`.dirdoc_pfd` を手でリネームした事故を検出する） |
| `target_hash` | string | 生成 | compile（または `touch`）した時点の対象ファイルのハッシュ（`sha256:<16進>`。CRLF と CR を LF にそろえてから計算する）。古さの検知に使う（§10.5）。Phase 1 で作った PFD には無い |
| `summary` | string | 推奨 | ファイルの役割・目的 |
| `overview` | object | 生成 | compile が生成する目次。元ファイルには書かない（書いても警告して無視する） |
| `types` | TypeEntry[] | 任意 | 元の仕様からの追加 |
| `functions` | FunctionEntry[] | 任意 | |
| `variables` | VariableEntry[] | 任意 | |

- **TypeEntry**: `name`（必須）、`summary`（必須）、`id`（省略時は name から導出）、`document`（任意、Markdown）。クラス・構造体・インターフェース・列挙型などを書く。メソッドは types の中に入れ子にせず、functions に `Pool::spawn()`（Rust）や `Pool.spawn()` のように型名を付けて書く（コード解析と同じ ID にする。§10.6）。
- **FunctionEntry**: `name`（必須）、`summary`（必須）、`id`（省略時は name から導出）、`document`（任意、Markdown）
- **VariableEntry**: `name`（必須）、`summary`（必須）、`id`（省略時は name から導出）。仕様どおり `document` は持たない。
- **overview**: `types` / `functions` / `variables` のそれぞれについて、記述順に `{id, name, summary}` を並べたもの。対象ファイルを解析できるとき（§10.6 の言語のファイル）は、ID が一致するシンボルの `line`（行番号）と `signature`（1 行の宣言）を各項目に足し、どのエントリも指していないシンボルを `undocumented`（`{id, kind, line, signature}` の配列、行番号順）に並べる。解析していないときは `undocumented` 自体が無い（空配列は「解析したがすべて記述済み」の意味）。

```json
"overview": {
  "types": [{ "id": "Target", "name": "Target", "summary": "ビルド対象", "line": 10, "signature": "type Target struct{ ... }" }],
  "functions": [{ "id": "build", "name": "build(t)", "summary": "アーカイブ名を返す", "line": 22, "signature": "func build(t Target) string" }],
  "variables": [],
  "undocumented": [{ "id": "Target.String", "kind": "function", "line": 26, "signature": "func (t Target) String() string" }]
}
```

## 6. ID と参照

### 6.1 ID

- 使える文字: 英数字（Unicode の文字・数字を含む）と `_` `.` `:` `$` `-`。長さは 1〜128 文字。大文字と小文字は区別する。
- 一意である範囲: **1 つのドキュメントの中**。PFD では types・functions・variables をまたいで一意にする（`#id` だけでどれを指すか決まるように）。
- `id` を省略したときは name から導出する（types / functions / variables のみ）。
  1. name の中で最初に現れる `(`・`<`・`[` から後ろを捨てる（引数リストと型パラメータを除くため）
  2. 前後の空白を除く
  3. 残りが ID として正しければそれを使う。正しくなければ compile エラーにして、`id` を明示してもらう

  | name | 導出される id |
  |---|---|
  | `check_stdout()` | `check_stdout` |
  | `run(args: &[String]) -> i32` | `run` |
  | `Parser::parse(&self)` | `Parser::parse` |
  | `Pool<T>` | `Pool` |
  | `Map[K comparable, V any]` | `Map` |
  | `MAX_AGENTS` | `MAX_AGENTS` |
  | `async def fetch(url)` | エラー（空白を含む）→ `id` を明示する |
  | `(s *Server) Start()` | エラー（空になる）→ `id` を明示する |

  推測でおかしな ID を作るより、決まらないときは明示してもらう方針。
- オーバーロードなどで導出結果が重なった場合も compile エラーになる。`id` を明示して区別する。
- 保存される JSON では `id` は常に埋まっている（読む側は導出ルールを知らなくてよい）。`export` も `id` を常に明示して出力する（往復で ID が勝手に変わらないように）。

### 6.2 参照の書き方

`document` と `summary` の中に `[[参照]]` と書くと、他のエントリやドキュメントへのリンクになる。`[[参照|表示名]]` で表示名を付けられる。

| 書き方 | 指す先 |
|---|---|
| `[[#id]]` | 同じドキュメント内のエントリ |
| `[[args.rs]]` | ファイルの PFD 全体 |
| `[[args.rs#parse]]` | ファイルの PFD 内のエントリ |
| `[[agent/]]` | ディレクトリの DSD 全体（末尾の `/` がディレクトリの印） |
| `[[agent/#pool]]` | ディレクトリの DSD 内のエントリ |
| `[[/src/cli/args.rs#parse]]` | 先頭の `/` はプロジェクトルートからのパス |

- パスは**所有ディレクトリからの相対パス**で書く（DSD・PFD で共通）。`../` も使える。プロジェクトルートの外を指す参照はエラー。
- パスと ID は最後の `#` で区切る。最初の `|` より後ろは表示名。
- コードスパン（`` `…` ``）やコードブロックの中の `[[…]]` は参照として扱わない。`\[[` でエスケープできる。
- Tree の中でも、同じ `[[#id]]` 記法でエントリを指す（§7）。
- 表示時は、すべての参照を**カレントディレクトリからの相対パス**に書き換えて出力する（`[[#MAX_AGENTS]]` → `[[src/main.rs#MAX_AGENTS]]`）。出てきた参照は、そのまま `dirdoc '<参照>'` に渡せる。
- 検証: 同じドキュメント内の参照（`[[#id]]`）の指す先が無ければ compile エラー。他のドキュメントへの参照は、指す先がまだ書かれていないこともあるので警告にとどめる（`check --strict` ではエラー）。

## 7. Tree 記法

元ファイルでは `tree = '''…'''` の中に次の記法で書く。`tree` コマンドの出力に注釈を足す感覚で書ける。

```text
./                [[#src]]
├── agent/                     # エージェントの起動と管理
├── cli/          [[#cli]]     # 引数解析とサブコマンド
│   ├── args.rs
│   └── ...
├── generated/                 # ビルド時に生成（記述不要）
└── main.rs                    # エントリポイント
```

1. 1 行に 1 ノード。行頭の罫線（`│ ├ └ ─`、ASCII の `|`・`` ` `` とそれに続く `-`）と空白は、インデントとして読み飛ばす。タブと制御文字は使えない（エラー）。
2. 深さは名前が始まる桁で決まる。インデント幅は自由で、`tree` の出力・2 スペース・4 スペースのどれでもよい。兄弟は同じ桁に揃える。
3. 名前が `/` で終わればディレクトリ、それ以外はファイル。
4. `[[#id]]` で同じ DSD 内のエントリを指す（任意）。
5. 前に空白がある ` # ` から行末までが 1 行メモ（`note`）。
6. `...`（または `…`）だけの行は「ここに列挙していない子がある」ことを表す（親が `more: true` になる）。
7. 空白・`#`・`[[` を含む名前は `"…"` で囲む。
8. 1 行目が `.` か `./` なら、ルート（所有ディレクトリ自身）の行とみなす。ルートには兄弟が無いので、続く行はルート行と同じ桁から書いてもよい。1 行目がそれ以外ならルートは暗黙で、全行がルートの子孫になる。
9. `cmd/server/` のように途中に `/` を含む名前は、中間のディレクトリを省略した書き方として展開する。

自前の `.dirdoc` を持つサブディレクトリ（例の `agent/`）は、親の DSD にエントリを書かなくてよい。表示時に自動で印が付く（§10.2）。

**ファイルシステムとの照合**（compile 時と表示時に行う。いずれも警告）

| コード | 内容 |
|---|---|
| `tree-missing` | Tree に書かれたパスが存在しない |
| `tree-type` | ディレクトリ／ファイルの種別が実際と違う |
| `tree-unlisted` | 子を 1 つ以上列挙していて `...` も無いディレクトリに、書かれていないサブディレクトリがある（ファイルは対象外。.gitignore などで除外されるものも対象外） |

子を 1 つも列挙していないディレクトリは「畳んである」とみなし、中身は照合しない。`...` を付けたディレクトリは「一部だけ列挙している」とみなし、書かれていないものがあっても警告しない。

## 8. 元ファイル（TOML）

### 8.1 DSD の元ファイル

```toml
summary = "アプリ本体のソースコード。CLI とエージェント実行系からなる。"

tree = '''
./              [[#src]]
├── agent/                   # エージェントの起動と管理
├── cli/        [[#cli]]     # 引数解析とサブコマンド
└── main.rs                  # エントリポイント
'''

[[dsd]]
id = "src"
name = "ソースルート"
document = '''
クレートのルート。`main.rs` が [[cli/]] と [[agent/]] を束ねる。
'''

[[dsd]]
id = "cli"            # name を省略 → Tree 上の名前 "cli" になる
document = '''
clap の derive でコマンドを定義する。…
'''
```

### 8.2 PFD の元ファイル

```toml
summary = "CLIのエントリポイント。引数を解析して各サブコマンドへ委譲する。"

[[types]]
name = "Config"
summary = "コマンドライン引数と設定ファイルから組み立てる実行時設定"
document = '''
[[#main]] で一度だけ作り、各サブコマンドへ参照で渡す。
'''

[[functions]]
name = "main()"
summary = "プロセスのエントリポイント"
document = '''
[[#check_stdout]] で出力先を判定してから、[[agent/pool.rs#spawn]] でワーカーを起動する。
'''

[[functions]]
name = "check_stdout()"
summary = "標準出力がTTYかを判定する"
document = '''
TTYなら色付き出力を有効にする。上限値は [[#MAX_AGENTS]] を参照。

## 注意
Windows では …
'''

[[variables]]
name = "MAX_AGENTS"
summary = "同時に処理するエージェント数の上限（既定 8）"
```

### 8.3 ルール

- トップレベルに書けるキーは、DSD なら `summary` `tree` `dsd`、PFD なら `summary` `types` `functions` `variables`。任意で `kind = "dsd" | "pfd"` を書くと、対象の種別と照合する。
- 知らないキーはエラーにする（タイプミスの検出。近いキー名を提案する）。`x-` で始まるキーは許可して保持する。
- 本文は `'''`（リテラル文字列。エスケープ不要）で囲むことを推奨する。本文に `'''` が含まれるときだけ `"""` を使う。`export` はこのルールで自動的に使い分ける。
- `overview` と `target_hash` は書かない（compile が生成する）。
- プログラムから生成する場合に備えて、JSON（保存形式と同じ構造で、`overview` と `target_hash` は無し）も元ファイルとして受け付ける。

## 9. compile / export

### 9.1 compile

```text
dirdoc compile <TARGET> <SOURCE | -> [--merge] [--dry-run] [--strict]
```

- `TARGET` がディレクトリなら DSD、ファイルなら PFD として扱う。`TARGET` が存在しなければエラー。
- `SOURCE` に `-` を指定すると標準入力から読む（ヒアドキュメントで書ける）。

処理の流れ:

1. 元ファイルを読み、TOML（または JSON）として解析する
2. `--merge` なら既存のドキュメントとマージする（§9.2）
3. 正規化する: `id` の補完、改行と前後の空白の正規化、空フィールドの除去
4. 検証する。エラーが 1 つでもあれば書き込まない（行番号付きで報告する）
5. PFD なら Overview を生成し（解析できる言語のファイルはコード解析の結果も入れる。§10.6）、対象ファイルのハッシュを記録する
6. 保存する: JSON 化 → 圧縮 → 既存と同じならスキップ → `.dirdoc/` が無ければ作成 → アトミックに書き込み
7. 結果（書き込んだパス、件数、警告）を出力する。`--dry-run` では 6 を行わない

```text
warning[ref-broken] main.toml:14 (src/main.rs#main): cannot resolve [[agent/pool.rs#spawn]]: the file has no PFD (if it is not a reference, put it in backticks)
wrote src/.dirdoc/main.rs.dirdoc_pfd (types: 1, functions: 2, variables: 1)
```

- 診断は `重要度[コード] 元ファイル:行 (場所): メッセージ` の形で標準エラーに出す。結果の 1 行は標準出力に出す。
- TOML（または JSON）として読めれば、キーの誤りがあっても検証まで続けて、問題をまとめて報告する（直すたびに 1 件ずつ出てくるのを避けるため）。
- §12 のコードに加えて、compile だけが出すコードがある: `merge`（`--merge` で更新・削除する対象が無い、`delete` を `--merge` なしで使った）、`source-ignored`（`overview` などの生成されるフィールドを書いた）。
- 元ファイルに git の競合マーカー（`<<<<<<< ` / `>>>>>>> ` で始まる行）が残っていて TOML として読めないときは、その行を示して「未解決のマージ衝突」と報告する（§16）。

### 9.2 --merge（部分更新）

エージェントが一部だけ直すための仕組み。

- トップレベルの `summary` / `tree`: 入力に書いたものだけを置き換える（Tree はまるごと置き換える）。
- 配列（`dsd` / `types` / `functions` / `variables`）: `id`（省略時は name から導出）で既存のエントリと突き合わせる。
  - 既存にある → 入力に書いたフィールドだけを上書きする（`summary` だけ直す、などができる）
  - 既存に無い → 末尾に追加する（新規なので `name` と `summary` が必要）
  - `delete = true` を付けたエントリは削除する（`--merge` のときだけ使える）
- 既存の並び順は保つ。マージ後のドキュメント全体を、通常どおり検証する。

```bash
dirdoc compile src/main.rs --merge - <<'EOF'
[[functions]]
id = "check_stdout"
summary = "標準出力と標準エラーがTTYかを判定する"

[[functions]]
name = "load_config()"
summary = "設定ファイルを読み込む"

[[variables]]
id = "OLD_LIMIT"
delete = true
EOF
```

### 9.3 export

```text
dirdoc export <TARGET> [-o FILE] [--format toml|json]
```

- 既存のドキュメントを元ファイルの形式で出力する。`overview` と `target_hash` は出さず、`id` は常に明示する。先頭に対象を示すコメント（`# dirdoc pfd: src/main.rs`）を付ける。
- ドキュメントがまだ無ければ**ひな形**を出力する。
  - DSD: Tree を実際のファイルシステムから生成する（既定では直下のみ。`--depth N` で深くできる。.gitignore などで除外されるものは含めない）。サブディレクトリが自前の DSD を持っていれば、その summary をメモとして入れる。書き方の例をコメントで添える。
  - PFD: 空の summary / types / functions / variables と、書き方の例。
  - `[template]` で必須の見出しが決まっていれば、例の本文にその見出しを入れる（§17）。
- 往復の保証: `export` の出力をそのまま `compile` すると、同じ内容になる（テストで担保する）。

## 10. 表示（dirdoc [PATH]）

```text
dirdoc [show] [PATH[#ID]] [-f md|json] [--brief | --full] [-r] [--depth N]
```

### 10.1 PATH の解釈

| PATH | 表示 |
|---|---|
| 省略 | カレントディレクトリのディレクトリビュー |
| ディレクトリ | そのディレクトリのディレクトリビュー |
| ファイル | その PFD のファイルビュー |
| `PATH#ID` | エントリ単体のビュー |

- パスはカレントディレクトリからの相対パス（または絶対パス）で指定する。
- サブコマンド名と同じ名前のディレクトリを表示したいときは、`dirdoc show compile` か `dirdoc ./compile` とする。
- zsh の extendedglob などでは `#` が特別扱いされることがあるので、`#` を含む PATH はクォートを推奨する（`dirdoc 'src/main.rs#main'`）。

### 10.2 ディレクトリビュー

| 詳細度 | 内容 |
|---|---|
| `--brief` | summary、Tree、ファイル一覧（各 1 行の summary）、DSD を持つサブディレクトリの一覧（各 1 行の summary） |
| 既定 | 上記 + DSD エントリの本文 + 各ファイルの Overview（目次） |
| `--full` | 上記 + 各ファイルの型・関数・変数の本文 |

- Tree には表示時に印を付ける。`(pfd)` は PFD があるファイル、`(dirdoc)` は自前の `.dirdoc` を持つディレクトリ。
- Files では、最後に compile した後で変わったファイルに `(stale)` の印を付ける（§10.5）。
- 末尾に警告（Tree と実ファイルの食い違い、対象ファイルが無い PFD など）をまとめて出す。エージェントが「記述が古いかもしれない」と気付けるようにするため。
- `-r` でサブディレクトリも再帰的に表示する（`--depth N` で深さを制限）。表示するのは自前の `.dirdoc` を持つディレクトリだけ。

### 10.3 継承（上位ディレクトリの記述へのフォールバック）

自前の DSD を持たないディレクトリを表示するときは、次の順に探す。

1. 祖先ディレクトリを近い順にたどり、その DSD の Tree にこのディレクトリのノードがあり、そのノードに `dsd` か `note` があるかを調べる
2. 見つかれば、ノードが指すエントリ（と `note`）を「記述元: `<祖先>/.dirdoc` の `[[#id]]`」として表示する
3. どこにも無ければ「記述なし」として終了コード 3 で終わる。`dirdoc export <PATH>` でひな形を作れることを案内する

自前の DSD があればそちらを優先する。参照 `[[cli/]]` の解決もこの規則に従う（例では `src/cli/` に DSD が無いので、`src/` の DSD のエントリ `cli` に解決される）。

### 10.4 ファイルビューとエントリビュー

- ファイルビュー: （古ければ）stale の注意 → summary → Overview → Undocumented（記述の無いシンボル。§10.6）→ Types → Functions（シグネチャと本文付き）→ Variables → Referenced by（被参照）の順に出す。`--brief` なら summary と Overview だけ。
- エントリビュー: name・summary・本文に加えて、本文中の参照先の summary を一覧で添える（もう一度 dirdoc を呼ばなくても、リンク先の概要が分かる）。コード解析の結果があれば、定義の位置（`path:行`）とシグネチャを本文の前に出す。
- 被参照（Referenced by）: プロジェクトのすべての文書を走査し、表示している対象を指す参照を持つ場所（エントリか文書の summary）を一覧にする。エントリビューではそのエントリを指すもの、ファイルビューではそのファイルかそのエントリを指すもの（ファイル自身の PFD の中からの参照は除く）を出す。DSD エントリなら、自前の DSD を持たずにそのエントリで説明されるディレクトリ（§10.3）への参照も含める。JSON では `referenced_by`。

```markdown
## Referenced by

- [[src/agent/pool.rs#spawn]] → `#MAX_AGENTS`
```

### 10.5 古さの検知

- 表示時に対象ファイルの現在のハッシュを計算し、`target_hash` と違えば「最後に compile した後で対象ファイルが変わった」とみなす。
- ファイルビューとエントリビューでは先頭に 1 行の注意を出す（`> stale: src/main.rs has changed since this document was last compiled`）。ディレクトリビューでは、Files の該当ファイルに `(stale)` の印を付けるだけにする。
- `check` は既定では報告しない。`--stale` を付けたときだけ `stale` を報告する（§12）。`dirdoc status` は `stale` の状態として一覧に出す（§14）。
- ハッシュは compile（`--merge` を含む）のたびに更新される。コードは変わったが記述を直す必要が無かったときは、`dirdoc touch <FILE>` でハッシュ（とコード解析の結果）だけを更新する（§14）。
- `rm` とマージドライバは記述を変えても対象ファイルを読まないので、ハッシュはそのまま残す（§20.2）。
- `target_hash` が無い PFD（Phase 1 で作ったものなど）は判定しない（JSON の `stale` は `null`）。

### 10.6 コード解析

- compile と touch は対象ファイルのコードを解析し、シンボルを Overview に反映する（§5.3）。言語は拡張子で決め、拡張子で決まらないファイル（拡張子が無い、または `invoke-rc.d` のように名前に `.` があるスクリプト）は 1 行目の `#!` で決める（`sh`・`bash`・`dash`・`ash`・`ksh` はシェル、`python`・`python3` などは Python、`node` は JavaScript。`env` を挟んでもよい）。

| 言語 | 拡張子 | 解析器 |
|---|---|---|
| Go | `.go` | 標準の `go/parser` |
| Rust | `.rs` | 自前のスキャナ |
| TypeScript | `.ts` `.tsx` `.mts` `.cts` | 自前のスキャナ（JSX は `.tsx` だけ） |
| JavaScript | `.js` `.jsx` `.mjs` `.cjs` | 自前のスキャナ（JSX も読む） |
| Java | `.java` | 自前のスキャナ |
| Kotlin | `.kt` `.kts` | 自前のスキャナ |
| Python | `.py` `.pyi` | 自前のスキャナ |
| シェル | `.sh` `.bash` | 自前のスキャナ（POSIX sh と、bash の `function`・配列・`declare`） |

- Go 以外は言語ごとの小さなスキャナで読む（§20.3）。コメント、文字列（raw 文字列、テンプレートリテラル、テキストブロック、ヒアドキュメント、JSX など）、括弧の入れ子は正しく読み飛ばし、文法は宣言を見つけるのに要る分だけ解釈する。行番号はトークンの位置から取る。見慣れない書き方に出会うと、そのシンボルを落とすことはあっても失敗はしない。Go は構文エラーのときは何も足さない。どちらの場合も compile は失敗しない。
- 対象のシンボルは、トップレベルの宣言と型のメンバー（入れ子の型を含む）。関数の中は見ない。インスタンスのフィールドは数えない（ほとんど記述されないので、数えると記述の無いシンボルの一覧が埋もれる）。ID は言語ごとに次のとおり。

| 言語 | 型 | 関数 | 変数 |
|---|---|---|---|
| Go | `type` | 関数、メソッド（`Pool.Spawn`。ポインタや型パラメータは除く）。`init` は数えない | `var`・`const`（名前ごと。`_` は数えない） |
| Rust | `struct`・`enum`・`union`・`trait`・`type` | `fn`、`macro_rules!`。固有の impl の項目は `Pool::spawn`、トレイトの項目は `Runner::run`。トレイトの impl とインラインの `mod` の中は見ない | `const`・`static` |
| TypeScript / JavaScript | `class`・`interface`・`enum`・`type` | `function`、関数を値に持つ `const`・`let`・`var`、クラスのメソッド（`Pool.spawn`、コンストラクタは `Pool.constructor`）と関数を値に持つフィールド | `const`・`let`・`var`、クラスの static フィールド（`Pool.DEFAULT`） |
| Java | `class`・`interface`・`enum`・`record`・`@interface`（入れ子は `Outer.Inner`） | メソッド（`Pool.submit`）、コンストラクタ（`Pool.Pool`） | static フィールド（インターフェースのフィールドはすべて） |
| Kotlin | `class`・`interface`・`object`・`typealias` | `fun`（メンバーは `Pool.spawn`、拡張関数は `String.orBlank`、副コンストラクタは `Pool.constructor`） | トップレベルとオブジェクトの `val`・`var`（コンパニオンオブジェクトのメンバーはクラスのもの: `Pool.DEFAULT`） |
| Python | `class`、`type X = ...` | `def`（メソッドは `Pool.spawn`）、`lambda` を代入した名前 | モジュールでの代入（`if` や `try` の中を含み、`if __name__ == "__main__":` の中は除く）、値のあるクラス属性 |
| シェル | — | `name() { ... }`、`function name`（if などの中も。関数の中は除く） | トップレベル（関数・if・case・ループの外）での代入と、`export`・`readonly`・`declare`・`typeset` による代入 |

- 同じ ID のシンボルが複数あるとき（オーバーロード、getter と setter、条件ごとの定義）は最初のものだけを数える。ID 規則（§6.1）に合わない名前は数えない。
- エントリの ID がシンボルの ID と一致すれば、そのエントリに行番号とシグネチャが付く（セクションの違いは問わない）。
- シグネチャは宣言を 1 行にしたもの。関数は本体を、型は中身を省き（`type Config struct{ ... }`、`pub struct Config { ... }`）、長い値は `{...}` や `...` に縮める。
- 表示: Overview の各行に `L<行>`、ファイルビューに Undocumented の一覧と各エントリのシグネチャ、エントリビューに定義の位置とシグネチャを出す。コードブロックには言語名（`go`・`rust`・`typescript`・`javascript`・`java`・`kotlin`・`python`・`sh`）を付ける。
- `check --undocumented` は、記述のあるファイルについてその時点のコードを解析し直し、エントリの無いシンボルを `undocumented` として報告する（既定では報告しない。一部だけ記述するのは普通のことなので）。
- 解析は `internal/analyze` の 1 か所にまとめてある。言語を足すときは、そこにスキャナを加える。

## 11. 出力形式

### 11.1 Markdown（既定）

`dirdoc src`（カレントディレクトリは `myapp/`）の出力例:

````markdown
# src/

> アプリ本体のソースコード。CLI とエージェント実行系からなる。

## Tree

```text
./              [[src/#src]]
├── agent/      (dirdoc)        # エージェントの起動と管理
├── cli/        [[src/#cli]]    # 引数解析とサブコマンド
└── main.rs     (pfd)           # エントリポイント
```

## Directories

### ソースルート `src/#src`

クレートのルート。`main.rs` が [[src/cli/]] と [[src/agent/]] を束ねる。

### cli `src/#cli`

clap の derive でコマンドを定義する。…

## Files

### main.rs `src/main.rs`

> CLIのエントリポイント。引数を解析して各サブコマンドへ委譲する。

- type `Config` `#Config` L8 — コマンドライン引数と設定ファイルから組み立てる実行時設定
- fn `main()` `#main` L13 — プロセスのエントリポイント
- fn `check_stdout()` `#check_stdout` L19 — 標準出力がTTYかを判定する
- var `MAX_AGENTS` `#MAX_AGENTS` L5 — 同時に処理するエージェント数の上限（既定 8）

## Subdirectories

- `src/agent/` — エージェントの起動・監視・終了を担う。

## Warnings

- tree-missing `src/old/`: listed in the tree but does not exist
````

- 埋め込む本文の見出しレベルは自動で調整する。本文の中で一番上の見出しが、入れ物の見出しの 1 つ下になるようにずらす（エントリ見出しが `###` で本文が `##` から始まるなら、`##` → `####`）。6 を超える見出しは太字に変える。
- 参照はカレントディレクトリからの相対パスに書き換え、`[[…]]` の形のまま出す。

### 11.2 JSON（`-f json`）

保存形式をそのまま出すのではなく、表示時に計算した情報（補完した名前、パス、参照の解決結果、警告）を加えた「ビュー」を出す。ビューのスキーマもバージョンを持つ。

```json
{
  "dirdoc_view": 1,
  "kind": "directory",
  "path": "src/",
  "source": "src/.dirdoc/.dirdoc_dsd",
  "inherited_from": null,
  "summary": "アプリ本体のソースコード。…",
  "tree": { "name": ".", "type": "dir", "dsd": "src", "children": [
    { "name": "agent", "type": "dir", "has_dirdoc": true, "note": "エージェントの起動と管理" },
    { "name": "main.rs", "type": "file", "has_pfd": true, "note": "エントリポイント" }
  ]},
  "dsd": [
    { "id": "src", "ref": "src/#src", "name": "ソースルート", "paths": ["src/"], "document": "…" }
  ],
  "files": [
    { "path": "src/main.rs", "ref": "src/main.rs", "stale": false, "summary": "…",
      "overview": { "types": [], "functions": [], "variables": [] } }
  ],
  "subdirs": [ { "path": "src/agent/", "summary": "…" } ],
  "warnings": [ { "code": "tree-missing", "path": "src/old/", "message": "…" } ]
}
```

- `kind` は `directory` / `file` / `type` / `function` / `variable` / `dsd-entry` のいずれか（ほかに `list`、`check`、`search`、`status`）。
- `stale` は、判定できないとき（`target_hash` が無いとき、対象ファイルが読めないとき）は `null` になる。エントリビューでは判定できるときだけ `stale` を出す。
- コード解析の結果は、Overview の各項目とエントリの `line` / `signature`、Overview の `undocumented` として出す（§10.6）。被参照はファイルビュー（通常以上）とエントリビューの `referenced_by`。
- `-r` のときは `{"dirdoc_view": 1, "kind": "list", "views": [...]}` の形で、各ディレクトリのビューを前順に並べる（jq で扱いやすいように平たくする）。

## 12. 検証（check）

```text
dirdoc check [PATH] [--strict] [--stale] [--undocumented] [-f json]
```

- PATH（既定はプロジェクトルート）以下のすべての `.dirdoc` を検証する。ファイルを指定すると、その PFD だけを検証する。
- エラーがあれば終了コード 1。`--strict` では警告もエラーとして扱う（CI 用）。

| コード | 重要度 | 内容 |
|---|---|---|
| `parse` | error | 解凍・JSON・スキーマの不正 |
| `version` | error | 未対応のスキーマバージョン |
| `id-invalid` | error | ID の文字種・長さの違反 |
| `id-duplicate` | error | ドキュメント内での ID の重複 |
| `tree-syntax` | error | Tree 記法の誤り、兄弟の名前の重複 |
| `tree-link` | error | Tree の `[[#id]]` の指す先が無い |
| `ref-local` | error | `[[#id]]` の指す先が同じドキュメント内に無い |
| `ref-outside` | error | プロジェクトルートの外を指す参照 |
| `target-mismatch` | error | PFD の `target` とファイル名の不一致 |
| `ref-broken` | warning | 他のドキュメントへの参照が解決できない |
| `orphan-pfd` | warning | PFD の対象ファイルが存在しない |
| `tree-missing` / `tree-type` / `tree-unlisted` | warning | §7 を参照 |
| `dsd-unlinked` | warning | Tree から指されていない DSD エントリ |
| `summary-empty` / `summary-long` | warning | summary が空、または長すぎる（既定のしきい値は DSD 200 / PFD 400 / エントリ 120 文字。§15） |
| `unknown-file` | warning | `.dirdoc/` 内の不明なファイル |
| `template-heading` | warning | 本文に、`[template]` で必須とした見出しが無い（空の本文は対象外。§17） |
| `stale` | warning | 最後に compile した後で対象ファイルが変わった（`--stale` を付けたときだけ報告する。§10.5） |
| `undocumented` | warning | 記述のあるファイル（§10.6 の言語）に、エントリの無いシンボルがある（`--undocumented` を付けたときだけ報告する。§10.6） |

compile と表示でも同じ検証器を使い、それぞれの場面に関係するものだけを報告する。

## 13. 検索（search）

```text
dirdoc search <QUERY> [PATH] [--regex] [--in name,summary,document,note] [-f json] [--limit N]
```

- `.dirdoc_*` は圧縮されていて grep できないので、検索は dirdoc が提供する。
- PATH（既定はプロジェクトルート）以下のすべての記述を対象にする。
- ヒットしたエントリの参照・name・summary と、一致した箇所の抜粋を出す。大文字と小文字は、クエリに大文字が含まれるときだけ区別する（ripgrep の smart case と同じ）。

```text
src/main.rs#check_stdout  fn check_stdout() — 標準出力がTTYかを判定する
    …TTYなら色付き出力を有効にする。…
```

## 14. その他のコマンド

| コマンド | 内容 | 時期 |
|---|---|---|
| `dirdoc cat <FILE \| ->` | `.dirdoc_*` ファイルを直接指定して TOML に展開する（git の textconv やコンフリクト解消に使う） | Phase 1 |
| `dirdoc init [--git]` | `dirdoc.toml` を作る。`--git` を付けると `.gitattributes` への追記と diff ドライバのローカル設定も行う | Phase 1 |
| `dirdoc guide [--lang ja\|en]` | エージェント向けの使い方（AGENTS.md / CLAUDE.md に貼るための Markdown）を出力する（§17）。`--lang` を省略すると、環境変数 `LANG` が `ja` で始まるなら日本語、それ以外なら英語 | Phase 1 |
| `dirdoc edit <TARGET>` | export → `$EDITOR` → compile をまとめて行う（人間向け。エラーなら再編集を促す） | Phase 2 |
| `dirdoc rm <REF>...` | エントリまたはドキュメントを削除する（対象ファイルが既に無くても使える） | Phase 2 |
| `dirdoc status [PATH]` | どのディレクトリ・ファイルに DSD / PFD があるかを一覧にする | Phase 2 |
| `dirdoc touch <FILE>...` | 記述は変えずに `target_hash`（とコード解析の結果）だけを更新する（コードは変わったが記述を直す必要が無かったとき。§10.5） | Phase 2 |
| `dirdoc mv <SOURCE> <DEST>` | ファイルやディレクトリを記述ごと移動・リネームし、参照を書き換える | Phase 3 |
| `dirdoc merge-driver %O %A %B %P` | `.dirdoc_*` の 3-way マージ（git のマージドライバ。§16） | Phase 3 |
| `dirdoc mcp` | MCP サーバー（標準入出力）。dirdoc のコマンドをツールとして公開する | Phase 3 |

**edit**: export の結果（記述が無ければひな形）を一時ファイルに書き、`$VISUAL`、`$EDITOR`（どちらも無ければ `vi`）で開く。エディタが終わったら compile する。エラーがあれば診断を出して「Edit again? [Y/n]」と尋ね、やめたときは一時ファイルの場所を伝えて残す。何も変えずに閉じたら何も書かない。

**rm**: `PATH` で文書ごと（ディレクトリなら DSD、ファイルなら PFD）、`PATH#ID` でエントリを 1 つ消す。同じ文書の複数のエントリをまとめて指定でき、まとめて消してから検証する。消したことで同じ文書に新しくエラーが出る（`[[#id]]` が残る）なら何も消さない。DSD エントリを消すと、それを指す Tree のリンクも外す。他の文書からの参照は `ref-broken` の警告として知らせる。文書を消して `.dirdoc/` が空になれば、それも消す。

**status**: PATH（既定はプロジェクトルート）以下の、除外されていないディレクトリとファイルを 1 行ずつ状態付きで出す。`--only` で状態を絞り、`--depth` で深さを制限し、`-f json` で JSON にする。最後に状態ごとの数を出す。

| 状態 | 意味 |
|---|---|
| `dsd` | ディレクトリが自前の DSD を持つ |
| `inherited` | ディレクトリが祖先の Tree で説明されている（§10.3） |
| `pfd` | ファイルが PFD を持つ |
| `stale` | PFD を持つが、最後に compile した後でファイルが変わった |
| `orphan` | ファイルが無いのに PFD が残っている |
| `note` | PFD は無いが、祖先の Tree にメモがある |
| `none` | 記述が無い |

**mv**: SOURCE を DEST に移動する（DEST が既存のディレクトリならその中へ）。SOURCE が既に無く DEST があるとき（`git mv` やエディタで移動済み）は、記述だけを追従させる。`--dry-run` で変更点だけを出す。

- 移動したファイルの PFD は移動先の `.dirdoc/` に移し、`target` を新しい名前にする。ディレクトリの `.dirdoc/` はディレクトリと一緒に移る。
- すべての文書について、移動したパス（とその中）を指す参照を新しいパスに書き換える。移動した文書の相対参照は、同じ場所を指し続けるように書き換える。意味の変わらない参照は綴りを変えない。`/` で始まる参照は `/` で始まるまま、末尾の `/` と表示名も保つ。
- 祖先の DSD の Tree が移動元を載せていれば、そのノード（メモ・リンク・子ノードごと）を移動先へ移す。同じディレクトリ内のリネームなら位置を保ち、別の場所なら移動先の親の子に加える（Tree に無い途中のディレクトリは作るが、畳まれたディレクトリ（子を 1 つも載せていないもの）には加えない）。加えられないときや Tree の外へ出るときはノードを外し、メモやリンクがあれば警告する。
- 移動先に既に何かあるとき、ディレクトリを自分の中へ移すとき、プロジェクトの外へ出るときはエラー。

**mcp**: Model Context Protocol のサーバーを標準入出力で動かす（JSON-RPC 2.0、1 行 1 メッセージ）。ツールは `show`・`search`・`status`・`check`・`export`・`compile`・`touch`・`rm`・`mv` の 9 つで、それぞれ同名のコマンドを起動したディレクトリで実行し、その出力を返す（終了コードが 0 以外なら `isError`）。パスはプロジェクトの中に限る。使い方の要約を `instructions` として返す。例: `claude mcp add dirdoc -- dirdoc mcp`。

終了コード: `0` 成功 / `1` エラー（check の失敗を含む） / `2` 使い方の誤り / `3` 記述が無い（表示時）

メッセージの言語: エラー・警告・Markdown 出力の見出しなど、CLI が出す文言は英語にする（記述の本文は書かれたまま出す）。翻訳を用意するのは `guide` だけ。

## 15. 設定ファイル（dirdoc.toml、任意）

```toml
[storage]
compression = "zstd"   # "zstd" | "none"
level = "best"         # "fastest" | "default" | "better" | "best"

[walk]
gitignore = true                       # .gitignore に従う
ignore = ["target/", "node_modules/"]  # 追加で除外するパターン

[lint]
summary_max = { dsd = 200, pfd = 400, entry = 120 }   # summary の長さの警告しきい値（Unicode のコードポイント数）

[show]
format = "md"          # 既定の出力形式

[template]
function = ["Errors"]  # 関数の本文に必須の見出し（ほかに dsd・type。§17）
```

- プロジェクトルートの目印も兼ねる。
- `.git/` と `.dirdoc/` は常に走査の対象外。

## 16. Git での運用

`.dirdoc_*` はバイナリなので、そのままでは差分が読めず、テキストとしてはマージできない。`dirdoc init --git` で次を設定する。

```gitattributes
.dirdoc_dsd   -text merge=dirdoc diff=dirdoc
*.dirdoc_pfd  -text merge=dirdoc diff=dirdoc
```

```bash
git config diff.dirdoc.textconv "dirdoc cat"
git config diff.dirdoc.cachetextconv true
git config merge.dirdoc.name "dirdoc: merge .dirdoc_* files by entry id"
git config merge.dirdoc.driver "dirdoc merge-driver %O %A %B %P"
```

- これで `git diff` / `git log -p` が TOML の差分として表示され（PFD は `# target_hash:` のコメント行も出すので、`touch` だけの変更も差分に見える）、マージはマージドライバが行う。ただし `git config` はクローンごとの設定なので、各自が `dirdoc init --git` を実行する必要がある。Phase 1 の `-merge` の行は、`init --git` がその場で `merge=dirdoc` の行に置き換える。
- マージドライバ（`dirdoc merge-driver`）は共通の祖先・自分・相手の 3 つを読み、エントリを ID で突き合わせてフィールドごとに 3-way マージする。片方だけが変えたものはそちらを採り、追加されたエントリは相手側で直前にあったエントリの後ろに入る。Tree はノードのパスで突き合わせて同様にマージする。`target_hash` は片方だけが変えたならそちら、両方が変えたなら自分側を採る（マージ後のファイルはどちらとも違うので stale と表示され、確認を促す）。
- 両方が同じものを違うふうに変えたとき（同じフィールド、片方が変えて片方が消したエントリ、違うセクションに足された同じ ID、同じ Tree ノード）は衝突として、`.dirdoc_*` を **TOML の元ファイル** に置き換え、衝突した箇所を git と同じ競合マーカーで両側並べて書く。マージ結果に検証エラー（消されたエントリへの `[[#id]]` など）があるときも同様に元ファイルにして、エラーをコメントで添える。

```toml
# dirdoc pfd: src/main.rs
# Merge conflict: keep one side of each block between <<<<<<< ours and >>>>>>> theirs
# (or combine them) and delete the markers.
# Then store this file again with:
#   dirdoc compile src/main.rs src/.dirdoc/main.rs.dirdoc_pfd
<<<<<<< ours
summary = "main 側の要約"
=======
summary = "feature 側の要約"
>>>>>>> theirs
```

  解消の手順は git のテキストの衝突と同じで、ファイルを編集してマーカーを消し、表示どおり `dirdoc compile <対象> <その .dirdoc_* ファイル>` で保存し直してから `git add` する。マーカーが残っていれば compile はその行を示して失敗する。解消前のファイルは `dirdoc cat` ではそのまま表示され、他のコマンドからは「未解決のマージ衝突」として読み込みエラーになる。
- 既知の制約:
  - GitHub などの PR 画面ではバイナリ扱いのままで、レビュー時に差分が見えない。
  - マージドライバを設定していないクローンでは、git は既定のテキストマージを試みる。圧縮されたファイルはバイナリとして扱われて衝突になる（Phase 1 と同じ）が、`compression = "none"` の JSON は行単位でマージされうるので、非圧縮で運用するときは全員が `dirdoc init --git` を実行する。
  - 圧縮済みのデータには git のデルタ圧縮が効かないので、履歴のサイズは非圧縮より大きくなりうる（1 ファイル数 KB なので、実害は小さい見込み）。
- これらが問題になるプロジェクトは `compression = "none"` を選べる。

## 17. AI エージェント向けの運用

`dirdoc guide --lang ja` が出力する内容（要約。実際の文面は `internal/cli/guide_ja.md`）。AGENTS.md / CLAUDE.md に貼るか、「作業前に `dirdoc guide` を読むこと」と書いておく。MCP に対応したエージェントには `dirdoc mcp` をつなぐ方法もある（§14）。

```markdown
## dirdoc

このリポジトリでは、ディレクトリとファイルの説明を dirdoc で管理している。

読む:
- `dirdoc <dir>` でディレクトリの概要と各ファイルの目次、`dirdoc <file>` でファイルの詳細、
  `dirdoc '<file>#<id>'` で個々の型・関数の説明を読む
- `[[…]]` は参照。中身をそのまま `dirdoc '…'` に渡すと開ける
- 検索は `dirdoc search <語>`（`.dirdoc/` 内は圧縮されていて grep できない）
- `> stale:` で始まるビューはファイルの古い版の説明。`dirdoc status` で記述の有無と古さを一覧できる
- `.dirdoc/` 内のファイルを直接読み書きしない

書く（コードを変えたら記述も更新する）:
- 一部だけ: `dirdoc compile <file> --merge - <<'EOF' … EOF`
- 全体: `dirdoc export <file> > /tmp/x.toml` → 編集 → `dirdoc compile <file> /tmp/x.toml`
- 記述を直す必要が無ければ `dirdoc touch <file>`。移動は `dirdoc mv`、削除は `dirdoc rm`
- 最後に `dirdoc check <dir>` を通す
```

- document 本文の書き方（見出しの構成、書くべき項目など）は、プロジェクトが AGENTS.md で決める。dirdoc は関与しない。
- 本文テンプレート: 決めた見出しを機械的に守らせたいプロジェクトは、dirdoc.toml の `[template]` に種類（`dsd`・`type`・`function`）ごとの必須の見出しを書ける。`export` のひな形の例にその見出しが入り、compile と check は、本文があるのに見出しが無いエントリを `template-heading` として警告する。見出しは本文中のどのレベルでもよく、大文字と小文字は区別しない。本文が空のエントリは対象外（本文は任意なので）。

```toml
[template]
function = ["Errors"]   # 関数の本文には「Errors」の見出しを必ず書く
dsd = ["Usage"]
```

## 18. 実装構成（Go）

```text
dirdoc/
├── .github/workflows/        # release.yml: タグの push でクロスビルドし、Release を作る
├── go.mod
├── cmd/dirdoc/main.go        # エントリポイント
├── internal/
│   ├── cli/                  # サブコマンドの定義と入出力
│   ├── model/                # DSD / PFD / TreeNode / Entry の型と ID 規則、Overview とハッシュ
│   ├── container/            # zstd と JSON の判定・読み書き、サイズ上限、アトミック書き込み
│   ├── source/               # TOML の解析と出力（競合マーカー付きを含む）、Tree 記法のパーサ／プリンタ
│   ├── compile/              # 正規化、Overview とハッシュの生成、merge
│   ├── analyze/              # コード解析（Go は go/parser、ほかの言語は自前のスキャナ）
│   ├── config/               # dirdoc.toml の読み込みと既定値
│   ├── diag/                 # 検証器と診断（コード・重要度・位置）
│   ├── resolve/              # プロジェクトルートの探索、パスと参照の解決、継承、古さ、被参照
│   ├── walk/                 # ディレクトリの走査（.gitignore 対応）
│   ├── render/               # Markdown / JSON の出力、見出しレベルの調整
│   ├── merge/                # 3-way マージ（git のマージドライバ）
│   ├── move/                 # dirdoc mv の計画（PFD の移動、参照と Tree の書き換え）
│   └── mcp/                  # MCP サーバー（JSON-RPC 2.0 over stdio）
├── schema/                   # JSON Schema（dsd.v1 / pfd.v1 / view.v1）
└── testdata/myapp/           # 統合テスト: 元ファイル（sources/）、compile の順（compile.txt）、期待する出力（golden/）
```

| 用途 | ライブラリ（候補） |
|---|---|
| zstd | `github.com/klauspost/compress/zstd`（純 Go） |
| TOML の解析 | `github.com/pelletier/go-toml/v2`（未知キーの検出、行番号付きのエラー） |
| TOML の出力 | 自前の小さな書き出し器（`'''` の使い分け、キー順、ひな形のコメントを制御するため） |
| Markdown | `github.com/yuin/goldmark`（AST からコード部分と見出しの位置を取り、元のテキストを書き換える） |
| CLI | `github.com/spf13/cobra` |
| .gitignore | 自前の実装（gitignore のパターンを正規表現に変換する。依存を増やさないため） |
| JSON Schema の検証（テストのみ） | `github.com/santhosh-tekuri/jsonschema/v6` |
| Go のコード解析 | 標準の `go/parser`・`go/printer` |
| ほかの言語のコード解析 | 自前のスキャナ（依存を増やさず、純 Go のまま。§20.3） |
| MCP | 自前の小さな実装（JSON-RPC 2.0、tools のみ。依存を増やさないため） |

テスト:

- ゴールデンテスト: `testdata/myapp/` の元ファイルを一時ディレクトリで compile し、各コマンドの出力（md / json）を `golden/` と比べる。JSON の出力と保存ファイルは JSON Schema でも検証する。出力を変えたときは `go test ./internal/cli -update` で更新する
- 往復テスト: export → compile で同じ JSON になること
- 決定性: 同じ入力から同じ JSON が得られること
- Tree 記法のパーサは Go 標準のファジングにかける
- コード解析は言語ごとのテストで確かめ、すべての解析器をファジングにかける（パニックも無限ループもせず、ID と行番号が正しいこと）
- Phase 2・3 のコマンド（status / touch / rm / edit / mv / merge-driver / mcp / init --git）は `internal/cli` のテストで実際に動かして確かめる。edit はシェルスクリプトのエディタ、init --git は本物の git を使う（無ければ飛ばす）

リリース:

- `v` で始まるタグ（`v1.2.0`）を push すると、`.github/workflows/release.yml` がテストのあと、`go tool dist list` のプラットフォームのうち、cgo が要るもの（android の 386・amd64・arm と ios）と JavaScript のホストが要る js/wasm を除くすべてに向けて `CGO_ENABLED=0` でビルドする。ARM は ARMv6 以降、MIPS は浮動小数点ユニットの無いものでも動くようにビルドする
- アーカイブ（Windows は zip、ほかは tar.gz。README と LICENSE を同梱）と SHA-256 のチェックサムを GitHub の Release に載せる。`-` を含むタグ（`v1.2.0-rc.1`）はプレリリースにする。アーカイブの時刻はタグのコミットの時刻にそろえ、同じタグからは同じファイルができる
- バージョンはタグから `-ldflags -X` で埋め込む。`go install` で入れたものは、Go が記録するモジュールのバージョン（`go install ...@v1.2.0` なら 1.2.0）を表示する

## 19. ロードマップ

**Phase 1（MVP）— 実装済み**

- コンテナ（zstd / JSON の判定と読み書き）、スキーマ（PFD の `types` を含む）、ID 規則
- 元ファイル（TOML）と Tree 記法、`compile`（`--merge` を含む）、`export`（ひな形を含む）
- 表示（ディレクトリ／ファイル／エントリのビュー、継承、`-r`、Markdown / JSON）
- `check`、`search`、`cat`、`init`、`guide`（日本語・英語）
- JSON Schema
- dirdoc 自身のリポジトリを dirdoc で記述する（ドッグフーディング）

**Phase 2 — 実装済み**

- `edit`、`rm`、`status`（§14）
- 古さの検知（`target_hash` の記録、表示時の注意、`check --stale`、`touch`。§10.5）
- 本文テンプレート（`[template]` の必須見出し。§17）

**Phase 3 — 実装済み**

- コード解析による Overview の拡張（行番号、シグネチャ、記述の無いシンボルの検出、`check --undocumented`。§10.6）。Go のコードは標準の `go/parser` で解析する（ほかの言語は次の段階で加えた）
- git のマージドライバ（ID 単位の 3-way マージ。§16）
- MCP サーバーモード（`dirdoc mcp`。show / search / compile に加え、status / check / export / touch / rm / mv をツールとして公開する。§14）
- リネームの支援（`dirdoc mv` で PFD の移動と参照・Tree の書き換え）、被参照（バックリンク）の表示（§10.4）

**解析する言語の追加 — 実装済み**

- Rust、TypeScript、JavaScript、Java、Kotlin、Python、シェル（POSIX sh と bash の一部）。言語ごとの自前のスキャナで読む（§10.6、§20.3）

**今後の候補**

- ほかの言語の解析（C / C++、C#、Ruby、PHP、Swift など）と、今は見ていない入れ子（Rust のインラインの `mod`、TypeScript の `namespace`）
- 被参照の索引（大きなプロジェクトで表示のたびに全文書を走査しないように）

## 20. 決定の記録

### 20.1 v0.1 の未決事項

v0.1 で未決だった事項は、2026-10-25 にすべて推奨案で確定した。

| # | 論点 | 決定 |
|---|---|---|
| 1 | 型（クラス・構造体・インターフェース）の扱い | PFD に任意の `types` セクションを追加する（§5.3）。メソッドは functions に `Pool::spawn()` のように書く。あわせて、ID の導出で `<`・`[` から後ろも捨てるようにした（§6.1。`Pool<T>` → `Pool`） |
| 2 | 古さの検知 | Phase 2 で採用する（§10.5）。表示時は 1 行の注意だけ。`check` は `--stale` を付けたときだけ報告する。記述を直さずにハッシュだけ更新する `dirdoc touch` を追加した |
| 3 | DSD 配列のキー名 | 仕様どおり `dsd` |
| 4 | summary の長さの上限 | DSD 200 / PFD 400 / エントリ 120 文字。超えたら警告するだけで、設定で変えられる（§15） |
| 5 | 表示の既定の詳細度 | ディレクトリを指定したときは各ファイルの目次まで（§10.2）。本文は `--full` かファイル指定で読む |
| 6 | 本文テンプレート | MVP には入れず、Phase 2 で検討する |
| 7 | メッセージの言語 | CLI のメッセージは英語。`guide` は日本語と英語を用意し、`--lang` を省略したときは `LANG` に従う（§14） |

### 20.2 Phase 2・3 の実装で決めたこと

2026-10-26、Phase 2・3 の実装にあたって決めた。

| # | 論点 | 決定 |
|---|---|---|
| 8 | 本文テンプレート | 本文全体のひな形ではなく、種類（dsd・type・function）ごとの「必須の見出し」だけを `[template]` に書く。export のひな形の例に入れ、compile と check が `template-heading` で警告する（エラーにはしない）。本文が空のエントリは対象外。書き方そのものは引き続きプロジェクトが決める（§1 の非目的） |
| 9 | 古さの注意を出す場所 | ファイルビューに加えてエントリビューにも出す（関数 1 つだけを読むエージェントにも伝わるように）。ディレクトリビューは `(stale)` の印だけ |
| 10 | ハッシュを更新する操作 | compile（`--merge` を含む）と touch だけ。rm とマージドライバは記述しか変えず、対象ファイルを確かめていないので、ハッシュはそのまま残す。touch はコード解析の結果（行番号など）も更新する |
| 11 | コード解析の対象言語 | Go だけ（`go/parser`）。tree-sitter は cgo か、WASM ランタイムと各言語の文法の同梱が必要で、「純 Go の単一バイナリ」（§0）に反するので見送った。正規表現による簡易解析は、誤った行番号や「記述の無いシンボル」を出しうるので採らない。解析器は `internal/analyze` に差し替えられる形でまとめた（→ 2026-10-27 に #18 で改めた） |
| 12 | 記述の無いシンボル | compile 時に Overview の `undocumented` に保存してファイルビューに出す。`check` は既定では報告せず、`--undocumented` のときだけその時点のコードで判定する（一部だけ記述するのが普通なので） |
| 13 | 被参照の求め方 | 索引は作らず、表示のたびにプロジェクトの全文書を走査する（check・search と同じ規模）。ディレクトリビューには出さない |
| 14 | mv が動かすもの | ファイル（ディレクトリ）自体も移動する。既に移動済みなら記述だけを追従させる。参照は意味が変わるものだけを書き換え、綴りの流儀（相対・ルートから、末尾の `/`、表示名）を保つ。Tree は畳まれたディレクトリを勝手に展開しない |
| 15 | マージの衝突の表し方 | 圧縮ファイルには競合マーカーを入れられないので、衝突したら `.dirdoc_*` を TOML の元ファイル（競合マーカー付き）に置き換える。解消は git のテキストの衝突と同じ手順で、最後に compile で保存し直す。マージ結果の検証エラーも同じ扱いにする |
| 16 | MCP のツール | 設計の show / search / compile に、エージェントの作業に必要な status / check / export / touch / rm / mv を足した 9 つ。CLI と同じコマンドを実行して結果を返すので、出力も検証も CLI と一致する。パスは起動したプロジェクトの中に限る |
| 17 | rm の安全策 | 同じ文書に新しいエラーを生む削除は拒否する（既にあったエラーでは拒否しない）。他の文書からの参照は警告だけにする（相手の文書を勝手に書き換えない） |

### 20.3 解析する言語の追加で決めたこと

2026-10-27、Rust・TypeScript・JavaScript・Java・Kotlin・Python・シェルの解析を加えるにあたって決めた。

| # | 論点 | 決定 |
|---|---|---|
| 18 | Go 以外の解析器 | 言語ごとに自前の小さなスキャナを書く（#11 を改めた）。tree-sitter を見送った理由（cgo、または WASM ランタイムと文法の同梱）は変わらないので、純 Go のまま依存も増やさない。#11 が退けた正規表現と違い、字句解析はきちんと行う。コメント（入れ子を含む）、文字列（raw 文字列、テンプレートリテラルの `${...}`、テキストブロック、ヒアドキュメント、JSX など）、括弧の入れ子を読み飛ばすので、行番号はトークンの位置から正確に取れ、コメントや文字列の中をシンボルと見誤らない。構文は宣言を見つけるのに要る分だけ解釈し、読めない書き方ではそのシンボルを落とすことはあっても失敗はしない。これをファジングで確かめる。実装時に、Python の標準ライブラリ（672 ファイル）を `ast` モジュールの結果と、node_modules の JavaScript / TypeScript（4,933 ファイル。圧縮済みのバンドルを含む）を TypeScript のコンパイラの結果と突き合わせ、ID・種類・行がすべて一致することを確かめた |
| 19 | 解析の範囲 | トップレベルの宣言と型のメンバー（入れ子の型を含む）。関数の中、Rust のトレイトの impl とインラインの `mod`、TypeScript の `namespace` と `declare module` の中は見ない。インスタンスのフィールド（Kotlin のクラスのプロパティを含む）は数えず、static なフィールドなど型の外から使うものだけを数える（関数を値に持つ TypeScript / JavaScript のフィールドはメソッドとして数える）。ほとんど記述されないものが「記述の無いシンボル」に並ぶと、本当に記述したいものが埋もれるため |
| 20 | メンバーの ID | 各言語の書き方に合わせる。Rust は `Pool::spawn`（#1 の書き方と同じ）、ほかは `Pool.spawn`。Java のコンストラクタは `Pool.Pool`、TypeScript / JavaScript のコンストラクタと Kotlin の副コンストラクタは `Pool.constructor`。同じ ID が複数あるとき（オーバーロード、getter と setter）は最初の 1 つだけを数え、エントリはまとめて 1 つ書く |
| 21 | 言語の判定 | 拡張子で決め、拡張子で決まらないファイルは `#!` の行で決める（`check --undocumented` は記述のあるファイルをすべて読む。`--stale` がハッシュのために読むのと同じ量）。JSX は `.js`・`.jsx`・`.tsx` で読む。`.ts` では `<T>x` が型アサーションなので読まない |

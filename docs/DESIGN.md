# dirdoc 設計書（v0.2）

> ステータス: Phase 1（MVP）を実装済み（2026-10-26）。決定の経緯は §20 に記録する。
> 実装中に決めた細部（§7 のルート行と制御文字、§9.1 の compile の出力、§18 の構成とテスト）も反映してある。

## 0. 決定済み事項

| 項目 | 決定 | 備考 |
|---|---|---|
| 正（Source of Truth） | `.dirdoc_*`（zstd 圧縮 JSON）。git で管理するのもこれだけ | 元ファイルは一時的なもの |
| 編集フロー | `export` でテキスト化 → 編集 → `compile` で検証して書き戻す | `--merge` で部分更新、標準入力も可 |
| 元ファイルの記法 | TOML（JSON も入力可） | Tree は TOML 文字列の中に独自記法で書く |
| Overview | compile 時に Types / Functions / Variables から生成 | コード解析は将来の拡張 |
| 実装言語 | Go | zstd は純 Go 実装を使い、cgo 不要 |
| 型の記述 | PFD に任意の `types` セクションを追加する | 元の仕様からの追加。メソッドは functions に `Pool::spawn()` のように書く |
| 古さの検知 | PFD に対象ファイルのハッシュを記録する（Phase 2） | 表示時に 1 行の注意を出すだけ。`check` は `--stale` を付けたときだけ報告する |
| DSD 配列のキー名 | 仕様どおり `dsd` | |
| summary の長さ | DSD 200 / PFD 400 / エントリ 120 文字 | 超えたら警告するだけ。設定で変えられる |
| 表示の既定の詳細度 | ディレクトリを指定したときは各ファイルの目次まで | 本文は `--full` かファイル指定で読む |
| 本文テンプレート | MVP には入れない | Phase 2 で検討する |
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
- 保存するのは「正規化した記述内容」と「compile が生成する情報（Overview と対象ファイルのハッシュ）」だけ。名前の補完や逆引きなど、ファイルシステムや他のドキュメントに依存する情報は表示時に計算する（§10）。

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
| `target_hash` | string | 生成 | compile した時点の対象ファイルのハッシュ（`sha256:<16進>`。改行を LF にそろえてから計算する）。古さの検知に使う（§10.5）。Phase 2 から書き込む |
| `summary` | string | 推奨 | ファイルの役割・目的 |
| `overview` | object | 生成 | compile が生成する目次。元ファイルには書かない（書いても警告して無視する） |
| `types` | TypeEntry[] | 任意 | 元の仕様からの追加 |
| `functions` | FunctionEntry[] | 任意 | |
| `variables` | VariableEntry[] | 任意 | |

- **TypeEntry**: `name`（必須）、`summary`（必須）、`id`（省略時は name から導出）、`document`（任意、Markdown）。クラス・構造体・インターフェース・列挙型などを書く。メソッドは types の中に入れ子にせず、functions に `Pool::spawn()` のように型名を付けて書く。
- **FunctionEntry**: `name`（必須）、`summary`（必須）、`id`（省略時は name から導出）、`document`（任意、Markdown）
- **VariableEntry**: `name`（必須）、`summary`（必須）、`id`（省略時は name から導出）。仕様どおり `document` は持たない。
- **overview**: `types` / `functions` / `variables` のそれぞれについて、記述順に `{id, name, summary}` を並べたもの。将来コード解析を入れるときは、ここに `line`（行番号）、`signature`、`undocumented`（記述の無いシンボルの一覧）を足す。

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
5. PFD なら Overview を生成し、対象ファイルのハッシュを記録する（ハッシュの記録は Phase 2 から）
6. 保存する: JSON 化 → 圧縮 → 既存と同じならスキップ → `.dirdoc/` が無ければ作成 → アトミックに書き込み
7. 結果（書き込んだパス、件数、警告）を出力する。`--dry-run` では 6 を行わない

```text
warning[ref-broken] main.toml:14 (src/main.rs#main): cannot resolve [[agent/pool.rs#spawn]]: the file has no PFD (if it is not a reference, put it in backticks)
wrote src/.dirdoc/main.rs.dirdoc_pfd (types: 1, functions: 2, variables: 1)
```

- 診断は `重要度[コード] 元ファイル:行 (場所): メッセージ` の形で標準エラーに出す。結果の 1 行は標準出力に出す。
- TOML（または JSON）として読めれば、キーの誤りがあっても検証まで続けて、問題をまとめて報告する（直すたびに 1 件ずつ出てくるのを避けるため）。
- §12 のコードに加えて、compile だけが出すコードがある: `merge`（`--merge` で更新・削除する対象が無い、`delete` を `--merge` なしで使った）、`source-ignored`（`overview` などの生成されるフィールドを書いた）。

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
- 末尾に警告（Tree と実ファイルの食い違い、対象ファイルが無い PFD など）をまとめて出す。エージェントが「記述が古いかもしれない」と気付けるようにするため。
- `-r` でサブディレクトリも再帰的に表示する（`--depth N` で深さを制限）。表示するのは自前の `.dirdoc` を持つディレクトリだけ。

### 10.3 継承（上位ディレクトリの記述へのフォールバック）

自前の DSD を持たないディレクトリを表示するときは、次の順に探す。

1. 祖先ディレクトリを近い順にたどり、その DSD の Tree にこのディレクトリのノードがあり、そのノードに `dsd` か `note` があるかを調べる
2. 見つかれば、ノードが指すエントリ（と `note`）を「記述元: `<祖先>/.dirdoc` の `[[#id]]`」として表示する
3. どこにも無ければ「記述なし」として終了コード 3 で終わる。`dirdoc export <PATH>` でひな形を作れることを案内する

自前の DSD があればそちらを優先する。参照 `[[cli/]]` の解決もこの規則に従う（例では `src/cli/` に DSD が無いので、`src/` の DSD のエントリ `cli` に解決される）。

### 10.4 ファイルビューとエントリビュー

- ファイルビュー: summary → Overview → Types → Functions（本文付き）→ Variables の順に出す。`--brief` なら summary と Overview だけ。
- エントリビュー: name・summary・本文に加えて、本文中の参照先の summary を一覧で添える（もう一度 dirdoc を呼ばなくても、リンク先の概要が分かる）。

### 10.5 古さの検知（Phase 2）

- 表示時に対象ファイルの現在のハッシュを計算し、`target_hash` と違えば「最後に compile した後で対象ファイルが変わった」とみなす。
- ファイルビューでは先頭に 1 行の注意を出す（`> stale: src/main.rs has changed since this document was last compiled`）。ディレクトリビューでは、Files の該当ファイルに `(stale)` の印を付けるだけにする。
- `check` は既定では報告しない。`--stale` を付けたときだけ `stale` を報告する（§12）。
- ハッシュは compile（`--merge` を含む）のたびに更新される。コードは変わったが記述を直す必要が無かったときは、`dirdoc touch <TARGET>` でハッシュだけを更新する（§14）。
- `target_hash` が無い PFD（Phase 1 で作ったものなど）は判定しない。

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

- type `Config` `#Config` — コマンドライン引数と設定ファイルから組み立てる実行時設定
- fn `main()` `#main` — プロセスのエントリポイント
- fn `check_stdout()` `#check_stdout` — 標準出力がTTYかを判定する
- var `MAX_AGENTS` `#MAX_AGENTS` — 同時に処理するエージェント数の上限（既定 8）

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

- `kind` は `directory` / `file` / `type` / `function` / `variable` / `dsd-entry` のいずれか。
- `stale` は、判定できないとき（`target_hash` が無いとき）は `null` になる。
- `-r` のときは `{"dirdoc_view": 1, "kind": "list", "views": [...]}` の形で、各ディレクトリのビューを前順に並べる（jq で扱いやすいように平たくする）。

## 12. 検証（check）

```text
dirdoc check [PATH] [--strict] [--stale] [-f json]
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
| `stale` | warning | 最後に compile した後で対象ファイルが変わった（`--stale` を付けたときだけ報告する。Phase 2） |

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
| `dirdoc rm <REF>` | エントリまたはドキュメントを削除する（対象ファイルが既に無くても使える） | Phase 2 |
| `dirdoc status [PATH]` | どのディレクトリ・ファイルに DSD / PFD があるかを一覧にする | Phase 2 |
| `dirdoc touch <TARGET>` | 記述は変えずに `target_hash` だけを更新する（コードは変わったが記述を直す必要が無かったとき。§10.5） | Phase 2 |

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
```

- プロジェクトルートの目印も兼ねる。
- `.git/` と `.dirdoc/` は常に走査の対象外。

## 16. Git での運用

`.dirdoc_*` はバイナリなので、そのままでは差分が読めない。`dirdoc init --git` で次を設定する。

```gitattributes
.dirdoc_dsd   -text -merge diff=dirdoc
*.dirdoc_pfd  -text -merge diff=dirdoc
```

```bash
git config diff.dirdoc.textconv "dirdoc cat"
git config diff.dirdoc.cachetextconv true
```

- これで `git diff` / `git log -p` が TOML の差分として表示される。ただし `git config` はクローンごとの設定なので、各自が `dirdoc init --git` を実行する必要がある。
- 既知の制約:
  - GitHub などの PR 画面ではバイナリ扱いのままで、レビュー時に差分が見えない。
  - 同じファイルを別々のブランチで編集するとコンフリクトし、自動ではマージできない。当面は「`git show :2:<path> | dirdoc cat -` と `:3:` で両方を展開 → 手でマージ → `compile`」で解消する。Phase 3 で ID 単位の 3-way マージドライバを提供する。
  - 圧縮済みのデータには git のデルタ圧縮が効かないので、履歴のサイズは非圧縮より大きくなりうる（1 ファイル数 KB なので、実害は小さい見込み）。
- これらが問題になるプロジェクトは `compression = "none"` を選べる。

## 17. AI エージェント向けの運用

`dirdoc guide --lang ja` が出力する内容（案）。AGENTS.md / CLAUDE.md に貼るか、「作業前に `dirdoc guide` を読むこと」と書いておく。

```markdown
## dirdoc

このリポジトリでは、ディレクトリとファイルの説明を dirdoc で管理している。

読む:
- `dirdoc <dir>` でディレクトリの概要と各ファイルの目次、`dirdoc <file>` でファイルの詳細、
  `dirdoc '<file>#<id>'` で個々の型・関数の説明を読む
- `[[…]]` は参照。中身をそのまま `dirdoc '…'` に渡すと開ける
- 検索は `dirdoc search <語>`（`.dirdoc/` 内は圧縮されていて grep できない）
- `.dirdoc/` 内のファイルを直接読み書きしない

書く（コードを変えたら記述も更新する）:
- 一部だけ: `dirdoc compile <file> --merge - <<'EOF' … EOF`
- 全体: `dirdoc export <file> > /tmp/x.toml` → 編集 → `dirdoc compile <file> /tmp/x.toml`
- 最後に `dirdoc check <dir>` を通す
```

- document 本文の書き方（見出しの構成、書くべき項目など）は、プロジェクトが AGENTS.md で決める。dirdoc は関与しない。
- 本文テンプレート（dirdoc.toml で本文の必須見出しなどを定義し、ひな形と check に使う機能）は MVP には入れず、Phase 2 で検討する。

## 18. 実装構成（Go）

```text
dirdoc/
├── go.mod
├── cmd/dirdoc/main.go        # エントリポイント
├── internal/
│   ├── cli/                  # サブコマンドの定義と入出力
│   ├── model/                # DSD / PFD / TreeNode / Entry の型と ID 規則
│   ├── container/            # zstd と JSON の判定・読み書き、サイズ上限、アトミック書き込み
│   ├── source/               # TOML の解析と出力、Tree 記法のパーサ／プリンタ
│   ├── compile/              # 正規化、Overview の生成、merge
│   ├── config/               # dirdoc.toml の読み込みと既定値
│   ├── diag/                 # 検証器と診断（コード・重要度・位置）
│   ├── resolve/              # プロジェクトルートの探索、パスと参照の解決、継承
│   ├── walk/                 # ディレクトリの走査（.gitignore 対応）
│   └── render/               # Markdown / JSON の出力、見出しレベルの調整
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

テスト:

- ゴールデンテスト: `testdata/myapp/` の元ファイルを一時ディレクトリで compile し、各コマンドの出力（md / json）を `golden/` と比べる。JSON の出力と保存ファイルは JSON Schema でも検証する。出力を変えたときは `go test ./internal/cli -update` で更新する
- 往復テスト: export → compile で同じ JSON になること
- 決定性: 同じ入力から同じ JSON が得られること
- Tree 記法のパーサは Go 標準のファジングにかける

## 19. ロードマップ

**Phase 1（MVP）— 実装済み**

- コンテナ（zstd / JSON の判定と読み書き）、スキーマ（PFD の `types` を含む）、ID 規則
- 元ファイル（TOML）と Tree 記法、`compile`（`--merge` を含む）、`export`（ひな形を含む）
- 表示（ディレクトリ／ファイル／エントリのビュー、継承、`-r`、Markdown / JSON）
- `check`、`search`、`cat`、`init`、`guide`（日本語・英語）
- JSON Schema
- dirdoc 自身のリポジトリを dirdoc で記述する（ドッグフーディング）

**Phase 2**

- `edit`、`rm`、`status`
- 古さの検知（`target_hash` の記録、表示時の注意、`check --stale`、`touch`）
- 本文テンプレートの検討

**Phase 3**

- コード解析による Overview の拡張（行番号、シグネチャ、記述の無いシンボルの検出）。Go のコードは標準の `go/parser`、他の言語は tree-sitter（cgo か WASM 経由）で解析する
- git のマージドライバ（ID 単位の 3-way マージ）
- MCP サーバーモード（`dirdoc mcp`。show / search / compile をツールとして公開する）
- リネームの支援（`dirdoc mv` で PFD の移動と参照の書き換え）、被参照（バックリンク）の表示

## 20. 決定の記録

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

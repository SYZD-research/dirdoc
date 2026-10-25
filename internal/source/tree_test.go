package source

import (
	"strings"
	"testing"

	"dirdoc/internal/model"
)

// outline flattens a tree for comparison: one "path type [link] (note) +more" per node.
func outline(root *model.TreeNode) string {
	var b strings.Builder
	root.Walk(func(n *model.TreeNode, rel string) {
		if rel == "" {
			rel = "."
		}
		b.WriteString(rel + " " + n.Type)
		if n.DSD != "" {
			b.WriteString(" [" + n.DSD + "]")
		}
		if n.Note != "" {
			b.WriteString(" (" + n.Note + ")")
		}
		if n.More {
			b.WriteString(" +more")
		}
		b.WriteString("\n")
	})
	return b.String()
}

const wantOutline = `. dir [src]
agent dir (エージェントの起動と管理)
cli dir [cli] (引数解析とサブコマンド)
cli/args.rs file
main.rs file (エントリポイント)
`

func TestParseTreeNotations(t *testing.T) {
	inputs := map[string]string{
		"unicode": `
./                [[#src]]
├── agent/                     # エージェントの起動と管理
├── cli/          [[#cli]]     # 引数解析とサブコマンド
│   └── args.rs
└── main.rs                    # エントリポイント
`,
		"tree with nbsp": "./ [[#src]]\n├──\u00a0agent/ # エージェントの起動と管理\n├── cli/ [[#cli]] # 引数解析とサブコマンド\n│\u00a0\u00a0 └── args.rs\n└── main.rs # エントリポイント\n",
		"ascii":          "./ [[#src]]\n|-- agent/ # エージェントの起動と管理\n|-- cli/ [[#cli]] # 引数解析とサブコマンド\n|   `-- args.rs\n`-- main.rs # エントリポイント\n",
		"two spaces":     "./ [[#src]]\n  agent/ # エージェントの起動と管理\n  cli/[[#cli]] # 引数解析とサブコマンド\n    args.rs\n  main.rs # エントリポイント\n",
		"shorthand":      "./ [[#src]]\nagent/ # エージェントの起動と管理\ncli/args.rs\ncli/ [[#cli]] # 引数解析とサブコマンド\nmain.rs # エントリポイント\n",
	}
	for name, in := range inputs {
		root, _, issues := ParseTree(in)
		if len(issues) > 0 {
			t.Errorf("%s: issues %v", name, issues)
			continue
		}
		if got := outline(root); got != wantOutline {
			t.Errorf("%s:\n%s", name, got)
		}
	}
}

func TestParseTreeDetails(t *testing.T) {
	root, _, issues := ParseTree(`
"my dir/"      # spaced
+page.svelte
-flag.txt
cmd/server/    [[#server]]
cmd/client/
src/
  ...
  lib.rs
`)
	if len(issues) > 0 {
		t.Fatalf("issues: %v", issues)
	}
	want := `. dir
my dir dir (spaced)
+page.svelte file
-flag.txt file
cmd dir
cmd/server dir [server]
cmd/client dir
src dir +more
src/lib.rs file
`
	if got := outline(root); got != want {
		t.Errorf("got:\n%s", got)
	}
}

func TestParseTreeErrors(t *testing.T) {
	tests := map[string]string{
		"tab":          "./\n\tsrc/\n",
		"misaligned":   "src/\n    a/\n  b/\n",
		"file child":   "main.rs\n  sub/\n",
		"duplicate":    "a/\nb/\na/\n",
		"bad link":     "a/ [[cli]]\n",
		"other doc":    "a/ [[x.rs#cli]]\n",
		"unquoted":     "my dir/\n",
		"dot segment":  "./\n├── ../up/\n",
		"under root":   "  ./\nsrc/\n",
		"elision link": "a/\n  ... [[#x]]\n",
		"type clash":   "a/b\na/b/\n",
	}
	for name, in := range tests {
		if _, _, issues := ParseTree(in); len(issues) == 0 {
			t.Errorf("%s: no issue reported for %q", name, in)
		}
	}
	_, _, issues := ParseTree("./\n├── ok/\n├── bad name/\n")
	if len(issues) != 1 || issues[0].Line != 3 {
		t.Errorf("line numbers: %+v", issues)
	}
}

func TestFormatTreeRoundTrip(t *testing.T) {
	in := `./                [[#src]]
├── agent/                     # エージェントの起動と管理
├── cli/          [[#cli]]     # 引数解析とサブコマンド
│   ├── args.rs
│   └── ...
├── "my dir/"
└── main.rs                    # エントリポイント`
	root, _, issues := ParseTree(in)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	out := FormatTree(root, LinkAnnotator)
	want := `./           [[#src]]
├── agent/             # エージェントの起動と管理
├── cli/     [[#cli]]  # 引数解析とサブコマンド
│   ├── args.rs
│   └── ...
├── "my dir/"
└── main.rs            # エントリポイント`
	if out != want {
		t.Errorf("FormatTree:\n%s", out)
	}
	again, _, issues := ParseTree(out)
	if len(issues) > 0 || outline(again) != outline(root) {
		t.Errorf("printed tree does not parse back the same: %v\n%s", issues, outline(again))
	}
}

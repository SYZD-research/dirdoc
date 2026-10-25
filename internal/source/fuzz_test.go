package source

import (
	"testing"

	"dirdoc/internal/model"
)

// FuzzParseTree checks that the parser never panics and that every tree it accepts prints back
// to text that parses into the same tree.
func FuzzParseTree(f *testing.F) {
	for _, seed := range []string{
		"./ [[#src]]\n├── agent/ # note\n├── cli/ [[#cli]]\n│   └── ...\n└── main.rs\n",
		"src/\n  cli/\n    args.rs\n  main.rs\n",
		"\"my dir/\"\n+page.svelte\ncmd/server/ [[#s]]\n",
		"|-- a/\n|   `-- b\n`-- c\n",
		"./\n\t bad\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		root, _, issues := ParseTree(text)
		if len(issues) > 0 {
			return
		}
		printed := FormatTree(root, LinkAnnotator)
		again, _, issues := ParseTree(printed)
		if len(issues) > 0 {
			t.Fatalf("printed tree does not parse: %v\ninput: %q\nprinted:\n%s", issues, text, printed)
		}
		if outline(again) != outline(root) {
			t.Fatalf("round trip changed the tree\ninput: %q\nbefore:\n%s\nafter:\n%s", text, outline(root), outline(again))
		}
		var check func(n *model.TreeNode)
		check = func(n *model.TreeNode) {
			for _, c := range n.Children {
				if c.Name == "" || c.Name == "." || c.Name == ".." {
					t.Fatalf("accepted invalid name %q from %q", c.Name, text)
				}
				check(c)
			}
		}
		check(root)
	})
}

package move

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SYZD-research/dirdoc/internal/container"
	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/resolve"
)

func store(t *testing.T, path string, doc any) {
	t.Helper()
	js, err := model.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := container.WriteFile(path, js, false, container.LevelFastest); err != nil {
		t.Fatal(err)
	}
}

func TestRewrite(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "dirdoc.toml"), nil, 0o644)
	for _, f := range []string{"a/x.go", "a/y.go", "b/z.go"} {
		os.MkdirAll(filepath.Join(root, filepath.Dir(f)), 0o755)
		os.WriteFile(filepath.Join(root, f), nil, 0o644)
	}
	x := filepath.Join(root, "a", "x.go")
	store(t, resolve.PFDFile(x), &model.PFD{Target: "x.go", Summary: "[[y.go]] [[#f]] [[../b/z.go#g|zed]] [[/b/]] [[../../out.go]]",
		Functions: []model.Entry{{ID: "f", Name: "f()", Summary: "s"}}})
	z := filepath.Join(root, "b", "z.go")
	store(t, resolve.PFDFile(z), &model.PFD{Target: "z.go", Summary: "[[../a/x.go#f]] [[/a/x.go]] `[[../a/x.go]]` [[../a/]]",
		Functions: []model.Entry{{ID: "g", Name: "g()", Summary: "s"}}})
	store(t, resolve.DSDFile(root), &model.DSD{Tree: &model.TreeNode{Name: ".", Type: model.NodeDir, Children: []*model.TreeNode{
		{Name: "a", Type: model.NodeDir, Children: []*model.TreeNode{{Name: "x.go", Type: model.NodeFile, Note: "x"}, {Name: "y.go", Type: model.NodeFile}}},
		{Name: "b", Type: model.NodeDir},
	}}})

	p, err := resolve.Open(root, root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Make(p, x, filepath.Join(root, "b", "w.go"), false, false)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Write{}
	for _, w := range plan.Writes {
		rel, _ := filepath.Rel(root, w.Path)
		got[filepath.ToSlash(rel)] = w
	}
	if len(got) != 3 {
		t.Fatalf("writes: %v", got)
	}
	moved := got["b/.dirdoc/w.go.dirdoc_pfd"]
	if moved.PFD == nil || moved.PFD.Target != "w.go" || moved.From != resolve.PFDFile(x) ||
		moved.PFD.Summary != "[[../a/y.go]] [[#f]] [[z.go#g|zed]] [[/b/]] [[../../out.go]]" {
		t.Errorf("moved PFD: %+v", moved.PFD)
	}
	if s := got["b/.dirdoc/z.go.dirdoc_pfd"].PFD.Summary; s != "[[w.go#f]] [[/b/w.go]] `[[../a/x.go]]` [[../a/]]" {
		t.Errorf("referring PFD: %s", s)
	}
	// b/ is folded in the tree, so x.go leaves the tree and its note is reported.
	root2 := got[".dirdoc/.dirdoc_dsd"].DSD.Tree
	if a := root2.Find("a"); len(a.Children) != 1 || a.Children[0].Name != "y.go" || len(plan.Warnings) != 1 ||
		!strings.Contains(plan.Warnings[0], "removed a/x.go from the tree") {
		t.Errorf("tree %+v, warnings %v", a, plan.Warnings)
	}
}

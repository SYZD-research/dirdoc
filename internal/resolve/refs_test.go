package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SYZD-research/dirdoc/internal/container"
	"github.com/SYZD-research/dirdoc/internal/model"
)

func TestFindRefs(t *testing.T) {
	md := "See [[#a]] and [[../core/x.rs#parse|parser]].\n\n" +
		"Inline `[[#code]]` and escaped \\[[#esc]].\n\n" +
		"```\n[[#block]]\n```\n\n" +
		"    [[#indented]]\n\n" +
		"Dir [[agent/]] and file [[x.rs]]."
	var got []string
	for _, r := range FindRefs(md) {
		got = append(got, r.Path+"|"+r.ID+"|"+r.Label)
	}
	want := "|a|,../core/x.rs|parse|parser,agent/||,x.rs||"
	if strings.Join(got, ",") != want {
		t.Errorf("FindRefs = %v", got)
	}
}

func writeDoc(t *testing.T, path string, v any) {
	t.Helper()
	js, err := model.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := container.WriteFile(path, js, true, container.LevelFastest); err != nil {
		t.Fatal(err)
	}
}

func TestResolveAndInherit(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "dirdoc.toml"), nil, 0o644)
	src := filepath.Join(root, "src")
	os.MkdirAll(filepath.Join(src, "cli"), 0o755)
	os.WriteFile(filepath.Join(src, "main.rs"), nil, 0o644)
	writeDoc(t, DSDFile(src), &model.DSD{
		Summary: "source",
		Tree: &model.TreeNode{Name: ".", Type: model.NodeDir, Children: []*model.TreeNode{
			{Name: "cli", Type: model.NodeDir, DSD: "cli", Note: "commands"},
		}},
		Entries: []model.DSDEntry{{ID: "cli", Document: "doc"}},
	})
	pfd := &model.PFD{Target: "main.rs", Summary: "entry point", Functions: []model.Entry{{ID: "main", Name: "main()", Summary: "starts"}}}
	writeDoc(t, PFDFile(filepath.Join(src, "main.rs")), pfd)

	p, err := Open(root, src)
	if err != nil {
		t.Fatal(err)
	}
	if p.Root != root {
		t.Fatalf("root = %s", p.Root)
	}
	owner := FileOwner(filepath.Join(src, "main.rs"))
	check := func(md string, status Status, ref, kind string) {
		t.Helper()
		refs := FindRefs(md)
		if len(refs) != 1 {
			t.Fatalf("%s: %d refs", md, len(refs))
		}
		res := p.Resolve(owner, refs[0])
		if res.Status != status || res.Ref != ref || res.Kind != kind {
			t.Errorf("%s: got status %d ref %q kind %q (%s)", md, res.Status, res.Ref, res.Kind, res.Reason)
		}
	}
	check("[[#main]]", RefOK, "src/main.rs#main", "function")
	check("[[#nope]]", RefMissingLocal, "src/main.rs#nope", "")
	check("[[cli/]]", RefOK, "src/cli/", "directory")
	check("[[cli]]", RefOK, "src/cli/", "directory")
	check("[[./#cli]]", RefOK, "src/#cli", "dsd-entry")
	check("[[/src/main.rs]]", RefOK, "src/main.rs", "file")
	check("[[missing.rs#x]]", RefBroken, "src/missing.rs#x", "")
	check("[[../../outside.rs]]", RefOutside, "../../outside.rs", "")

	inh := p.Inherited(filepath.Join(src, "cli"))
	if inh == nil || inh.Dir != src || inh.Entry() == nil || inh.Node.Note != "commands" {
		t.Errorf("Inherited = %+v", inh)
	}
	if p.Rel(src, true) != "src/" || p.Rel(root, true) != "./" {
		t.Errorf("Rel: %q %q", p.Rel(src, true), p.Rel(root, true))
	}
}

func TestBacklinksAndStale(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "dirdoc.toml"), nil, 0o644)
	src := filepath.Join(root, "src")
	os.MkdirAll(src, 0o755)
	main := filepath.Join(src, "main.rs")
	os.WriteFile(main, []byte("fn main() {}\n"), 0o644)
	writeDoc(t, PFDFile(main), &model.PFD{Target: "main.rs", Summary: "see [[#run]]", TargetHash: model.Hash([]byte("fn main() {}\r\n")),
		Functions: []model.Entry{{ID: "run", Name: "run()", Summary: "runs", Document: "[[#run]] and [[/src/main.rs#run]]"}}})
	writeDoc(t, DSDFile(root), &model.DSD{Summary: "[[src/main.rs]] [[src/main.rs#run|again]] `[[src/main.rs#code]]`",
		Entries: []model.DSDEntry{{ID: "x", Document: "[[src/main.rs#run]] [[src/]]"}}})

	p, err := Open(root, root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, b := range p.Backlinks(main) {
		got = append(got, p.Ref(b.From)+"#"+b.FromID+" -> "+b.ID)
	}
	want := "./# -> ,./# -> run,./#x -> run,src/main.rs# -> run,src/main.rs#run -> run"
	if strings.Join(got, ",") != want {
		t.Errorf("Backlinks = %v\nwant %s", got, want)
	}

	pf, _ := p.PFD(main)
	if s := p.Stale(main, pf); s == nil || *s {
		t.Errorf("Stale = %v, want false (only the line endings differ)", s)
	}
	os.WriteFile(main, []byte("fn main() { run() }\n"), 0o644)
	p, _ = Open(root, root)
	pf, _ = p.PFD(main)
	if s := p.Stale(main, pf); s == nil || !*s {
		t.Errorf("Stale = %v, want true", s)
	}
	if s := p.Stale(main, &model.PFD{Target: "main.rs"}); s != nil {
		t.Errorf("Stale without target_hash = %v, want nil", *s)
	}
	if s := p.Stale(filepath.Join(src, "gone.rs"), pf); s != nil {
		t.Errorf("Stale of a missing file = %v, want nil", *s)
	}
}

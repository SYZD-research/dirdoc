package merge

import (
	"strings"
	"testing"

	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/source"
)

func entries(spec string) []model.Entry {
	var out []model.Entry
	for _, f := range strings.Fields(spec) {
		id, summary, _ := strings.Cut(f, "=")
		out = append(out, model.Entry{ID: id, Name: id + "()", Summary: summary})
	}
	return out
}

func summaries(es []model.Entry) string {
	var out []string
	for _, e := range es {
		out = append(out, e.ID+"="+e.Summary)
	}
	return strings.Join(out, " ")
}

func TestOrder(t *testing.T) {
	keep := func(string) bool { return true }
	tests := []struct{ ours, theirs, want string }{
		{"a b c", "a b c", "a b c"},
		{"a b c", "a x b c", "a x b c"},
		{"a c", "x a b c", "x a b c"},
		{"a b", "b a y", "a y b"}, // theirs reordered: ours' order wins, y follows a as in theirs
		{"", "x y", "x y"},
	}
	for _, tt := range tests {
		if got := strings.Join(order(strings.Fields(tt.ours), strings.Fields(tt.theirs), keep), " "); got != tt.want {
			t.Errorf("order(%q, %q) = %q, want %q", tt.ours, tt.theirs, got, tt.want)
		}
	}
}

func TestPFDMergesByID(t *testing.T) {
	base := &model.PFD{Target: "a.go", Summary: "s", Functions: entries("f=1 g=1 h=1 old=1")}
	ours := &model.PFD{Target: "a.go", Summary: "s", Functions: entries("f=ours g=1 h=1 new=ours")}
	theirs := &model.PFD{Target: "a.go", Summary: "theirs", Functions: entries("f=1 x=theirs g=theirs h=1")}
	out, cs := PFD(base, ours, theirs)
	if len(cs) != 0 {
		t.Fatalf("conflicts: %+v", cs)
	}
	if got := summaries(out.Functions); got != "f=ours x=theirs g=theirs h=1 new=ours" {
		t.Errorf("functions = %s", got)
	}
	if out.Summary != "theirs" || len(out.Overview.Functions) != 5 {
		t.Errorf("merged = %+v", out)
	}
}

func TestPFDConflicts(t *testing.T) {
	base := &model.PFD{Target: "a.go", Summary: "s", Functions: entries("f=1 g=1")}
	ours := &model.PFD{Target: "a.go", Summary: "ours", Functions: entries("f=ours")}
	theirs := &model.PFD{Target: "a.go", Summary: "theirs", Functions: entries("f=1 g=theirs"), Types: entries("new=theirs")}
	ours.Variables = entries("new=ours")
	out, cs := PFD(base, ours, theirs)
	var got []string
	for _, c := range cs {
		got = append(got, c.Section+"#"+c.ID+"."+c.Key)
	}
	// summary: both changed; g: deleted by us, changed by them; new: a variable here, a type there.
	if strings.Join(got, " ") != "#.summary functions#g. types#new. variables#new." {
		t.Errorf("conflicts = %v", got)
	}
	if out.Summary != "ours" || summaries(out.Functions) != "f=ours g=theirs" {
		t.Errorf("merged = %+v", out)
	}
	text := source.EmitConflicts(nil, out, "dirdoc pfd: a.go", cs)
	for _, want := range []string{
		"<<<<<<< ours\nsummary = \"ours\"\n=======\nsummary = \"theirs\"\n>>>>>>> theirs\n",
		"<<<<<<< ours\n=======\n[[functions]]\nid = \"g\"\nname = \"g()\"\nsummary = \"theirs\"\n>>>>>>> theirs\n",
		"<<<<<<< ours\n=======\n[[types]]\nid = \"new\"",
		"<<<<<<< ours\n[[variables]]\nid = \"new\"",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("conflict source lacks %q:\n%s", want, text)
		}
	}
}

func TestPFDWithoutBase(t *testing.T) {
	ours := &model.PFD{Target: "a.go", Summary: "same", Functions: entries("f=1 g=ours")}
	theirs := &model.PFD{Target: "a.go", Summary: "same", Functions: entries("f=1 g=theirs h=2")}
	out, cs := PFD(nil, ours, theirs)
	if len(cs) != 1 || cs[0].ID != "g" || cs[0].Key != "summary" {
		t.Errorf("conflicts = %+v", cs)
	}
	if summaries(out.Functions) != "f=1 g=ours h=2" {
		t.Errorf("functions = %s", summaries(out.Functions))
	}
}

func TestPFDKeepsTheAnalysisOfTheHashItTakes(t *testing.T) {
	analyzed := func(hash string, line int) *model.PFD {
		p := &model.PFD{Target: "a.go", TargetHash: hash, Functions: entries("f=1")}
		p.Overview = model.BuildOverview(p)
		model.AnnotateOverview(&p.Overview, []model.Symbol{{ID: "f", Kind: "function", Line: line, Signature: "func f()"}})
		return p
	}
	out, _ := PFD(analyzed("h0", 1), analyzed("h0", 1), analyzed("h2", 7))
	if out.TargetHash != "h2" || out.Overview.Functions[0].Line != 7 {
		t.Errorf("took hash %s with line %d", out.TargetHash, out.Overview.Functions[0].Line)
	}
	out, _ = PFD(analyzed("h0", 1), analyzed("h1", 3), analyzed("h2", 7))
	if out.TargetHash != "h1" || out.Overview.Functions[0].Line != 3 {
		t.Errorf("both recompiled: took hash %s with line %d", out.TargetHash, out.Overview.Functions[0].Line)
	}
}

func parseTree(t *testing.T, text string) *model.TreeNode {
	t.Helper()
	root, _, issues := source.ParseTree(text)
	if len(issues) > 0 {
		t.Fatalf("tree %q: %v", text, issues)
	}
	return root
}

func TestTreeMerge(t *testing.T) {
	base := parseTree(t, "./\n├── a/  # a\n└── b.rs\n")
	ours := parseTree(t, "./\n├── a/  # a\n├── new/  # ours\n└── b.rs\n")
	theirs := parseTree(t, "./\n├── a/  # a changed\n├── b.rs\n└── c.rs  # theirs\n")
	out, cs := DSD(&model.DSD{Tree: base}, &model.DSD{Tree: ours}, &model.DSD{Tree: theirs})
	if len(cs) != 0 {
		t.Fatalf("conflicts: %+v", cs)
	}
	if got := source.FormatTree(out.Tree, source.LinkAnnotator); got != "./\n├── a/    # a changed\n├── new/  # ours\n├── b.rs\n└── c.rs  # theirs" {
		t.Errorf("tree:\n%s", got)
	}

	// Both change the note of a/, or ours adds under a/ what theirs deletes: the tree conflicts.
	for _, sides := range [][2]string{
		{"./\n├── a/  # ours\n└── b.rs\n", "./\n├── a/  # theirs\n└── b.rs\n"},
		{"./\n├── a/  # a\n│   └── x.rs\n└── b.rs\n", "./\n└── b.rs\n"},
	} {
		_, cs := DSD(&model.DSD{Tree: base}, &model.DSD{Tree: parseTree(t, sides[0])}, &model.DSD{Tree: parseTree(t, sides[1])})
		if len(cs) != 1 || cs[0].Key != "tree" {
			t.Errorf("%q vs %q: conflicts %+v", sides[0], sides[1], cs)
		}
	}
}

func TestDSDEntriesAndExtras(t *testing.T) {
	base := &model.DSD{Entries: []model.DSDEntry{{ID: "cli", Document: "old"}}, Extra: model.Extra{"x-a": []byte(`1`)}}
	ours := &model.DSD{Entries: []model.DSDEntry{{ID: "cli", Document: "old", Name: "CLI"}}, Extra: model.Extra{"x-a": []byte(`2`)}}
	theirs := &model.DSD{Entries: []model.DSDEntry{{ID: "cli", Document: "new"}}, Extra: model.Extra{"x-a": []byte(`3`), "x-b": []byte(`true`)}}
	out, cs := DSD(base, ours, theirs)
	if len(out.Entries) != 1 || out.Entries[0].Name != "CLI" || out.Entries[0].Document != "new" || string(out.Extra["x-b"]) != "true" {
		t.Errorf("merged = %+v", out)
	}
	if len(cs) != 1 || cs[0].Key != "x-a" {
		t.Fatalf("conflicts = %+v", cs)
	}
	text := source.EmitConflicts(out, nil, "dirdoc dsd", cs)
	if !strings.Contains(text, "<<<<<<< ours\nx-a = 2\n=======\nx-a = 3\n>>>>>>> theirs\nx-b = true\n") {
		t.Errorf("conflict source:\n%s", text)
	}
}

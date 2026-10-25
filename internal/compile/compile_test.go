package compile

import (
	"strings"
	"testing"

	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/source"
)

func parse(t *testing.T, src string, kind model.Kind) *source.Doc {
	t.Helper()
	doc, l := source.Parse([]byte(src), "in.toml", kind)
	if len(l) > 0 {
		t.Fatalf("parse: %v", l)
	}
	return doc
}

func TestDocumentNormalization(t *testing.T) {
	if got := Document("\r\n  \n    code\r\nnext  \n\n"); got != "    code\nnext" {
		t.Errorf("Document = %q", got)
	}
	if got := Summary("  a\r\nb \n"); got != "a\nb" {
		t.Errorf("Summary = %q", got)
	}
}

func TestPFDBuildAndOverview(t *testing.T) {
	src := parse(t, `summary = " s "
[[functions]]
name = "check_stdout()"
summary = "f"
document = '''

body
'''
[[variables]]
name = "MAX"
summary = "v"
`, model.KindPFD)
	p, loc, l := PFD(src, nil, "main.rs", false)
	if len(l) > 0 {
		t.Fatal(l)
	}
	if p.Summary != "s" || p.Functions[0].ID != "check_stdout" || p.Functions[0].Document != "body" {
		t.Errorf("built %+v", p)
	}
	if len(p.Overview.Functions) != 1 || p.Overview.Variables[0].ID != "MAX" {
		t.Errorf("overview %+v", p.Overview)
	}
	if ln := loc.EntryLine(model.SectionFunctions, 0, "summary"); ln != 4 {
		t.Errorf("summary line = %d", ln)
	}
}

func TestPFDMerge(t *testing.T) {
	base := &model.PFD{
		Target:  "main.rs",
		Summary: "old",
		Functions: []model.Entry{
			{ID: "a", Name: "a()", Summary: "A", Document: "keep me"},
			{ID: "b", Name: "b()", Summary: "B"},
		},
		Variables: []model.Entry{{ID: "OLD", Name: "OLD", Summary: "gone soon"}},
		Extra:     model.Extra{"x-keep": []byte(`1`)},
	}
	src := parse(t, `
[[functions]]
id = "a"
summary = "A2"

[[functions]]
name = "c(x)"
summary = "C"

[[variables]]
id = "OLD"
delete = true
`, model.KindPFD)
	p, _, l := PFD(src, base, "main.rs", true)
	if len(l) > 0 {
		t.Fatal(l)
	}
	var got []string
	for _, e := range p.Functions {
		got = append(got, e.ID+"="+e.Summary+"/"+e.Document)
	}
	if strings.Join(got, ",") != "a=A2/keep me,b=B/,c=C/" {
		t.Errorf("functions = %v", got)
	}
	if len(p.Variables) != 0 || p.Summary != "old" || string(p.Extra["x-keep"]) != "1" {
		t.Errorf("merged = %+v", p)
	}
	if base.Functions[0].Summary != "A" {
		t.Error("merge modified the base document")
	}

	bad := parse(t, "[[variables]]\nid = \"NOPE\"\ndelete = true\n", model.KindPFD)
	if _, _, l := PFD(bad, base, "main.rs", true); len(l) != 1 || l[0].Line != 3 {
		t.Errorf("deleting a missing entry: %v", l)
	}
	if _, _, l := PFD(bad, base, "main.rs", false); len(l) != 1 {
		t.Errorf("delete without --merge: %v", l)
	}
}

func TestDSDMerge(t *testing.T) {
	base := &model.DSD{Summary: "s", Entries: []model.DSDEntry{{ID: "cli", Document: "old"}}}
	src := parse(t, "[[dsd]]\nid = \"cli\"\nname = \"CLI\"\n\n[[dsd]]\nid = \"new\"\ndocument = \"n\"\n", model.KindDSD)
	d, _, l := DSD(src, base, true)
	if len(l) > 0 {
		t.Fatal(l)
	}
	if len(d.Entries) != 2 || d.Entries[0].Name != "CLI" || d.Entries[0].Document != "old" || d.Summary != "s" {
		t.Errorf("merged = %+v", d)
	}
}

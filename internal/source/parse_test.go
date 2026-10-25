package source

import (
	"strings"
	"testing"

	"github.com/SYZD-research/dirdoc/internal/diag"
	"github.com/SYZD-research/dirdoc/internal/model"
)

const pfdSource = `summary = "CLIのエントリポイント。"
x-owner = "core"

[[types]]
name = "Config"
summary = "実行時設定"

[[functions]]
name = "check_stdout()"
summary = "標準出力がTTYかを判定する"
document = '''
TTYなら色付き出力を有効にする。

## 注意
Windows では …
'''

[[variables]]
name = "MAX_AGENTS"
summary = "同時に処理するエージェント数の上限"
`

func TestParsePFD(t *testing.T) {
	doc, l := Parse([]byte(pfdSource), "main.toml", model.KindPFD)
	if len(l) > 0 {
		t.Fatalf("diagnostics: %v", l)
	}
	if Str(doc.Summary) != "CLIのエントリポイント。" {
		t.Errorf("summary = %q", Str(doc.Summary))
	}
	fns := doc.Sections[model.SectionFunctions]
	if len(fns) != 1 || Str(fns[0].Name) != "check_stdout()" || fns[0].Line != 8 {
		t.Fatalf("functions = %+v", fns)
	}
	if !strings.HasPrefix(Str(fns[0].Document), "TTYなら") {
		t.Errorf("document = %q", Str(fns[0].Document))
	}
	if string(doc.Extra["x-owner"]) != `"core"` {
		t.Errorf("extra = %s", doc.Extra["x-owner"])
	}
}

func TestParseErrorsCarryLines(t *testing.T) {
	src := `summary = "s"

[[functions]]
name = "f()"
summray = "typo"

[[variables]]
name = "V"
summary = "v"
document = "not allowed"
`
	_, l := Parse([]byte(src), "x.toml", model.KindPFD)
	if len(l) != 2 {
		t.Fatalf("want 2 diagnostics, got %v", l)
	}
	if l[0].Line != 5 || !strings.Contains(l[0].Message, `did you mean "summary"`) {
		t.Errorf("typo: %v", l[0])
	}
	if l[1].Line != 10 {
		t.Errorf("variable document: %v", l[1])
	}

	_, l = Parse([]byte("summary = \"s\"\ntree = '''\n./\nbad name/\n'''\n"), "d.toml", model.KindDSD)
	if len(l) != 1 || l[0].Code != diag.CodeTreeSyntax || l[0].Line != 4 {
		t.Errorf("tree syntax: %v", l)
	}

	_, l = Parse([]byte("[[functions]]\nname = \"f()\"\nsummary = \"s\"\n"), "d.toml", model.KindDSD)
	if len(l) != 1 || !strings.Contains(l[0].Message, "only valid for a file") {
		t.Errorf("wrong kind: %v", l)
	}

	_, l = Parse([]byte("summary = \n"), "bad.toml", model.KindPFD)
	if len(l) != 1 || l[0].Line != 1 {
		t.Errorf("syntax error: %v", l)
	}
}

func TestEmitPFDRoundTrip(t *testing.T) {
	p := &model.PFD{
		Target:  "main.rs",
		Summary: "line one\nline two",
		Functions: []model.Entry{
			{ID: "check_stdout", Name: "check_stdout()", Summary: `say "hi"`, Document: "code:\n```\n'''\n```"},
			{ID: "x", Name: "x", Summary: "s", Document: "plain\n\n## heading"},
		},
		Variables: []model.Entry{{ID: "MAX", Name: "MAX", Summary: "tab\there", Extra: model.Extra{"x-unit": []byte(`{"min":1,"tags":["a"]}`)}}},
	}
	text := EmitPFD(p, "dirdoc pfd: main.rs")
	doc, l := Parse([]byte(text), "out.toml", model.KindPFD)
	if len(l) > 0 {
		t.Fatalf("emitted TOML does not parse: %v\n%s", l, text)
	}
	// The newline before the closing ''' is removed by compile's normalization, not by the parser.
	if strings.TrimSpace(Str(doc.Summary)) != p.Summary {
		t.Errorf("summary = %q", Str(doc.Summary))
	}
	fns := doc.Sections[model.SectionFunctions]
	for i, e := range p.Functions {
		if got := strings.TrimRight(Str(fns[i].Document), "\n"); got != e.Document {
			t.Errorf("document %d = %q, want %q", i, got, e.Document)
		}
		if Str(fns[i].Summary) != e.Summary || Str(fns[i].ID) != e.ID {
			t.Errorf("entry %d = %+v", i, fns[i])
		}
	}
	vars := doc.Sections[model.SectionVariables]
	if Str(vars[0].Summary) != "tab\there" || string(vars[0].Extra["x-unit"]) != `{"min":1,"tags":["a"]}` {
		t.Errorf("variable = %+v %s", vars[0], vars[0].Extra["x-unit"])
	}
}

package source

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/SYZD-research/dirdoc/internal/diag"
	"github.com/SYZD-research/dirdoc/internal/model"
)

func storedPFD() *model.PFD {
	p := &model.PFD{
		Target:     "main.rs",
		TargetHash: "sha256:9f2c",
		Summary:    "CLIのエントリポイント。\n2 行目",
		Types:      []model.Entry{{ID: "Config", Name: "Config", Summary: "実行時設定", Document: "中に ''' がある本文"}},
		Functions: []model.Entry{
			{ID: "main", Name: "main()", Summary: "エントリポイント", Document: "[[#Config]] を作る。\n\n## 注意\n…\n",
				Extra: model.Extra{"x-tags": json.RawMessage(`["cli", 1.5, {"deep": true}]`), "since": json.RawMessage(`2`)}},
		},
		Variables: []model.Entry{{ID: "MAX_AGENTS", Name: "MAX_AGENTS", Summary: "上限"}},
		Extra:     model.Extra{"x-owner": json.RawMessage(`"core"`), "added_later": json.RawMessage(`{"a": [1, 2]}`)},
	}
	p.Overview = model.BuildOverview(p)
	model.AnnotateOverview(&p.Overview, []model.Symbol{
		{ID: "MAX_AGENTS", Kind: "variable", Line: 5, Signature: `const MAX_AGENTS: usize = 8;`},
		{ID: "Config", Kind: "type", Line: 8, Signature: `pub struct Config { ... }`},
		{ID: "main", Kind: "function", Line: 13, Signature: `fn main()`},
		{ID: "Config::load", Kind: "function", Line: 30, Signature: `pub fn load(path: &Path) -> "x"`},
	})
	return p
}

func storedDSD() *model.DSD {
	root, _, _ := ParseTree("./  [[#src]]  # ルート\n├── \"with space\"/\n├── cli/  [[#cli]]  # 引数\n│   └── ...\n└── main.rs  # エントリ")
	return &model.DSD{
		Summary: "ソース",
		Tree:    root,
		Entries: []model.DSDEntry{{ID: "src", Name: "ソースルート", Document: "本文"}, {ID: "cli"}},
		Extra:   model.Extra{"x-team": json.RawMessage(`"a"`)},
	}
}

func roundTrip(t *testing.T, d *model.DSD, p *model.PFD) string {
	t.Helper()
	text, err := EmitStored(d, p)
	if err != nil {
		t.Fatal(err)
	}
	kind, d2, p2, err := ParseStored([]byte(text))
	if err != nil {
		t.Fatalf("ParseStored: %v\n%s", err, text)
	}
	var want, got []byte
	if d != nil {
		want, _ = model.Marshal(d)
		got, _ = model.Marshal(d2)
		if kind != model.KindDSD {
			t.Errorf("kind = %v", kind)
		}
	} else {
		want, _ = model.Marshal(p)
		got, _ = model.Marshal(p2)
	}
	if !sameValue(want, got) {
		t.Errorf("round trip differs:\nwant %s\ngot  %s\n%s", want, got, text)
	}
	return text
}

func TestStoredRoundTrip(t *testing.T) {
	text := roundTrip(t, nil, storedPFD())
	for _, want := range []string{
		"# dirdoc pfd: main.rs\nformat = \"dirdoc/pfd\"\nversion = 1\ntarget = \"main.rs\"\n",
		"since = 2\n", "x-tags = [\"cli\", 1.5, { deep = true }]\n", "added_later = { a = [1, 2] }\n",
		"[generated]\ntarget_hash = \"sha256:9f2c\"\nsymbols = [\n" +
			`  { id = "MAX_AGENTS", kind = "variable", line = 5, signature = "const MAX_AGENTS: usize = 8;" },` + "\n",
		`  { id = "Config::load", kind = "function", line = 30, signature = "pub fn load(path: &Path) -> \"x\"" },` + "\n]\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("stored TOML lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "overview") || strings.Contains(text, "undocumented") {
		t.Errorf("the overview is stored:\n%s", text)
	}
	text = roundTrip(t, storedDSD(), nil)
	if !strings.HasPrefix(text, "# dirdoc dsd\nformat = \"dirdoc/dsd\"\nversion = 1\nsummary = \"ソース\"\n\ntree = '''\n./") {
		t.Errorf("stored DSD:\n%s", text)
	}

	// Not analyzed (no symbols) and analyzed without symbols stay apart; no hash, no [generated].
	p := &model.PFD{Target: "a.txt", Functions: []model.Entry{{ID: "f", Name: "f()", Summary: "s"}}}
	p.Overview = model.BuildOverview(p)
	if text := roundTrip(t, nil, p); strings.Contains(text, "[generated]") {
		t.Errorf("[generated] without a hash or symbols:\n%s", text)
	}
	model.AnnotateOverview(&p.Overview, []model.Symbol{})
	if text := roundTrip(t, nil, p); !strings.Contains(text, "[generated]\nsymbols = []\n") {
		t.Errorf("analyzed without symbols:\n%s", text)
	}
}

func TestStoredRebuildsTheOverview(t *testing.T) {
	p := storedPFD()
	p.Overview.Functions[0].Summary = "stale copy" // the overview copies the summaries
	text, err := EmitStored(nil, p)
	if err != nil {
		t.Fatal(err)
	}
	_, _, got, err := ParseStored([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if got.Overview.Functions[0].Summary != "エントリポイント" {
		t.Errorf("overview summary = %q", got.Overview.Functions[0].Summary)
	}
	if len(got.Overview.Undocumented) != 1 || got.Overview.Undocumented[0].ID != "Config::load" || got.Overview.Types[0].Line != 8 {
		t.Errorf("overview = %+v", got.Overview)
	}
}

func TestStoredRejectsWhatTOMLCannotHold(t *testing.T) {
	p := storedPFD()
	p.Functions[0].Extra["x-gone"] = json.RawMessage(`null`)
	if _, err := EmitStored(nil, p); err == nil || !strings.Contains(err.Error(), `"x-gone" holds null`) {
		t.Errorf("null: %v", err)
	}
	d := storedDSD()
	d.Tree.Children[1].Extra = model.Extra{"later": json.RawMessage(`1`)}
	if _, err := EmitStored(d, nil); err == nil || !strings.Contains(err.Error(), "./cli") {
		t.Errorf("tree node with unknown fields: %v", err)
	}
	d = storedDSD()
	d.Tree.Children[2].Name = "a\nb"
	if _, err := EmitStored(d, nil); err == nil || !strings.Contains(err.Error(), "cannot keep") {
		t.Errorf("a name the tree notation cannot write: %v", err)
	}
	p = storedPFD()
	p.Extra["x-big"] = json.RawMessage(`1e999`)
	if _, err := EmitStored(nil, p); err == nil {
		t.Error("a number TOML cannot read back was stored")
	}
}

func TestParseStoredHandEdits(t *testing.T) {
	src := "format = \"dirdoc/pfd\"\r\nversion = 1\r\ntarget = \"a.go\"\r\n\r\n[[functions]]\r\nname = \"Run(ctx)\"\r\n" +
		"summary = \"runs\"\r\ndocument = '''\r\nline 1\r\nline 2\r\n'''\r\n"
	_, _, p, err := ParseStored([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if f := p.Functions[0]; f.ID != "Run" || f.Document != "line 1\nline 2" {
		t.Errorf("entry = %+v", f)
	}
	if p.Overview.Undocumented != nil || len(p.Overview.Functions) != 1 {
		t.Errorf("overview = %+v", p.Overview)
	}

	if _, _, _, err := ParseStored([]byte("# dirdoc pfd: a.go\nsummary = \"s\"\n")); !errors.Is(err, ErrNotStored) {
		t.Errorf("source: %v", err)
	}
	var se *SyntaxError
	if _, _, _, err := ParseStored([]byte("format = \"dirdoc/pfd\"\n<<<<<<< ours\n")); !errors.As(err, &se) || se.Line != 2 {
		t.Errorf("conflict markers: %v", err)
	}
	for src, want := range map[string]string{
		"format = \"dirdoc/pfd\"\nversion = 2\ntarget = \"a.go\"\n":      "unsupported schema version 2",
		"format = \"dirdoc/xyz\"\nversion = 1\n":                         "unknown format",
		"format = \"dirdoc/dsd\"\nversion = 1\ntree = 3\n":               "tree must be a string",
		"format = \"dirdoc/dsd\"\nversion = 1\ntree = 'a\tb'\n":          "tree, line 1",
		"format = \"dirdoc/pfd\"\nversion = 1\n[generated]\nsymbols = 1": "symbols must be an array",
	} {
		if _, _, _, err := ParseStored([]byte(src)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v (want %q)", src, err, want)
		}
	}
}

func TestStoredTOMLIsASource(t *testing.T) {
	p := storedPFD()
	delete(p.Extra, "added_later") // a source may hold only the fields this version knows, and x- ones
	delete(p.Functions[0].Extra, "since")
	text, err := EmitStored(nil, p)
	if err != nil {
		t.Fatal(err)
	}
	doc, l := Parse([]byte(text), "main.rs.dirdoc_pfd", model.KindPFD)
	if doc == nil || l.HasErrors() {
		t.Fatalf("diagnostics: %v", l)
	}
	if len(l) != 1 || l[0].Code != diag.CodeSourceIgnored || !strings.Contains(l[0].Message, "generated") {
		t.Errorf("diagnostics = %v", l)
	}
	if Str(doc.Target) != "main.rs" || len(doc.Sections[model.SectionFunctions]) != 1 {
		t.Errorf("doc = %+v", doc)
	}
	if _, l := Parse([]byte("format = \"dirdoc/dsd\"\nversion = 1\n"), "x", model.KindPFD); !l.HasErrors() {
		t.Error("a DSD header was accepted for a PFD")
	}
	if _, l := Parse([]byte("version = 7\n"), "x", model.KindDSD); len(l) == 0 || l[0].Code != diag.CodeVersion {
		t.Errorf("version: %v", l)
	}
}

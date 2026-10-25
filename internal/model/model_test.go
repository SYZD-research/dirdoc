package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDeriveID(t *testing.T) {
	tests := []struct {
		name string
		want string
		ok   bool
	}{
		{"check_stdout()", "check_stdout", true},
		{"run(args: &[String]) -> i32", "run", true},
		{"Parser::parse(&self)", "Parser::parse", true},
		{"Pool<T>", "Pool", true},
		{"Map[K comparable, V any]", "Map", true},
		{"MAX_AGENTS", "MAX_AGENTS", true},
		{"  spaced()  ", "spaced", true},
		{"$scope.value", "$scope.value", true},
		{"関数()", "関数", true},
		{"async def fetch(url)", "", false},
		{"(s *Server) Start()", "", false},
		{"operator+(a, b)", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := DeriveID(tt.name)
		if got != tt.want || ok != tt.ok {
			t.Errorf("DeriveID(%q) = %q, %v; want %q, %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}

func TestValidID(t *testing.T) {
	for _, id := range []string{"a", "Parser::parse", "x-1.2", "$v", "日本語"} {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false", id)
		}
	}
	for _, id := range []string{"", "a b", "a#b", "a/b", "a[0]", strings.Repeat("a", MaxIDLen+1), "\xff"} {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true", id)
		}
	}
}

func TestPFDRoundTripKeepsUnknownFields(t *testing.T) {
	in := `{"format":"dirdoc/pfd","version":1,"target":"main.rs","summary":"<b>&","future":{"a":1},` +
		`"functions":[{"id":"main","name":"main()","summary":"entry","x-owner":"core"}]}`
	kind, _, p, err := Decode([]byte(in))
	if err != nil || kind != KindPFD {
		t.Fatalf("Decode: %v %v", kind, err)
	}
	p.Overview = BuildOverview(p)
	out, err := Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"format":"dirdoc/pfd","version":1,"target":"main.rs","summary":"<b>&",` +
		`"overview":{"types":[],"functions":[{"id":"main","name":"main()","summary":"entry"}],"variables":[]},` +
		`"functions":[{"id":"main","name":"main()","summary":"entry","x-owner":"core"}],"future":{"a":1}}`
	if string(out) != want {
		t.Errorf("Marshal:\n got %s\nwant %s", out, want)
	}
}

func TestDSDRoundTrip(t *testing.T) {
	d := &DSD{
		Summary: "src",
		Tree: &TreeNode{Name: ".", Type: NodeDir, DSD: "root", Children: []*TreeNode{
			{Name: "cli", Type: NodeDir, DSD: "cli", Note: "commands", More: true},
			{Name: "main.rs", Type: NodeFile},
		}},
		Entries: []DSDEntry{{ID: "root", Name: "Root", Document: "doc"}, {ID: "cli"}},
	}
	b, err := Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	kind, got, _, err := Decode(b)
	if err != nil || kind != KindDSD {
		t.Fatalf("Decode: %v %v", kind, err)
	}
	b2, _ := Marshal(got)
	if string(b) != string(b2) {
		t.Errorf("round trip changed the document:\n%s\n%s", b, b2)
	}
	if name := got.DisplayName("cli", "src"); name != "cli" {
		t.Errorf("DisplayName(cli) = %q", name)
	}
	if name := got.DisplayName("root", "src"); name != "Root" {
		t.Errorf("DisplayName(root) = %q", name)
	}
	if n := got.Tree.Find("cli"); n == nil || n.Note != "commands" {
		t.Errorf("Find(cli) = %+v", n)
	}
}

func TestDecodeErrors(t *testing.T) {
	_, _, _, err := Decode([]byte(`{"format":"dirdoc/dsd","version":2}`))
	if _, ok := err.(*VersionError); !ok {
		t.Errorf("version 2: got %v, want VersionError", err)
	}
	if _, _, _, err := Decode([]byte(`{"format":"other","version":1}`)); err == nil {
		t.Error("unknown format: no error")
	}
	if _, _, _, err := Decode([]byte(`{"format":"dirdoc/dsd","version":1,"summary":3}`)); err == nil {
		t.Error("summary of wrong type: no error")
	}
	var n TreeNode
	if err := json.Unmarshal([]byte(`{"name":".","type":"dir","children":[null]}`), &n); err == nil {
		t.Error("null child: no error")
	}
}

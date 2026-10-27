package diag

import (
	"strings"
	"testing"
)

func TestSortByLine(t *testing.T) {
	l := List{
		{Code: "a", Line: 9},
		{Code: "b"},
		{Code: "c", Line: 2},
		{Code: "d"},
		{Code: "e", Line: 5},
	}
	l.SortByLine()
	got := ""
	for _, d := range l {
		got += d.Code
	}
	if got != "cdeab" {
		t.Errorf("order = %s, want cdeab", got)
	}
}

func TestDiagnosticString(t *testing.T) {
	d := Diagnostic{Severity: Warning, Code: CodeRefBroken, Path: "src/main.rs#main", File: "<stdin>", Line: 6, Message: "m"}
	if got := d.String(); got != "warning[ref-broken] <stdin>:6 (src/main.rs#main): m" {
		t.Errorf("String = %q", got)
	}
	d.File, d.Line = "", 0
	if got := d.String(); got != "warning[ref-broken] src/main.rs#main: m" {
		t.Errorf("String = %q", got)
	}
	l := List{{Severity: Warning}, {Severity: Warning}}
	if l.Failed(false) || !l.Failed(true) {
		t.Error("Failed: warnings must fail only in strict mode")
	}
}

func TestHeadings(t *testing.T) {
	got := Headings("# One\ntext\n\n## `Two` *x*\n```\n# not\n```\nThree\n-----\n")
	if strings.Join(got, "|") != "One|Two x|Three" {
		t.Errorf("Headings = %q", got)
	}
}

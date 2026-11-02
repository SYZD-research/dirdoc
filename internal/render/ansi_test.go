package render

import (
	"regexp"
	"strings"
	"testing"
)

var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func strip(s string) string { return sgrRe.ReplaceAllString(s, "") }

const sampleView = "# src/main.rs\n\n> stale: src/main.rs has changed since this document was last compiled\n\n" +
	"> CLIの**エントリ**ポイント。\n\n## Tree\n\n```text\n./           [[src/#src]]\n├── agent/   (dirdoc)      # 起動と管理\n" +
	"├── \"a b\"/    (pfd)  # note [[x]]\n├── main.rs  (stale)       # エントリ\n└── ...\n```\n\n" +
	"## Functions\n\n### main() `#main`\n\n[[src/agent/pool.rs#spawn]] を *呼ぶ*。`[[not/a.ref]]` と\n\n```toml\n[[functions]]\n```\n\n" +
	"> ```\n> > quoted code\n> ```\n\nSetext\n------\n\n## Warnings\n\n- tree-missing `src/old/`: listed in the tree\n- ref-broken `x`: y\n"

func TestTerminalOnlyAddsEscapes(t *testing.T) {
	for _, md := range []string{sampleView, "", "plain\n", "# h #\n", "``` x\nunclosed", "`a` **`b`** [[c]]", "日本語の `コード` と [[参照]]\n",
		"- > ```\n  > x\n", "> > a\n>\n> b", "~~~\n> not a quote\n~~~\n", "    indented\n    code\n"} {
		if got := strip(Terminal(md)); got != md {
			t.Errorf("styled text differs from %q:\n%q", md, got)
		}
	}
}

func TestTerminalStyles(t *testing.T) {
	got := Terminal(sampleView)
	for _, want := range []string{
		"\x1b[2m# \x1b[0m\x1b[1msrc/main.rs\x1b[0m\n",                                                                                  // heading
		"\x1b[2m>\x1b[0m \x1b[33mstale: src/main.rs has changed",                                                                       // the stale notice
		"\x1b[2m>\x1b[0m CLIの\x1b[2m**\x1b[0m\x1b[1mエントリ\x1b[0m\x1b[2m**\x1b[0mポイント。",                                                  // a quote, bold text
		"\x1b[2m```text\x1b[0m\n./           \x1b[4;34m[[src/#src]]\x1b[0m\n",                                                          // fence, tree link
		"\x1b[2m├── \x1b[0magent/   \x1b[2m(dirdoc)\x1b[0m      \x1b[2m# 起動と管理\x1b[0m\n",                                               // tree line and note
		"\x1b[2m├── \x1b[0m\"a b\"/    \x1b[2m(pfd)\x1b[0m  \x1b[2m# note [[x]]\x1b[0m\n",                                              // quoted name; no link in a note
		"main.rs  \x1b[33m(stale)\x1b[0m",                                                                                              // a stale file
		"\x1b[2m└── ...\x1b[0m\n",                                                                                                      // more
		"### \x1b[0m\x1b[1mmain() \x1b[0m\x1b[1;32m`#main`\x1b[0m\n",                                                                   // code in a heading
		"\x1b[4;34m[[src/agent/pool.rs#spawn]]\x1b[0m を \x1b[2m*\x1b[0m\x1b[3m呼ぶ\x1b[0m\x1b[2m*\x1b[0m。\x1b[32m`[[not/a.ref]]`\x1b[0m", // ref, italic, code
		"\x1b[32m[[functions]]\x1b[0m\n",                                                                                               // a code block is not a reference
		"\x1b[2m>\x1b[0m \x1b[2m```\x1b[0m\n\x1b[2m>\x1b[0m \x1b[32m> quoted code\x1b[0m\n",                                            // code in a quote
		"\x1b[1mSetext\x1b[0m\n\x1b[2m------\x1b[0m\n",                                                                                 // setext heading
		"- \x1b[33mtree-missing\x1b[0m \x1b[32m`src/old/`\x1b[0m: listed",                                                              // warning code
	} {
		if !strings.Contains(got, want) {
			t.Errorf("styled view lacks %q:\n%s", want, strings.ReplaceAll(got, "\x1b", "\\e"))
		}
	}
	for n, line := range strings.Split(got, "\n") {
		if i := strings.LastIndex(line, "\x1b["); i >= 0 && !strings.HasPrefix(line[i:], sgrReset) {
			t.Errorf("line %d leaves a style open: %q", n+1, line)
		}
	}
}

func TestPainter(t *testing.T) {
	p := NewPainter("warning[x] a: see [[b#c]] and `[[d]]`")
	p.Paint(0, 7, StyleWarning)
	p.Paint(0, 3, Style{Bold: true})
	p.Refs()
	if got := p.String(); got != "\x1b[1;33mwar\x1b[0m\x1b[33mning\x1b[0m[x] a: see \x1b[4;34m[[b#c]]\x1b[0m and `[[d]]`" {
		t.Errorf("painted = %q", got)
	}
	if got := Paint("日本", StyleGood); got != "\x1b[32m日本\x1b[0m" {
		t.Errorf("Paint = %q", got)
	}
	if got := Paint("", StyleGood); got != "" {
		t.Errorf("Paint of nothing = %q", got)
	}
}

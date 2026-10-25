package render

import (
	"testing"

	"dirdoc/internal/resolve"
)

func TestTransformHeadings(t *testing.T) {
	keep := func(r resolve.Ref) string { return r.Raw }
	tests := []struct {
		name string
		in   string
		top  int
		want string
	}{
		{"shift down", "## A\ntext\n### B ##\n", 4, "#### A\ntext\n##### B ##\n"},
		{"shift up", "#### A\n", 2, "## A\n"},
		{"already right", "# A\n", 1, "# A\n"},
		{"beyond six", "# A\n## B\n", 6, "###### A\n**B**\n"},
		{"setext", "Title\n=====\nbody\n", 3, "### Title\nbody\n"},
		{"in blockquote", "> ## Q\n", 3, "> ### Q\n"},
		{"code block untouched", "```\n# not a heading\n```\n## H\n", 3, "```\n# not a heading\n```\n### H\n"},
		{"no headings", "plain", 3, "plain"},
	}
	for _, tt := range tests {
		if got := transform(tt.in, tt.top, keep); got != tt.want {
			t.Errorf("%s:\n got %q\nwant %q", tt.name, got, tt.want)
		}
	}
}

func TestTransformRefs(t *testing.T) {
	got := transform("## [[#a]] title\nsee [[b.rs#x|label]] and `[[#code]]`", 3, func(r resolve.Ref) string {
		return "<" + r.Target + ">"
	})
	want := "### <#a> title\nsee <b.rs#x> and `[[#code]]`"
	if got != want {
		t.Errorf("got %q", got)
	}
}

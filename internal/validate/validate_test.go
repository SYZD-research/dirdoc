package validate

import (
	"strings"
	"testing"
)

func TestHeadings(t *testing.T) {
	got := Headings("# One\ntext\n\n## `Two` *x*\n```\n# not\n```\nThree\n-----\n")
	if strings.Join(got, "|") != "One|Two x|Three" {
		t.Errorf("Headings = %q", got)
	}
}

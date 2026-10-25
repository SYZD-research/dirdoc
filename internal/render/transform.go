// Package render builds the views `dirdoc [PATH]` prints, as JSON or Markdown (design §10, §11).
package render

import (
	"bytes"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"dirdoc/internal/resolve"
)

type edit struct {
	start, end int
	text       string
}

// transform prepares a Markdown body for embedding. When top > 0, headings are shifted so the
// shallowest one becomes level top (levels past 6 turn into bold text). Every reference is
// replaced with repl's result.
func transform(md string, top int, repl func(resolve.Ref) string) string {
	if md == "" {
		return md
	}
	src := []byte(md)
	var edits []edit
	if top > 0 {
		edits = headingEdits(src, top)
	}
	for _, r := range resolve.FindRefs(md) {
		edits = append(edits, edit{r.Start, r.End, repl(r)})
	}
	if len(edits) == 0 {
		return md
	}
	// Insertions come before replacements that start at the same place.
	sort.SliceStable(edits, func(a, b int) bool {
		if edits[a].start != edits[b].start {
			return edits[a].start < edits[b].start
		}
		return edits[a].end < edits[b].end
	})
	var b strings.Builder
	pos := 0
	for _, e := range edits {
		if e.start < pos {
			continue
		}
		b.Write(src[pos:e.start])
		b.WriteString(e.text)
		pos = e.end
	}
	b.Write(src[pos:])
	return b.String()
}

func headingEdits(src []byte, top int) []edit {
	doc := goldmark.DefaultParser().Parse(text.NewReader(src))
	var heads []*ast.Heading
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering && h.Lines().Len() > 0 {
			heads = append(heads, h)
		}
		return ast.WalkContinue, nil
	})
	minLevel := 7
	for _, h := range heads {
		minLevel = min(minLevel, h.Level)
	}
	if minLevel == 7 || minLevel == top {
		return nil
	}
	shift := top - minLevel
	var edits []edit
	for _, h := range heads {
		level := max(1, h.Level+shift)
		lines := h.Lines()
		first, last := lines.At(0), lines.At(lines.Len()-1)
		lineStart := bytes.LastIndexByte(src[:first.Start], '\n') + 1
		prefix := src[lineStart:first.Start]
		if j := bytes.LastIndexByte(prefix, '#'); j >= 0 {
			// ATX heading: "## text ##". The segment covers only "text".
			i := j
			for i > 0 && prefix[i-1] == '#' {
				i--
			}
			hashStart, hashEnd := lineStart+i, lineStart+j+1
			if level <= 6 {
				edits = append(edits, edit{hashStart, hashEnd, strings.Repeat("#", level)})
				continue
			}
			lineEnd := len(src)
			if k := bytes.IndexByte(src[last.Stop:], '\n'); k >= 0 {
				lineEnd = last.Stop + k
			}
			edits = append(edits, edit{hashStart, first.Start, "**"}, edit{last.Stop, lineEnd, "**"})
			continue
		}
		// Setext heading: the underline is the line after the text.
		end := last.Stop
		if end > 0 && src[end-1] == '\n' {
			end--
		}
		if end >= len(src) {
			continue
		}
		underlineEnd := len(src)
		if k := bytes.IndexByte(src[end+1:], '\n'); k >= 0 {
			underlineEnd = end + 1 + k
		}
		if level <= 6 {
			edits = append(edits, edit{first.Start, first.Start, strings.Repeat("#", level) + " "}, edit{end, underlineEnd, ""})
		} else {
			edits = append(edits, edit{first.Start, first.Start, "**"}, edit{end, underlineEnd, "**"})
		}
	}
	return edits
}

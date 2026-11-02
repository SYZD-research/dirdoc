package render

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/SYZD-research/dirdoc/internal/resolve"
)

// Style is how a part of the output looks on a terminal: SGR attributes and one of the basic
// colors, so that the terminal's own palette applies (design §11.3).
type Style struct {
	Bold, Faint, Italic, Underline bool
	Color                          int // an SGR foreground code such as 31 (red); 0 keeps the default
}

// The styles of the terminal output.
var (
	StyleHeading = Style{Bold: true}
	StyleFaint   = Style{Faint: true} // Markdown syntax, tree lines and notes
	StyleCode    = Style{Color: 32}
	StyleRef     = Style{Color: 34, Underline: true}
	StyleGood    = Style{Color: 32}
	StyleWarning = Style{Color: 33}
	StyleError   = Style{Color: 31}
	StyleMatch   = Style{Bold: true, Color: 31}
)

func (s Style) sgr() string {
	var codes []string
	if s.Bold {
		codes = append(codes, "1")
	}
	if s.Faint {
		codes = append(codes, "2")
	}
	if s.Italic {
		codes = append(codes, "3")
	}
	if s.Underline {
		codes = append(codes, "4")
	}
	if s.Color != 0 {
		codes = append(codes, strconv.Itoa(s.Color))
	}
	if len(codes) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

const sgrReset = "\x1b[0m"

// Painter adds terminal styles to a text without changing it: String returns the text with escape
// sequences around the painted ranges, and removing them gives the text back.
type Painter struct {
	src []byte
	st  []Style
}

// NewPainter starts painting s.
func NewPainter(s string) *Painter { return &Painter{src: []byte(s), st: make([]Style, len(s))} }

// Paint adds st to the bytes from start to end: attributes add up, and a color replaces the one
// painted before.
func (p *Painter) Paint(start, end int, st Style) {
	p.apply(start, end, func(s *Style) {
		s.Bold, s.Faint = s.Bold || st.Bold, s.Faint || st.Faint
		s.Italic, s.Underline = s.Italic || st.Italic, s.Underline || st.Underline
		if st.Color != 0 {
			s.Color = st.Color
		}
	})
}

// faint makes the bytes faint and no longer bold, as for the # of a heading.
func (p *Painter) faint(start, end int) {
	p.apply(start, end, func(s *Style) { s.Faint, s.Bold = true, false })
}

func (p *Painter) apply(start, end int, f func(*Style)) {
	for i := max(start, 0); i < min(end, len(p.src)); i++ {
		f(&p.st[i])
	}
}

// Refs paints the [[...]] references of the text, as Markdown sees them (not in code).
func (p *Painter) Refs() {
	for _, r := range resolve.FindRefs(string(p.src)) {
		p.Paint(r.Start, r.End, StyleRef)
	}
}

// String returns the text with its escape sequences. Styles never run across a line end, so that a
// pager can show any line on its own.
func (p *Painter) String() string {
	var b strings.Builder
	cur := Style{}
	for i, c := range p.src {
		want := p.st[i]
		if c == '\n' {
			want = Style{}
		}
		if want != cur && utf8.RuneStart(c) {
			if cur != (Style{}) {
				b.WriteString(sgrReset)
			}
			b.WriteString(want.sgr())
			cur = want
		}
		b.WriteByte(c)
	}
	if cur != (Style{}) {
		b.WriteString(sgrReset)
	}
	return b.String()
}

// Paint returns s in style st.
func Paint(s string, st Style) string {
	p := NewPainter(s)
	p.Paint(0, len(s), st)
	return p.String()
}

// Terminal styles the Markdown of a view for a terminal (design §11.3): headings in bold, code in
// green, references underlined in blue, the stale notice, warning codes and stale files in yellow,
// and the Markdown syntax, tree lines and notes faint. Only escape sequences are added.
func Terminal(md string) string {
	p := NewPainter(md)
	src := p.src
	doc := goldmark.DefaultParser().Parse(text.NewReader(src))
	var code [][2]int // code block lines, where no quote marker is looked for
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Heading:
			p.heading(n)
			if n.Level == 2 && headingText(n, src) == "Warnings" {
				p.warnings(n.NextSibling())
			}
		case *ast.FencedCodeBlock:
			h, ok := n.PreviousSibling().(*ast.Heading)
			code = append(code, p.fenced(n, ok && h.Level == 2 && headingText(h, src) == "Tree")...)
			return ast.WalkSkipChildren, nil
		case *ast.CodeBlock:
			for i := 0; i < n.Lines().Len(); i++ {
				s := n.Lines().At(i)
				p.Paint(s.Start, s.Stop, StyleCode)
				code = append(code, [2]int{s.Start, s.Stop})
			}
			return ast.WalkSkipChildren, nil
		case *ast.CodeSpan:
			if start, end, ok := inlineRange(src, n); ok {
				p.Paint(start, end, StyleCode)
			}
			return ast.WalkSkipChildren, nil
		case *ast.Emphasis:
			if start, end, ok := inlineRange(src, n); ok {
				p.Paint(start, end, Style{Bold: n.Level >= 2, Italic: n.Level == 1})
				if d := n.Level; start >= d && delimiters(src[start-d:start]) && end+d <= len(src) && delimiters(src[end:end+d]) {
					p.faint(start-d, start)
					p.faint(end, end+d)
				}
			}
		}
		return ast.WalkContinue, nil
	})
	p.quotes(code)
	p.Refs()
	return p.String()
}

// heading makes a heading bold and its # (or setext underline) faint.
func (p *Painter) heading(h *ast.Heading) {
	lines := h.Lines()
	if lines.Len() == 0 {
		return
	}
	first, last := lines.At(0), lines.At(lines.Len()-1)
	p.Paint(first.Start, last.Stop, StyleHeading)
	lineStart := bytes.LastIndexByte(p.src[:first.Start], '\n') + 1
	if bytes.IndexByte(p.src[lineStart:first.Start], '#') >= 0 {
		p.faint(lineStart, first.Start)
		p.faint(last.Stop, lineEnd(p.src, last.Stop)) // closing #s
		return
	}
	end := last.Stop
	if end > 0 && p.src[end-1] == '\n' {
		end--
	}
	if end < len(p.src) {
		p.faint(end+1, lineEnd(p.src, end+1))
	}
}

func lineEnd(src []byte, from int) int {
	if i := bytes.IndexByte(src[from:], '\n'); i >= 0 {
		return from + i
	}
	return len(src)
}

func headingText(h *ast.Heading, src []byte) string {
	var b strings.Builder
	for c := h.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			b.Write(t.Segment.Value(src))
		}
	}
	return strings.TrimSpace(b.String())
}

// textRange returns the bytes the text of n's descendants spans.
func textRange(n ast.Node) (int, int, bool) {
	start, end := -1, -1
	ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if t, ok := c.(*ast.Text); ok && entering {
			if start < 0 || t.Segment.Start < start {
				start = t.Segment.Start
			}
			end = max(end, t.Segment.Stop)
		}
		return ast.WalkContinue, nil
	})
	return start, end, start >= 0
}

// inlineRange is textRange, with the code spans among n and its descendants taken whole, backticks
// included.
func inlineRange(src []byte, n ast.Node) (int, int, bool) {
	start, end := -1, -1
	ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		s, e, ok := 0, 0, false
		switch c := c.(type) {
		case *ast.CodeSpan:
			if s, e, ok = textRange(c); ok {
				s, e = widen(src, s, e)
			}
		case *ast.Text:
			s, e, ok = c.Segment.Start, c.Segment.Stop, true
		}
		if ok {
			if start < 0 || s < start {
				start = s
			}
			end = max(end, e)
		}
		if _, code := c.(*ast.CodeSpan); code {
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return start, end, start >= 0
}

func delimiters(b []byte) bool {
	for _, c := range b {
		if c != '*' && c != '_' {
			return false
		}
	}
	return len(b) > 0
}

// widen extends the content of a code span over its padding and backticks.
func widen(src []byte, start, end int) (int, int) {
	s := start
	for s > 0 && src[s-1] == ' ' {
		s--
	}
	if s > 0 && src[s-1] == '`' {
		start = s
		for start > 0 && src[start-1] == '`' {
			start--
		}
	}
	e := end
	for e < len(src) && src[e] == ' ' {
		e++
	}
	if e < len(src) && src[e] == '`' {
		end = e
		for end < len(src) && src[end] == '`' {
			end++
		}
	}
	return start, end
}

// fenced paints a fenced code block: the fences faint, the content green, or the content as a tree.
// It returns the content lines.
func (p *Painter) fenced(n *ast.FencedCodeBlock, tree bool) [][2]int {
	lines := n.Lines()
	var out [][2]int
	switch {
	case lines.Len() > 0: // the fence is the line before the first one of the content
		if start := bytes.LastIndexByte(p.src[:lines.At(0).Start], '\n') + 1; start > 0 {
			p.fence(bytes.LastIndexByte(p.src[:start-1], '\n')+1, start-1)
		}
	case n.Info != nil:
		s := n.Info.Segment.Start
		p.fence(bytes.LastIndexByte(p.src[:s], '\n')+1, lineEnd(p.src, s))
	}
	for i := 0; i < lines.Len(); i++ {
		s := lines.At(i)
		out = append(out, [2]int{s.Start, s.Stop})
		if tree {
			p.treeLine(s.Start, s.Stop)
		} else {
			p.Paint(s.Start, s.Stop, StyleCode)
		}
	}
	if lines.Len() > 0 { // the closing fence is the line after the last one
		if start := lineEnd(p.src, lines.At(lines.Len()-1).Start) + 1; start < len(p.src) {
			p.fence(start, lineEnd(p.src, start))
		}
	}
	return out
}

// fence makes the fence of a code block faint, when the line holds one after the indentation and
// the > of quotes.
func (p *Painter) fence(start, end int) {
	i := start
	for i < end && (p.src[i] == ' ' || p.src[i] == '>') {
		i++
	}
	if t := p.src[i:end]; bytes.HasPrefix(t, []byte("```")) || bytes.HasPrefix(t, []byte("~~~")) {
		p.Paint(i, end, StyleFaint)
	}
}

var (
	treeRefRe  = regexp.MustCompile(`\[\[[^\]\n]*\]\]`)
	treeMarkRe = regexp.MustCompile(`\((pfd|dirdoc|stale)\)`)
)

// treeLine paints a line of the tree of a directory view: the lines faint, the name as it is, then
// the links, the (pfd) and (dirdoc) marks and the note.
func (p *Painter) treeLine(start, end int) {
	line := p.src[start:end]
	i := 0
	for i < len(line) {
		r, size := utf8.DecodeRune(line[i:])
		if r != ' ' && r != '│' && r != '├' && r != '└' && r != '─' {
			break
		}
		i += size
	}
	p.Paint(start, start+i, StyleFaint)
	// The name: quoted, or up to the padding before the annotations.
	j := i
	if j < len(line) && line[j] == '"' {
		for j++; j < len(line) && line[j] != '"'; j++ {
			if line[j] == '\\' {
				j++
			}
		}
		j = min(j+1, len(line)) // the closing quote
		if j < len(line) && line[j] == '/' {
			j++
		}
	} else if k := bytes.Index(line[j:], []byte("  ")); k >= 0 {
		j += k
	} else {
		j = len(line)
	}
	if name := bytes.TrimSpace(line[i:min(j, len(line))]); string(name) == "..." || string(name) == "…" {
		p.Paint(start+i, start+j, StyleFaint)
	}
	rest := line[min(j, len(line)):]
	base := start + min(j, len(line))
	note := len(rest)
	if k := bytes.Index(rest, []byte("# ")); k >= 0 && (k == 0 || rest[k-1] == ' ') {
		note = k
		p.Paint(base+k, base+len(rest), StyleFaint)
	}
	for _, m := range treeRefRe.FindAllIndex(rest[:note], -1) {
		p.Paint(base+m[0], base+m[1], StyleRef)
	}
	for _, m := range treeMarkRe.FindAllSubmatchIndex(rest[:note], -1) {
		st := StyleFaint
		if string(rest[m[2]:m[3]]) == "stale" {
			st = StyleWarning
		}
		p.Paint(base+m[0], base+m[1], st)
	}
}

// warnings paints the code that starts each item of the Warnings list.
func (p *Painter) warnings(n ast.Node) {
	list, ok := n.(*ast.List)
	if !ok {
		return
	}
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		if start, _, ok := textRange(item); ok {
			end := start
			for end < len(p.src) && p.src[end] != ' ' && p.src[end] != ':' && p.src[end] != '\n' {
				end++
			}
			p.Paint(start, end, StyleWarning)
		}
	}
}

// quotes makes the > of quotes faint, and the stale notice yellow.
func (p *Painter) quotes(code [][2]int) {
	for start := 0; start < len(p.src); {
		end := lineEnd(p.src, start)
		inCode := func(pos int) bool {
			for _, c := range code {
				if pos >= c[0] && pos < c[1] {
					return true
				}
			}
			return false
		}
		i := start
		for i < end {
			j := i
			for j < end && j-i < 3 && p.src[j] == ' ' {
				j++
			}
			if j >= end || p.src[j] != '>' || inCode(j) {
				break
			}
			p.faint(j, j+1)
			i = j + 1
			if i < end && p.src[i] == ' ' {
				i++
			}
		}
		if i > start && bytes.HasPrefix(p.src[i:end], []byte("stale: ")) {
			p.Paint(i, end, StyleWarning)
		}
		start = end + 1
	}
}

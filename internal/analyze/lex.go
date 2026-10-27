package analyze

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// token is a lexical token. Comments are dropped; literals are kept whole.
type token struct {
	kind  tokKind
	text  string
	line  int
	space bool // whitespace or a comment comes before it
	nl    bool // a line break comes before it
}

type tokKind uint8

const (
	tIdent  tokKind = iota + 1 // identifiers and keywords
	tPunct                     // operators and brackets
	tString                    // string, character, template and regular expression literals
	tNumber
)

// scanner walks source text and collects tokens; the scanner of each language drives it.
type scanner struct {
	src   string
	pos   int
	line  int
	space bool
	nl    bool
	toks  []token
}

func newScanner(src []byte) *scanner { return &scanner{src: string(src), line: 1} }

func (s *scanner) eof() bool { return s.pos >= len(s.src) }

// peek returns the byte off bytes ahead, or 0 past the end.
func (s *scanner) peek(off int) byte {
	if s.pos+off < len(s.src) {
		return s.src[s.pos+off]
	}
	return 0
}

func (s *scanner) has(prefix string) bool { return strings.HasPrefix(s.src[s.pos:], prefix) }

// advance moves over n bytes, counting lines.
func (s *scanner) advance(n int) {
	for ; n > 0 && s.pos < len(s.src); n-- {
		if s.src[s.pos] == '\n' {
			s.line++
		}
		s.pos++
	}
}

// emit adds the token src[start:pos] that began on line.
func (s *scanner) emit(kind tokKind, start, line int) {
	s.toks = append(s.toks, token{kind: kind, text: s.src[start:s.pos], line: line, space: s.space, nl: s.nl})
	s.space, s.nl = false, false
}

// blank skips spaces and line breaks.
func (s *scanner) blank() {
	for !s.eof() {
		switch s.src[s.pos] {
		case '\n':
			s.nl = true
		case ' ', '\t', '\r', '\f', '\v':
		default:
			return
		}
		s.space = true
		s.advance(1)
	}
}

func (s *scanner) lineComment() {
	for !s.eof() && s.src[s.pos] != '\n' {
		s.pos++
	}
	s.space = true
}

// blockComment skips a /* */ comment, counting nested ones when nested is set.
func (s *scanner) blockComment(nested bool) {
	line, depth := s.line, 0
	for !s.eof() {
		switch {
		case s.has("/*"):
			depth++
			s.advance(2)
		case s.has("*/"):
			depth--
			s.advance(2)
			if depth == 0 || !nested {
				s.space, s.nl = true, s.nl || s.line > line
				return
			}
		default:
			s.advance(1)
		}
	}
	s.space, s.nl = true, s.nl || s.line > line
}

// quoted moves past a literal closed by q, with backslash escapes. With oneLine it also stops
// before a line break, so that an unterminated literal spoils one line only.
func (s *scanner) quoted(q byte, oneLine bool) {
	s.advance(1)
	for !s.eof() {
		switch c := s.src[s.pos]; {
		case c == '\\':
			s.advance(2)
		case c == q:
			s.advance(1)
			return
		case c == '\n' && oneLine:
			return
		default:
			s.advance(1)
		}
	}
}

// until moves past the next end, or to the end of the source.
func (s *scanner) until(end string) {
	if i := strings.Index(s.src[s.pos:], end); i >= 0 {
		s.advance(i + len(end))
		return
	}
	s.advance(len(s.src) - s.pos)
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

// identStart reports whether an identifier starts here: a letter, '_' or a byte of extra.
func (s *scanner) identStart(extra string) bool {
	if s.eof() {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s.src[s.pos:])
	return r == '_' || unicode.IsLetter(r) || r < utf8.RuneSelf && strings.IndexByte(extra, byte(r)) >= 0
}

// ident moves past letters, digits, '_' and the bytes of extra.
func (s *scanner) ident(extra string) {
	for !s.eof() {
		r, n := utf8.DecodeRuneInString(s.src[s.pos:])
		if !(r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || r < utf8.RuneSelf && strings.IndexByte(extra, byte(r)) >= 0) {
			return
		}
		s.pos += n
	}
}

// number moves past a numeric literal: digits, letters (hex digits, exponents, suffixes), '_', and
// '.' when a digit follows (so that 1..2 and 1.max() stay apart).
func (s *scanner) number() {
	for !s.eof() {
		c := s.src[s.pos]
		if !(isDigit(c) || 'a' <= c|0x20 && c|0x20 <= 'z' || c == '_' || c == '.' && isDigit(s.peek(1))) {
			return
		}
		s.pos++
	}
}

// punct moves past the longest of ops at the current position, or one character.
func (s *scanner) punct(ops []string) {
	for _, op := range ops {
		if s.has(op) {
			s.pos += len(op)
			return
		}
	}
	_, n := utf8.DecodeRuneInString(s.src[s.pos:])
	s.pos += n
}

// text joins tokens as they were written: one space where the source had space, on one line.
func text(toks []token) string {
	var b strings.Builder
	for i, t := range toks {
		if i > 0 && t.space {
			b.WriteByte(' ')
		}
		b.WriteString(t.text)
	}
	return tidy(b.String())
}

// value prints the tokens of a value, abbreviating a long one: a literal in brackets loses its
// content, a call its arguments, and anything else becomes "...".
func value(toks []token) string {
	s := text(toks)
	if utf8.RuneCountInString(s) <= maxValue || len(toks) == 0 {
		return s
	}
	switch toks[0].text {
	case "{", "[", "(":
		return toks[0].text + "..." + closer[toks[0].text]
	}
	j := 0
	if toks[0].text == "new" {
		j = 1
	}
	for j < len(toks) && (toks[j].kind == tIdent || toks[j].text == "." || toks[j].text == "::") {
		j++
	}
	if j > 0 && j < len(toks) && toks[j].text == "(" {
		if head := text(toks[:j]); utf8.RuneCountInString(head) <= maxValue {
			return head + "(...)"
		}
	}
	return "..."
}

var closer = map[string]string{"(": ")", "[": "]", "{": "}"}

// assignment prints a declaration with a value: the part before the first "=" outside brackets,
// then the value, abbreviated.
func assignment(toks []token) string {
	depth := 0
	for i, t := range toks {
		if t.kind != tPunct {
			continue
		}
		switch t.text {
		case "(", "[", "{", "<":
			depth++
		case ")", "]", "}", ">":
			depth--
		case "=":
			if depth <= 0 {
				return text(toks[:i]) + " = " + value(toks[i+1:])
			}
		}
	}
	return text(toks)
}

// parser walks the tokens of a C-like language.
type parser struct {
	toks []token
	i    int
	syms collector
}

func (p *parser) eof() bool { return p.i >= len(p.toks) }

// tok returns the current token, or a zero token at the end.
func (p *parser) tok() token { return p.at(0) }

// at returns the token off tokens away from the current one, or a zero token.
func (p *parser) at(off int) token {
	if j := p.i + off; 0 <= j && j < len(p.toks) {
		return p.toks[j]
	}
	return token{}
}

// is reports whether the current token is the punctuation or identifier text.
func (p *parser) is(text string) bool {
	t := p.tok()
	return t.text == text && (t.kind == tPunct || t.kind == tIdent)
}

// line returns the line of token j.
func (p *parser) line(j int) int {
	if j < len(p.toks) {
		return p.toks[j].line
	}
	return 0
}

// skipGroup moves past the bracket group that starts at the current token.
func (p *parser) skipGroup() {
	depth := 0
	for !p.eof() {
		t := p.toks[p.i]
		p.i++
		if t.kind != tPunct {
			continue
		}
		switch t.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		if depth <= 0 {
			return
		}
	}
}

// groupEnd returns the index just after the bracket group that starts at token j.
func (p *parser) groupEnd(j int) int {
	save := p.i
	p.i = j
	p.skipGroup()
	end := p.i
	p.i = save
	return end
}

// skipAngles moves past the type parameters or arguments <...> that start at the current token.
func (p *parser) skipAngles() {
	depth := 0
	for !p.eof() {
		t := p.toks[p.i]
		if t.kind == tPunct {
			switch t.text {
			case "<":
				depth++
			case ">":
				depth--
			case "(", "[":
				p.skipGroup()
				continue
			case "{", "}", ";":
				return // not type arguments after all
			}
		}
		p.i++
		if depth <= 0 {
			return
		}
	}
}

// find returns the index of the first token from the current one that stop accepts while outside
// (), [] and {} (and <> when angles is set), or of the closing bracket that ends the enclosing
// group, or len(toks).
func (p *parser) find(angles bool, stop func(j int) bool) int {
	depth := 0
	for j := p.i; j < len(p.toks); j++ {
		t := p.toks[j]
		if depth == 0 && stop(j) {
			return j
		}
		if t.kind != tPunct {
			continue
		}
		switch t.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth == 0 {
				return j
			}
			depth--
		case "<":
			if angles {
				depth++
			}
		case ">":
			if angles && depth > 0 {
				depth--
			}
		}
	}
	return len(p.toks)
}

// punctAt reports whether token j is one of the punctuation texts.
func (p *parser) punctAt(j int, texts ...string) bool {
	if j < 0 || j >= len(p.toks) || p.toks[j].kind != tPunct {
		return false
	}
	for _, t := range texts {
		if p.toks[j].text == t {
			return true
		}
	}
	return false
}

// skipItem moves past a declaration dirdoc does not list: past its ';' or its block, stopping
// before a '}' that closes the enclosing one.
func (p *parser) skipItem() {
	for !p.eof() {
		t := p.toks[p.i]
		if t.kind == tPunct {
			switch t.text {
			case ";":
				p.i++
				return
			case "}":
				return
			case "{":
				p.skipGroup()
				return
			case "(", "[":
				p.skipGroup()
				continue
			}
		}
		p.i++
	}
}

// unquote returns the content of a quoted name ("my-method", `fun name`), or "".
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'' || s[0] == '`') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return ""
}

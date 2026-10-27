package analyze

import (
	"strings"

	"github.com/SYZD-research/dirdoc/internal/model"
)

// pyOps are the operators of several characters that the scanner must not split.
var pyOps = []string{"**=", "//=", ">>=", "<<=", "...", "->", ":=", "==", "!=", "<=", ">=", "**", "//", "<<", ">>",
	"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "@="}

// pyLine is a logical line of Python: one statement or header, its physical lines joined.
type pyLine struct {
	indent int
	toks   []token
}

// lexPython splits Python source into logical lines: brackets, backslashes and triple-quoted
// strings join physical lines, comments and blank lines are dropped, and each line keeps the
// indentation of its first physical line.
func lexPython(src []byte) []pyLine {
	s := newScanner(src)
	var lines []pyLine
	var cur []token
	depth, indent := 0, 0
	end := func() {
		if len(cur) > 0 {
			lines = append(lines, pyLine{indent: indent, toks: cur})
		}
		cur = nil
	}
	lineStart := true
	for !s.eof() {
		if lineStart && depth == 0 && len(cur) == 0 {
			col := 0
			for !s.eof() && (s.src[s.pos] == ' ' || s.src[s.pos] == '\t' || s.src[s.pos] == '\f') {
				if s.src[s.pos] == '\t' {
					col = col/8*8 + 8
				} else {
					col++
				}
				s.pos++
			}
			indent = col
		}
		lineStart = false
		if s.eof() {
			break
		}
		start, line := s.pos, s.line
		c := s.src[s.pos]
		switch {
		case c == '\n':
			s.advance(1)
			lineStart = true
			s.space = true
			if depth == 0 {
				end()
			}
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			s.pos++
			s.space = true
		case c == '\\' && (s.peek(1) == '\n' || s.peek(1) == '\r'):
			s.advance(2)
			if s.peek(0) == '\n' {
				s.advance(1)
			}
			s.space = true
		case c == '#':
			s.lineComment()
		case c == '"' || c == '\'':
			s.pyString()
			s.emitPy(&cur, tString, start, line)
		case isDigit(c) || c == '.' && isDigit(s.peek(1)):
			s.number()
			s.emitPy(&cur, tNumber, start, line)
		case s.identStart(""):
			s.ident("")
			if q := s.peek(0); (q == '"' || q == '\'') && isStringPrefix(s.src[start:s.pos]) {
				s.pyString()
				s.emitPy(&cur, tString, start, line)
			} else {
				s.emitPy(&cur, tIdent, start, line)
			}
		default:
			s.punct(pyOps)
			switch s.src[start] {
			case '(', '[', '{':
				depth++
			case ')', ']', '}':
				if depth > 0 {
					depth--
				}
			}
			s.emitPy(&cur, tPunct, start, line)
		}
	}
	end()
	return lines
}

// emitPy adds the token src[start:pos] to the logical line cur.
func (s *scanner) emitPy(cur *[]token, kind tokKind, start, line int) {
	*cur = append(*cur, token{kind: kind, text: s.src[start:s.pos], line: line, space: s.space})
	s.space = false
}

// isStringPrefix reports whether word prefixes a string literal: r, b, u, f, t and their pairs.
func isStringPrefix(word string) bool {
	if len(word) > 2 {
		return false
	}
	for _, r := range strings.ToLower(word) {
		if !strings.ContainsRune("rbuft", r) {
			return false
		}
	}
	return true
}

// pyString moves past a string literal at a quote, single or triple-quoted.
func (s *scanner) pyString() {
	q := s.src[s.pos]
	if triple := strings.Repeat(string(q), 3); s.has(triple) {
		s.advance(3)
		for !s.eof() {
			if s.src[s.pos] == '\\' {
				s.advance(2)
				continue
			}
			if s.has(triple) {
				s.advance(3)
				return
			}
			s.advance(1)
		}
		return
	}
	s.quoted(q, true)
}

// pythonFile lists classes, functions and assignments at the module level, and the methods, nested
// classes and attributes of classes as "Class.member". Nothing inside a function is listed; blocks
// such as if and try are looked into, except if __name__ == "__main__". Class attributes need a
// value (a bare annotation is an instance field).
func pythonFile(src []byte) []model.Symbol {
	var c collector
	var stack []pyBlock
	for _, ln := range lexPython(src) {
		for len(stack) > 0 && ln.indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		inDef, prefix := false, ""
		for _, b := range stack {
			switch b.kind {
			case 'f':
				inDef = true
			case 'c':
				prefix += b.name + "."
			}
		}
		if b, ok := pyLogical(&c, ln.toks, prefix, inDef); ok {
			b.indent = ln.indent
			stack = append(stack, b)
		}
	}
	return c.list()
}

// pyCompound are the keywords of compound statements whose body can follow the ':' on its line.
var pyCompound = map[string]bool{"if": true, "elif": true, "else": true, "for": true, "while": true,
	"try": true, "except": true, "finally": true, "with": true}

// pyLogical lists what a logical line declares, with the prefix of its class, and returns the
// block it opens when its body follows on the next lines.
func pyLogical(c *collector, toks []token, prefix string, inDef bool) (pyBlock, bool) {
	if toks[0].text == "@" { // a decorator
		return pyBlock{}, false
	}
	i := 0
	if toks[0].text == "async" && len(toks) > 1 {
		i = 1
	}
	kw, colon := toks[i], headerColon(toks)
	opens := colon == len(toks)-1
	switch {
	case (kw.text == "class" || kw.text == "def") && kw.kind == tIdent && i+1 < len(toks) && toks[i+1].kind == tIdent:
		name := toks[i+1].text
		kind, block := KindFunction, byte('f')
		if kw.text == "class" {
			kind, block = KindType, 'c'
		}
		if !inDef {
			c.add(prefix+name, kind, toks[0].line, text(toks[:colon]))
		}
		if opens {
			return pyBlock{kind: block, name: name}, true
		}
		if block == 'c' && colon < len(toks) && !inDef { // class X: a = 1
			pySimple(c, toks[colon+1:], prefix+name+".")
		}
	case opens && kw.text == "if" && hasIdent(toks, "__name__"): // the script's main code
		return pyBlock{kind: 'f'}, true
	case opens:
		return pyBlock{kind: 'o'}, true
	case kw.kind == tIdent && pyCompound[kw.text] && colon < len(toks): // if x: y = 1
		if !inDef && !(kw.text == "if" && hasIdent(toks[:colon], "__name__")) {
			pySimple(c, toks[colon+1:], prefix)
		}
	case !inDef:
		pySimple(c, toks, prefix)
	}
	return pyBlock{}, false
}

// pySimple lists the assignments among simple statements separated by ';'.
func pySimple(c *collector, toks []token, prefix string) {
	for len(toks) > 0 {
		end := 0
		for end < len(toks) && !(toks[end].kind == tPunct && toks[end].text == ";") {
			end++
		}
		st := toks[:end]
		if end < len(toks) {
			end++
		}
		toks = toks[end:]
		switch {
		case len(st) < 2:
		case st[0].text == "type" && st[0].kind == tIdent && st[1].kind == tIdent && len(st) > 2 &&
			(st[2].text == "=" || st[2].text == "["): // type X = ...
			c.add(prefix+st[1].text, KindType, st[0].line, assignment(st))
		case st[0].kind == tIdent && st[1].text == ":": // x: int = 1
			if prefix == "" || hasPunct(st, "=") {
				c.add(prefix+st[0].text, pyKind(st), st[0].line, assignment(st))
			}
		default:
			for _, name := range pyTargets(st) {
				c.add(prefix+name.text, pyKind(st), name.line, assignment(st))
			}
		}
	}
}

// pyBlock is a block open at some indentation: a class, a function or another compound statement.
type pyBlock struct {
	indent int
	kind   byte // 'c' class, 'f' function, 'o' other
	name   string
}

// headerColon returns the index of the ':' that ends a def or class header, outside brackets.
func headerColon(toks []token) int {
	depth := 0
	for i, t := range toks {
		if t.kind != tPunct {
			continue
		}
		switch t.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		case ":":
			if depth == 0 {
				return i
			}
		}
	}
	return len(toks)
}

// pyKind tells a function assigned with lambda from other variables.
func pyKind(toks []token) string {
	for i, t := range toks[:len(toks)-1] {
		if t.kind == tPunct && t.text == "=" {
			if toks[i+1].kind == tIdent && toks[i+1].text == "lambda" {
				return KindFunction
			}
			break
		}
	}
	return KindVariable
}

// pyTargets returns the names an assignment such as "a = 1", "a, b = 1, 2" or "(a, b) = t" assigns,
// or nil.
func pyTargets(toks []token) []token {
	end := "="
	if len(toks) > 0 && toks[0].kind == tPunct && (toks[0].text == "(" || toks[0].text == "[") {
		end = closer[toks[0].text]
		toks = toks[1:]
	}
	var names []token
	for i := 0; i < len(toks) && toks[i].kind == tIdent; i += 2 {
		names = append(names, toks[i])
		switch next := i + 1; {
		case next < len(toks) && toks[next].text == ",":
			if next+1 < len(toks) && toks[next+1].text == end { // a trailing comma
				return pyTargetsEnd(names, toks[next+1:], end)
			}
		case next < len(toks) && toks[next].text == end:
			return pyTargetsEnd(names, toks[next:], end)
		default:
			return nil
		}
	}
	return nil
}

// pyTargetsEnd returns names when rest, which starts with the end of the targets, ends them: an
// "=", or a closing bracket followed by "=".
func pyTargetsEnd(names, rest []token, end string) []token {
	if end == "=" || len(rest) > 1 && rest[1].kind == tPunct && rest[1].text == "=" {
		return names
	}
	return nil
}

func hasIdent(toks []token, text string) bool {
	for _, t := range toks {
		if t.kind == tIdent && t.text == text {
			return true
		}
	}
	return false
}

func hasPunct(toks []token, text string) bool {
	for _, t := range toks {
		if t.kind == tPunct && t.text == text {
			return true
		}
	}
	return false
}

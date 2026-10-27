package analyze

import (
	"strings"
	"unicode/utf8"

	"github.com/SYZD-research/dirdoc/internal/model"
)

// rustOps are the operators of several characters that the parser looks for.
var rustOps = []string{"..=", "...", "::", "->", "=>", "==", "!=", "<=", ">=", "&&", "||", ".."}

// lexRust reads Rust: nested comments, raw and byte strings, character literals and lifetimes, raw
// identifiers.
func lexRust(src []byte) []token {
	s := newScanner(src)
	if s.has("#!") && !s.has("#![") { // a script's #! line, not an inner attribute
		s.lineComment()
	}
	for {
		s.blank()
		if s.eof() {
			return s.toks
		}
		start, line := s.pos, s.line
		c := s.src[s.pos]
		switch {
		case s.has("//"):
			s.lineComment()
		case s.has("/*"):
			s.blockComment(true)
		case c == '"':
			s.quoted('"', false)
			s.emit(tString, start, line)
		case c == '\'':
			s.emit(s.rustQuote(), start, line)
		case isDigit(c):
			s.number()
			s.emit(tNumber, start, line)
		case s.identStart(""):
			s.ident("")
			switch word := s.src[start:s.pos]; {
			case (word == "r" || word == "br" || word == "cr") && (s.peek(0) == '"' || s.peek(0) == '#' && rawFollows(s.src[s.pos:])):
				s.rawString()
				s.emit(tString, start, line)
			case (word == "b" || word == "c") && s.peek(0) == '"':
				s.quoted('"', false)
				s.emit(tString, start, line)
			case word == "b" && s.peek(0) == '\'':
				s.quoted('\'', true)
				s.emit(tString, start, line)
			case word == "r" && s.peek(0) == '#': // a raw identifier: r#type
				s.pos++
				s.ident("")
				s.emit(tIdent, start, line)
			default:
				s.emit(tIdent, start, line)
			}
		default:
			s.punct(rustOps)
			s.emit(tPunct, start, line)
		}
	}
}

// rawFollows reports whether rest, which starts with '#', opens a raw string: #..#" .
func rawFollows(rest string) bool {
	rest = strings.TrimLeft(rest, "#")
	return strings.HasPrefix(rest, "\"")
}

// rawString moves past the rest of a raw string literal after its prefix: #"..."#.
func (s *scanner) rawString() {
	hashes := 0
	for s.peek(0) == '#' {
		hashes++
		s.pos++
	}
	s.advance(1) // "
	s.until("\"" + strings.Repeat("#", hashes))
}

// rustID returns the name an identifier declares: "type" for the raw identifier r#type.
func rustID(ident string) string { return strings.TrimPrefix(ident, "r#") }

// rustQuote reads a character literal ('a', '\n') or a lifetime or label ('a, 'static).
func (s *scanner) rustQuote() tokKind {
	if s.peek(1) == '\\' {
		s.quoted('\'', true)
		return tString
	}
	r, n := utf8.DecodeRuneInString(s.src[s.pos+1:])
	if r != utf8.RuneError && s.peek(1+n) == '\'' {
		s.advance(2 + n)
		return tString
	}
	s.advance(1)
	s.ident("")
	return tIdent
}

// rustFile lists functions, types (struct, enum, union, trait, type), consts, statics and
// macro_rules! macros. Inherent methods and the items of traits are named "Type::item". The items of
// trait implementations and of inline modules (tests) are left out.
func rustFile(src []byte) []model.Symbol {
	p := &rustParser{parser{toks: lexRust(src)}}
	p.items("")
	return p.syms.list()
}

type rustParser struct{ parser }

// items reads items until the '}' that closes their block.
func (p *rustParser) items(prefix string) {
	for !p.eof() && !p.is("}") {
		before := p.i
		p.item(prefix)
		if p.i == before {
			p.i++
		}
	}
}

// item reads an item with its attributes and qualifiers, listing it as prefix+name.
func (p *rustParser) item(prefix string) {
	for p.is("#") { // attributes: #[...] and #![...]
		p.i++
		if p.is("!") {
			p.i++
		}
		if p.is("[") {
			p.skipGroup()
		}
	}
	start := p.i
	if p.is("pub") {
		p.i++
		if p.is("(") {
			p.skipGroup()
		}
	}
	for { // qualifiers of fn, trait and impl
		next := p.at(1)
		switch {
		case p.is("default") && next.kind == tIdent, p.is("async"), p.is("unsafe"), p.is("auto") && next.text == "trait":
			p.i++
			continue
		case p.is("const") && (next.text == "fn" || next.text == "unsafe" || next.text == "async" || next.text == "extern"):
			p.i++
			continue
		case p.is("extern") && next.text == "fn":
			p.i++
			continue
		case p.is("extern") && next.kind == tString && p.at(2).text == "fn":
			p.i += 2
			continue
		}
		break
	}
	switch {
	case p.is("fn"):
		p.fn(prefix, start)
	case p.is("struct") || p.is("enum") || p.is("trait") || p.is("union") && p.at(1).kind == tIdent:
		p.typeItem(prefix, start)
	case p.is("impl"):
		p.impl()
	case p.is("type") && p.at(1).kind == tIdent:
		name := rustID(p.at(1).text)
		p.i += 2
		end := p.find(true, func(j int) bool { return p.punctAt(j, ";") })
		p.syms.add(prefix+name, KindType, p.line(start), assignment(p.toks[start:end]))
		p.i = end
		p.skipItem()
	case p.is("const") || p.is("static"):
		p.i++
		if p.is("mut") {
			p.i++
		}
		name := p.tok()
		end := p.find(false, func(j int) bool { return p.punctAt(j, ";") })
		if name.kind == tIdent && name.text != "_" {
			p.syms.add(prefix+rustID(name.text), KindVariable, p.line(start), assignment(p.toks[start:end]))
		}
		p.i = end
		p.skipItem()
	case p.is("macro_rules") && p.at(1).text == "!" && p.at(2).kind == tIdent:
		name := p.at(2).text
		p.syms.add(prefix+rustID(name), KindFunction, p.line(start), "macro_rules! "+name)
		p.i += 3
		p.skipItem()
	default: // use, mod, extern crate, extern blocks, macro invocations
		p.skipItem()
	}
}

// header returns the end of a declaration header: its '{' or ';' outside (), [] and <>.
func (p *rustParser) header() int {
	return p.find(true, func(j int) bool { return p.punctAt(j, "{", ";") })
}

// fn reads a function from its fn keyword; its declaration starts at token start.
func (p *rustParser) fn(prefix string, start int) {
	p.i++
	name := p.tok()
	if name.kind != tIdent {
		p.skipItem()
		return
	}
	end := p.header()
	p.syms.add(prefix+rustID(name.text), KindFunction, p.line(start), text(p.toks[start:end]))
	p.i = end
	p.skipItem()
}

// typeItem reads a struct, enum, union or trait. The items of a trait are listed as "Trait::item".
func (p *rustParser) typeItem(prefix string, start int) {
	trait := p.is("trait")
	p.i++
	name := p.tok()
	if name.kind != tIdent {
		p.skipItem()
		return
	}
	id := prefix + rustID(name.text)
	end := p.header()
	sig := text(p.toks[start:end])
	p.i = end
	if !p.is("{") {
		p.syms.add(id, KindType, p.line(start), sig)
		p.skipItem()
		return
	}
	p.syms.add(id, KindType, p.line(start), sig+" { ... }")
	if !trait {
		p.skipGroup()
		return
	}
	p.i++
	p.items(id + "::")
	if p.is("}") {
		p.i++
	}
}

// impl reads an impl block: the items of an inherent impl become "Type::item"; a trait impl is
// skipped.
func (p *rustParser) impl() {
	p.i++
	if p.is("<") {
		p.skipAngles()
	}
	end := p.header()
	self, trait := p.toks[p.i:end], false
	depth := 0
scan:
	for k, t := range self {
		switch {
		case t.text == "<" && t.kind == tPunct:
			depth++
		case t.text == ">" && t.kind == tPunct:
			depth--
		case t.text == "where" && t.kind == tIdent && depth == 0:
			break scan
		case t.text == "for" && t.kind == tIdent && depth == 0 && !(k+1 < len(self) && self[k+1].text == "<"):
			self, trait = self[k+1:], true
			break scan
		}
	}
	p.i = end
	name := rustTypeName(self)
	if trait || name == "" || !p.is("{") {
		p.skipItem()
		return
	}
	p.i++
	p.items(name + "::")
	if p.is("}") {
		p.i++
	}
}

// rustTypeName returns the name of the type a path names: the last segment, without generic
// arguments ("Pool" for &'a crate::pool::Pool<T>), or "" for tuples, slices and the like.
func rustTypeName(toks []token) string {
	i := 0
	for i < len(toks) && (toks[i].text == "&" || toks[i].text == "&&" || toks[i].text == "mut" ||
		toks[i].kind == tIdent && strings.HasPrefix(toks[i].text, "'")) {
		i++
	}
	name := ""
	for i < len(toks) && toks[i].kind == tIdent {
		name = rustID(toks[i].text)
		i++
		if i < len(toks) && toks[i].text == "::" {
			i++
			continue
		}
		break
	}
	if name == "dyn" || name == "impl" {
		return ""
	}
	return name
}

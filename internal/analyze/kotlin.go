package analyze

import "github.com/SYZD-research/dirdoc/internal/model"

// ktOps are the operators of several characters that the parser looks for or must not split.
var ktOps = []string{"!==", "===", "...", "..<", "::", "->", "?.", "?:", "!!", "==", "!=", "<=", ">=", "&&",
	"||", "++", "--", "+=", "-=", "*=", "/=", "%=", ".."}

// lexKotlin reads Kotlin: nested comments, string templates, raw strings and quoted names.
func lexKotlin(src []byte) []token {
	s := newScanner(src)
	if s.has("#!") {
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
		case s.has(`"""`):
			s.ktString(true)
			s.emit(tString, start, line)
		case c == '"':
			s.ktString(false)
			s.emit(tString, start, line)
		case c == '\'':
			s.quoted('\'', true)
			s.emit(tString, start, line)
		case c == '`': // a quoted name: `my name`
			s.quoted('`', true)
			s.emit(tIdent, start, line)
		case isDigit(c) || c == '.' && isDigit(s.peek(1)):
			s.number()
			s.emit(tNumber, start, line)
		case s.identStart(""):
			s.ident("")
			s.emit(tIdent, start, line)
		default:
			s.punct(ktOps)
			s.emit(tPunct, start, line)
		}
	}
}

// ktString moves past a string literal, "..." or a raw """...""", with its ${...} templates.
func (s *scanner) ktString(raw bool) {
	if raw {
		s.advance(3)
	} else {
		s.advance(1)
	}
	for !s.eof() {
		switch c := s.src[s.pos]; {
		case raw && s.has(`"""`):
			s.advance(3)
			for s.peek(0) == '"' { // """"x"""" ends with the last three quotes
				s.advance(1)
			}
			return
		case !raw && c == '"':
			s.advance(1)
			return
		case !raw && c == '\\':
			s.advance(2)
		case !raw && c == '\n':
			return
		case c == '$' && s.peek(1) == '{':
			s.advance(2)
			s.substitution(s.ktLiteral)
		default:
			s.advance(1)
		}
	}
}

// substitution moves past the expression of a ${...} (its opening brace already read) to just
// after its closing brace. literal moves past a literal at the current byte, if there is one.
func (s *scanner) substitution(literal func() bool) {
	depth := 1
	for !s.eof() {
		switch c := s.src[s.pos]; {
		case literal():
		case c == '{':
			depth++
			s.advance(1)
		case c == '}':
			depth--
			s.advance(1)
			if depth == 0 {
				return
			}
		case s.has("//"):
			s.lineComment()
		case s.has("/*"):
			s.blockComment(false)
		default:
			s.advance(1)
		}
	}
}

// ktLiteral moves past the string, character or quoted name at the current byte, if there is one.
func (s *scanner) ktLiteral() bool {
	switch c := s.peek(0); {
	case s.has(`"""`):
		s.ktString(true)
	case c == '"':
		s.ktString(false)
	case c == '\'' || c == '`':
		s.quoted(c, true)
	default:
		return false
	}
	return true
}

// ktModifiers are the modifiers of declarations.
var ktModifiers = map[string]bool{"public": true, "private": true, "protected": true, "internal": true,
	"open": true, "final": true, "abstract": true, "sealed": true, "data": true, "enum": true,
	"annotation": true, "inner": true, "value": true, "inline": true, "noinline": true, "crossinline": true,
	"override": true, "lateinit": true, "const": true, "suspend": true, "tailrec": true, "operator": true,
	"infix": true, "external": true, "expect": true, "actual": true, "companion": true, "vararg": true,
	"reified": true}

// kotlinFile lists classes, interfaces, objects, type aliases, functions and properties declared at
// the top level, and the functions and nested types of classes as "Class.member". Properties of
// objects and companion objects are listed (as "Object.name" and "Class.name"); those of classes are
// not. Extension functions are named "Receiver.name".
func kotlinFile(src []byte) []model.Symbol {
	p := &ktParser{parser{toks: lexKotlin(src)}}
	p.declarations("", 'f')
	return p.syms.list()
}

type ktParser struct{ parser }

// continues reports whether token j, the first of its line, goes on with the statement before it.
func (p *ktParser) continues(j int) bool {
	prev, t := p.toks[j-1], p.toks[j]
	if prev.kind == tPunct {
		switch prev.text {
		case ")", "]", "}", ">", "++", "--", "!!", "?":
		default:
			return true
		}
	}
	if t.kind == tPunct {
		switch t.text {
		case ".", "?.", "?:", "&&", "||", ":", "=", "->", "::", "+", "-", "*", "/", "%", "..", "..<", "==", "!=", "<=", ">=":
			return true
		}
		return false
	}
	return t.kind == tIdent && (t.text == "as" || t.text == "is" || t.text == "in" || t.text == "by" || t.text == "where")
}

// statementEnd returns where the statement at token from ends: at its ';', at the first token of
// the next statement, or at the '}' that closes the enclosing block.
func (p *ktParser) statementEnd(from int) int {
	save := p.i
	p.i = from
	end := p.find(false, func(j int) bool {
		return p.punctAt(j, ";") || j > from && p.toks[j].nl && !p.continues(j)
	})
	p.i = save
	return end
}

// skipStatement moves past the statement at the current token and its ';'.
func (p *ktParser) skipStatement() {
	p.i = p.statementEnd(p.i)
	if p.is(";") {
		p.i++
	}
}

// annotations moves past annotations: @Inject, @file:JvmName("x"), @[A B].
func (p *ktParser) annotations() {
	for p.is("@") {
		p.i++
		if p.is("[") {
			p.skipGroup()
			continue
		}
		if p.tok().kind == tIdent && p.at(1).text == ":" && p.at(2).kind == tIdent { // a use-site target
			p.i += 2
		}
		for p.tok().kind == tIdent {
			p.i++
			if !p.is(".") {
				break
			}
			p.i++
		}
		if p.is("<") {
			p.skipAngles()
		}
		if p.is("(") && !p.tok().nl {
			p.skipGroup()
		}
	}
}

// name returns a declared name, without the backquotes of a quoted one.
func ktName(t token) string {
	if q := unquote(t.text); q != "" {
		return q
	}
	return t.text
}

// declarations reads declarations until the '}' that closes their block. ctx is 'f' for a file,
// 'c' for the body of a class and 'o' for the body of an object.
func (p *ktParser) declarations(prefix string, ctx byte) {
	for !p.eof() && !p.is("}") {
		before := p.i
		p.declaration(prefix, ctx)
		if p.i == before {
			p.i++
		}
	}
}

// declaration reads a declaration with its annotations and modifiers, listing it as prefix+name.
func (p *ktParser) declaration(prefix string, ctx byte) {
	if p.is(";") || p.is(",") {
		p.i++
		return
	}
	if p.is("package") || p.is("import") {
		p.skipStatement()
		return
	}
	p.annotations()
	start, mods := p.i, map[string]bool{}
	for p.tok().kind == tIdent && ktModifiers[p.tok().text] && (p.at(1).kind == tIdent || p.at(1).text == "@") {
		mods[p.tok().text] = true
		p.i++
		p.annotations()
	}
	switch {
	case p.is("class") || p.is("interface") || p.is("fun") && p.at(1).text == "interface":
		p.class(prefix, start, mods["enum"])
	case p.is("object"):
		p.object(prefix, start, mods["companion"])
	case p.is("fun"):
		p.fun(prefix, start)
	case p.is("val") || p.is("var"):
		p.property(prefix, start, ctx)
	case p.is("typealias") && p.at(1).kind == tIdent:
		end := p.statementEnd(p.i)
		p.syms.add(prefix+ktName(p.at(1)), KindType, p.line(start), assignment(p.toks[start:end]))
		p.i = end
	case p.is("constructor") && ctx == 'c' && p.at(1).text == "(":
		end := p.groupEnd(p.i + 1)
		p.syms.add(prefix+"constructor", KindFunction, p.line(start), text(p.toks[start:end]))
		p.i = end
		if p.is(":") { // delegation: : this(...) or : super(...)
			p.i += 2
			if p.is("(") {
				p.skipGroup()
			}
		}
		if p.is("{") {
			p.skipGroup()
		}
	case p.is("init") && p.at(1).text == "{":
		p.i++
		p.skipGroup()
	default:
		p.skipStatement()
	}
}

// header returns the end of the declaration header that goes on at the current token (the one
// after the name): the '{' of its body (also on the next line), a '=' of an expression body when
// expr is set, or the end of the statement.
func (p *ktParser) header(expr bool) int {
	return p.find(true, func(j int) bool {
		switch {
		case p.punctAt(j, "{"), expr && p.punctAt(j, "="), p.punctAt(j, ";"):
			return true
		}
		return p.toks[j].nl && !p.continues(j)
	})
}

// class reads a class or interface and the declarations of its body (after the entries of an
// enum).
func (p *ktParser) class(prefix string, start int, enum bool) {
	if p.is("fun") {
		p.i++
	}
	p.i++
	name := p.tok()
	if name.kind != tIdent {
		p.skipStatement()
		return
	}
	p.i++
	end := p.header(false)
	sig := text(p.toks[start:end])
	p.i = end
	if !p.is("{") {
		p.syms.add(prefix+ktName(name), KindType, p.line(start), sig)
		if p.is(";") {
			p.i++
		}
		return
	}
	p.syms.add(prefix+ktName(name), KindType, p.line(start), sig+" { ... }")
	p.i++
	if enum { // the entries come first, up to a ';'
		p.i = p.find(false, func(j int) bool { return p.punctAt(j, ";") })
		if p.is(";") {
			p.i++
		}
	}
	p.declarations(prefix+ktName(name)+".", 'c')
	if p.is("}") {
		p.i++
	}
}

// object reads an object declaration; the members of a companion object belong to the class.
func (p *ktParser) object(prefix string, start int, companion bool) {
	p.i++
	name := ""
	if t := p.tok(); t.kind == tIdent && !t.nl {
		name = ktName(t)
		p.i++
	}
	end := p.header(false)
	sig := text(p.toks[start:end])
	p.i = end
	inner := prefix
	switch {
	case companion:
	case name == "": // an object expression at the start of a statement
		p.skipStatement()
		return
	default:
		body := ""
		if p.is("{") {
			body = " { ... }"
		}
		p.syms.add(prefix+name, KindType, p.line(start), sig+body)
		inner = prefix + name + "."
	}
	if p.is("{") {
		p.i++
		p.declarations(inner, 'o')
		if p.is("}") {
			p.i++
		}
	} else if p.is(";") {
		p.i++
	}
}

// receiverName returns the name of the receiver type in tokens such as "List<T>" or "Foo.Bar".
func receiverName(toks []token) string {
	name := ""
	depth := 0
	for _, t := range toks {
		switch {
		case t.text == "<" && t.kind == tPunct:
			depth++
		case t.text == ">" && t.kind == tPunct:
			depth--
		case depth == 0 && t.kind == tIdent:
			name = ktName(t)
		}
	}
	return name
}

// fun reads a function; an extension function is listed as Receiver.name.
func (p *ktParser) fun(prefix string, start int) {
	p.i++
	if p.is("<") {
		p.skipAngles()
	}
	// [Receiver.]name(
	k := p.find(true, func(j int) bool { return p.punctAt(j, "(", "{", "=", ";") || j > p.i && p.toks[j].nl })
	if !p.punctAt(k, "(") || k == p.i || p.toks[k-1].kind != tIdent {
		p.skipStatement()
		return
	}
	id := ktName(p.toks[k-1])
	if k-2 > p.i && p.punctAt(k-2, ".", "?.") {
		if recv := receiverName(p.toks[p.i : k-2]); recv != "" {
			id = recv + "." + id
		}
	}
	p.i = k
	end := p.header(true)
	p.syms.add(prefix+id, KindFunction, p.line(start), text(p.toks[start:end]))
	p.i = end
	switch {
	case p.is("{"):
		p.skipGroup()
	case p.is("="):
		p.i++
		p.skipStatement()
	case p.is(";"):
		p.i++
	}
}

// property reads a val or var, listed unless it belongs to a class (ctx 'c').
func (p *ktParser) property(prefix string, start int, ctx byte) {
	p.i++
	if p.is("<") {
		p.skipAngles()
	}
	if p.is("(") { // destructuring: val (a, b) = pair
		p.skipStatement()
		return
	}
	// [Receiver.]name [: Type] [= value | by delegate]
	k := p.find(true, func(j int) bool {
		return p.punctAt(j, ":", "=", ";") || p.toks[j].kind == tIdent && p.toks[j].text == "by" ||
			j > p.i && p.toks[j].nl && !p.continues(j)
	})
	if k == p.i || p.toks[k-1].kind != tIdent {
		p.skipStatement()
		return
	}
	id := ktName(p.toks[k-1])
	if k-2 > p.i && p.punctAt(k-2, ".", "?.") {
		if recv := receiverName(p.toks[p.i : k-2]); recv != "" {
			id = recv + "." + id
		}
	}
	end := p.statementEnd(p.i)
	if ctx != 'c' {
		toks := p.toks[start:end]
		sig := assignment(toks)
		if k < end && p.toks[k].kind == tIdent && p.toks[k].text == "by" { // a delegated property: val x by lazy { ... }
			sig = text(p.toks[start:k]) + " by " + value(p.toks[k+1:end])
		}
		p.syms.add(prefix+id, KindVariable, p.line(start), sig)
	}
	p.i = end
	if p.is(";") {
		p.i++
	}
}

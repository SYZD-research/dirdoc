package analyze

import (
	"strings"

	"github.com/SYZD-research/dirdoc/internal/model"
)

// jsOps are the operators of several characters that the parser looks for or must not split.
var jsOps = []string{">>>=", "...", "===", "!==", "**=", "<<=", "&&=", "||=", "??=",
	"=>", "==", "!=", "<=", ">=", "&&", "||", "??", "?.", "**", "++", "--", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "<<"}

// regexAfter lists the keywords after which a '/' starts a regular expression rather than divides.
var regexAfter = map[string]bool{"return": true, "typeof": true, "instanceof": true, "in": true, "of": true,
	"new": true, "delete": true, "void": true, "throw": true, "case": true, "do": true, "else": true,
	"yield": true, "await": true}

// lexJS reads TypeScript or JavaScript. With jsx set, an element where an expression starts is
// read as one literal.
func lexJS(src []byte, jsx bool) []token {
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
			s.blockComment(false)
		case c == '"' || c == '\'':
			s.quoted(c, true)
			s.emit(tString, start, line)
		case c == '`':
			s.template()
			s.emit(tString, start, line)
		case c == '/' && s.regexAllowed(jsx) && s.regex():
			s.emit(tString, start, line)
		case c == '<' && jsx && s.regexAllowed(jsx) && s.jsxElement():
			s.emit(tString, start, line)
		case isDigit(c) || c == '.' && isDigit(s.peek(1)):
			s.number()
			s.emit(tNumber, start, line)
		case s.identStart("$"):
			s.ident("$")
			s.emit(tIdent, start, line)
		case c == '#':
			s.pos++
			if s.identStart("$") { // a private name: #field
				s.ident("$")
				s.emit(tIdent, start, line)
			} else {
				s.emit(tPunct, start, line)
			}
		default:
			s.punct(jsOps)
			s.emit(tPunct, start, line)
		}
	}
}

// regexAllowed reports whether a '/' here starts a regular expression (and a '<' a JSX element),
// judged by the token before.
func (s *scanner) regexAllowed(jsx bool) bool {
	if len(s.toks) == 0 {
		return true
	}
	prev := s.toks[len(s.toks)-1]
	switch prev.kind {
	case tNumber, tString:
		return false
	case tIdent:
		return regexAfter[prev.text]
	}
	switch prev.text {
	case ")", "]", "}", "++", "--":
		return false
	case "<":
		return !jsx
	}
	return true
}

// regex moves past a regular expression literal and its flags. It returns false, without moving,
// when the line ends first: the '/' divides after all.
func (s *scanner) regex() bool {
	class := false
	for j := s.pos + 1; j < len(s.src); j++ {
		switch c := s.src[j]; {
		case c == '\n':
			return false
		case c == '\\':
			j++
		case c == '[':
			class = true
		case c == ']':
			class = false
		case c == '/' && !class:
			s.advance(j + 1 - s.pos)
			s.ident("")
			return true
		}
	}
	return false
}

// jsxElement moves past the JSX element or fragment that starts at a '<', with its attributes,
// text and nested expressions and elements. It returns false, without moving, when the '<' starts
// none: type parameters such as <T,> or <T extends U>, or an element that never ends.
func (s *scanner) jsxElement() bool {
	pos, line, space, nl := s.pos, s.line, s.space, s.nl
	ok := s.jsxTags()
	if !ok {
		s.pos, s.line = pos, line
	}
	s.space, s.nl = space, nl
	return ok
}

// jsxTags moves past the tags of an element and of its children, each from its '<', and the text
// between them.
func (s *scanner) jsxTags() bool {
	depth := 0
	for !s.eof() {
		closing := s.peek(1) == '/'
		if closing && depth == 0 {
			return false
		}
		s.advance(1)
		if closing {
			s.advance(1)
		}
		s.jsxBlank()
		name := s.pos
		s.ident("$.:-")
		if s.pos == name && s.peek(0) != '>' { // only a fragment has no name
			return false
		}
		if closing {
			s.jsxBlank()
			if s.peek(0) != '>' {
				return false
			}
			s.advance(1)
			if depth--; depth == 0 {
				return true
			}
		} else {
			open, ok := s.jsxAttributes(depth == 0)
			switch {
			case !ok:
				return false
			case open:
				depth++
			case depth == 0:
				return true
			}
		}
		for !s.eof() && s.peek(0) != '<' { // text and {expressions}
			if s.peek(0) == '{' {
				s.jsBraces(true)
			} else {
				s.advance(1)
			}
		}
	}
	return false
}

// jsxAttributes moves past the attributes of an opening tag and its end: '>' (open is true) or
// "/>". ok is false when something else follows, as in type parameters; outer is set for the tag
// of the outermost element, where <T extends U> declares type parameters.
func (s *scanner) jsxAttributes(outer bool) (open, ok bool) {
	for n := 0; ; n++ {
		s.jsxBlank()
		switch c := s.peek(0); {
		case c == '>':
			s.advance(1)
			return true, true
		case s.has("/>"):
			s.advance(2)
			return false, true
		case c == '{': // {...props}
			s.jsBraces(true)
		case s.identStart("$"):
			name := s.pos
			s.ident("$-:")
			if outer && n == 0 && s.src[name:s.pos] == "extends" {
				return false, false
			}
			s.jsxBlank()
			if s.peek(0) != '=' {
				continue
			}
			s.advance(1)
			s.jsxBlank()
			switch c := s.peek(0); {
			case c == '"' || c == '\'':
				s.quoted(c, false)
			case c == '{':
				s.jsBraces(true)
			case c == '<' && s.jsxElement():
			default:
				return false, false
			}
		default:
			return false, false
		}
	}
}

// jsxBlank moves past spaces, line breaks and comments inside a tag.
func (s *scanner) jsxBlank() {
	for !s.eof() {
		switch {
		case s.has("//"):
			s.lineComment()
		case s.has("/*"):
			s.blockComment(false)
		case strings.IndexByte(" \t\r\n\f", s.src[s.pos]) >= 0:
			s.advance(1)
		default:
			return
		}
	}
}

// jsBraces moves past a {...} expression from its '{' (the ${...} of a template literal, or an
// expression in JSX), with the literals, comments and regular expressions inside, and with jsx
// set, the elements.
func (s *scanner) jsBraces(jsx bool) {
	depth := 0
	last := byte('{') // the last byte before that is not blank
	for !s.eof() {
		switch c := s.src[s.pos]; {
		case c == '{':
			depth++
			s.advance(1)
		case c == '}':
			depth--
			s.advance(1)
			if depth == 0 {
				return
			}
		case s.jsLiteral():
		case s.has("//"):
			s.lineComment()
			continue
		case s.has("/*"):
			s.blockComment(false)
			continue
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			s.advance(1)
			continue
		case c == '/' && s.operandAfter(last) && s.regex():
		case c == '<' && jsx && s.operandAfter(last) && s.jsxElement():
		default:
			s.advance(1)
		}
		last = s.src[s.pos-1]
	}
}

// operandAfter reports whether an operand can start here, after the byte last that is not blank:
// after an operator, an opening bracket or a keyword such as return.
func (s *scanner) operandAfter(last byte) bool {
	if strings.IndexByte("({[,;?:=&|!~+-*%<>^", last) >= 0 {
		return true
	}
	before := strings.TrimRight(s.src[:s.pos], " \t\r\n")
	i := len(before)
	for i > 0 && (before[i-1] == '_' || before[i-1] == '$' || isDigit(before[i-1]) || 'a' <= before[i-1]|0x20 && before[i-1]|0x20 <= 'z') {
		i--
	}
	return regexAfter[before[i:]]
}

// template moves past a template literal, with the literals, comments and regular expressions in
// its ${...} substitutions.
func (s *scanner) template() {
	s.advance(1)
	for !s.eof() {
		switch c := s.src[s.pos]; {
		case c == '\\':
			s.advance(2)
		case c == '`':
			s.advance(1)
			return
		case c == '$' && s.peek(1) == '{':
			s.advance(1)
			s.jsBraces(false)
		default:
			s.advance(1)
		}
	}
}

// jsLiteral moves past the string or template literal at the current byte, if there is one.
func (s *scanner) jsLiteral() bool {
	switch c := s.peek(0); c {
	case '`':
		s.template()
	case '"', '\'':
		s.quoted(c, true)
	default:
		return false
	}
	return true
}

// jsFile lists functions, classes, interfaces, enums, type aliases and the variables a module
// declares at its top level (with or without export), and the methods of its classes as
// "Class.method". Fields of classes are listed when they are static or hold a function.
func jsFile(src []byte, ts, jsx bool) []model.Symbol {
	p := &jsParser{parser: parser{toks: lexJS(src, jsx)}, ts: ts}
	for !p.eof() {
		before := p.i
		p.statement()
		if p.i == before {
			p.i++
		}
	}
	return p.syms.list()
}

type jsParser struct {
	parser
	ts bool
}

// jsContinuers are the words after which a statement goes on past a line break. void is not one:
// at the end of a line it is the return type of a declaration.
var jsContinuers = map[string]bool{"extends": true, "implements": true, "new": true, "typeof": true,
	"instanceof": true, "in": true, "of": true, "keyof": true, "as": true, "satisfies": true, "case": true,
	"await": true, "delete": true, "else": true, "do": true}

// jsInfixWords are the words that continue a statement from the start of a line.
var jsInfixWords = map[string]bool{"instanceof": true, "in": true, "as": true, "satisfies": true,
	"extends": true, "implements": true}

// continues reports whether token j, the first of its line, goes on with the statement before it
// instead of starting another (automatic semicolon insertion, roughly).
func (p *jsParser) continues(j int) bool {
	prev, t := p.toks[j-1], p.toks[j]
	switch prev.kind {
	case tPunct:
		switch prev.text {
		case ")", "]", "}", "++", "--", ">": // a '>' ending a line closes type arguments
		default:
			return true
		}
	case tIdent:
		if jsContinuers[prev.text] {
			return true
		}
	}
	switch t.kind {
	case tPunct:
		switch t.text {
		case "{", "}", ";", "++", "--", "!", "~", "@":
			return false
		}
		return true
	case tString:
		return strings.HasPrefix(t.text, "`") // a tagged template
	case tIdent:
		return jsInfixWords[t.text]
	}
	return false
}

// statementEnd returns where the statement starting at token from ends: at its ';', at the first
// token of the next statement, or at the '}' that closes the enclosing block.
func (p *jsParser) statementEnd(from int) int {
	save := p.i
	p.i = from
	end := p.find(false, func(j int) bool {
		return p.punctAt(j, ";") || j > from && p.toks[j].nl && !p.continues(j)
	})
	p.i = save
	return end
}

// skipStatement moves past the statement at the current token. A compound statement ends with its
// last block, also when another statement follows on the same line (as in minified code).
func (p *jsParser) skipStatement() {
	switch {
	case p.is("{"):
		p.skipGroup()
	case p.is("if"), p.is("for"), p.is("while"), p.is("with"):
		kw := p.tok().text
		p.i++
		if p.is("await") { // for await (...)
			p.i++
		}
		if p.is("(") {
			p.skipGroup()
		}
		p.skipStatement()
		if kw == "if" && p.is("else") {
			p.i++
			p.skipStatement()
		}
	case p.is("switch"):
		p.i++
		if p.is("(") {
			p.skipGroup()
		}
		if p.is("{") {
			p.skipGroup()
		}
	case p.is("try"):
		p.i++
		for p.is("{") || p.is("catch") || p.is("finally") {
			if !p.is("{") {
				p.i++
			}
			if p.is("(") {
				p.skipGroup()
			}
			if p.is("{") {
				p.skipGroup()
			}
		}
	case p.is("do"):
		p.i++
		p.skipStatement()
		if p.is("while") {
			p.i++
			if p.is("(") {
				p.skipGroup()
			}
		}
		p.endStatement()
	case p.tok().kind == tIdent && p.at(1).text == ":" && !p.at(1).nl: // a label
		p.i += 2
		p.skipStatement()
	default:
		p.i = p.statementEnd(p.i)
		p.endStatement()
	}
}

// decorators moves past decorators: @Injectable(), @Component({ ... }).
func (p *jsParser) decorators() {
	for p.is("@") && p.at(1).kind == tIdent {
		p.i += 2
		for p.is(".") && p.at(1).kind == tIdent {
			p.i += 2
		}
		if p.is("(") {
			p.skipGroup()
		}
	}
}

// statement reads a statement of the module and lists what it declares.
func (p *jsParser) statement() {
	if p.is(";") || p.is("}") || p.is(")") || p.is("]") {
		p.i++
		return
	}
	p.decorators()
	start := p.i
	if p.is("export") {
		p.i++
		switch {
		case p.is("default"):
			p.i++
		case p.is("*") || p.is("{") || p.is("=") || p.is("as") || p.is("import") || p.is("type") && p.at(1).text == "{":
			p.skipStatement()
			return
		}
		p.decorators()
	}
	if p.is("declare") && p.at(1).kind == tIdent && !p.at(1).nl {
		p.i++
	}
	if p.is("abstract") && p.at(1).text == "class" {
		p.i++
	}
	switch {
	case p.is("function"), p.is("async") && p.at(1).text == "function" && !p.at(1).nl:
		p.function(start)
	case p.is("class"):
		p.class(start)
	case p.ts && (p.is("interface") || p.is("enum")) && p.at(1).kind == tIdent:
		p.namedBlock(start)
	case p.ts && p.is("const") && p.at(1).text == "enum" && p.at(2).kind == tIdent:
		p.i++
		p.namedBlock(start)
	case p.ts && p.is("type") && p.at(1).kind == tIdent && (p.at(2).text == "=" || p.at(2).text == "<"):
		end := p.statementEnd(p.i)
		p.syms.add(p.at(1).text, KindType, p.line(start), assignment(p.toks[start:end]))
		p.i = end
		p.endStatement()
	case (p.is("const") || p.is("let") || p.is("var")) && (p.at(1).kind == tIdent || p.at(1).text == "{" || p.at(1).text == "["):
		p.variables(start)
	default:
		p.skipStatement()
	}
}

// typeLiteralAfter reports whether a '{' after token j opens a type ({ id: string }) rather than a
// body.
func (p *jsParser) typeLiteralAfter(j int) bool {
	if j < 0 {
		return false
	}
	switch t := p.toks[j]; t.kind {
	case tPunct:
		switch t.text {
		case ":", "<", ",", "|", "&", "=>", "?", "(", "[", "=":
			return true
		}
	case tIdent:
		return t.text == "extends" || t.text == "keyof" || t.text == "typeof" || t.text == "as" || t.text == "satisfies"
	}
	return false
}

// bodyStart returns the index of the '{' that opens the body of the function or method whose
// header starts at the current token, or of the end of a declaration without a body (an overload
// or an abstract method).
func (p *jsParser) bodyStart() int {
	return p.find(false, func(j int) bool {
		t := p.toks[j]
		switch {
		case p.punctAt(j, "{"):
			return !p.typeLiteralAfter(j - 1)
		case p.punctAt(j, ";"):
			return true
		}
		return j > p.i && t.nl && !p.continues(j)
	})
}

// function reads a function declaration (async, a generator, or an overload without a body) from
// its keyword; the statement starts at token start.
func (p *jsParser) function(start int) {
	if p.is("async") {
		p.i++
	}
	p.i++
	if p.is("*") {
		p.i++
	}
	name := p.tok()
	if name.kind == tIdent {
		p.i++
	}
	end := p.bodyStart()
	if name.kind == tIdent { // else anonymous: export default function () {}
		p.syms.add(name.text, KindFunction, p.line(start), text(p.toks[start:end]))
	}
	p.i = end
	p.body()
}

// body moves past the body at the current token, or past the ';' that ends a declaration without
// one. Anything else already starts the next statement.
func (p *jsParser) body() {
	if p.is("{") {
		p.skipGroup()
	} else {
		p.endStatement()
	}
}

// endStatement moves past the ';' at the current token, if it is one.
func (p *jsParser) endStatement() {
	if p.is(";") {
		p.i++
	}
}

// header returns the end of a class, interface or enum header: its body's '{' or a ';'.
func (p *jsParser) header() int {
	return p.find(false, func(j int) bool {
		return p.punctAt(j, "{") && !p.typeLiteralAfter(j-1) || p.punctAt(j, ";")
	})
}

// class reads a class declaration and the members of its body.
func (p *jsParser) class(start int) {
	p.i++
	name := p.tok()
	if name.kind != tIdent || name.text == "extends" || name.text == "implements" { // anonymous
		p.i = p.header()
		p.body()
		return
	}
	p.i++
	end := p.header()
	p.syms.add(name.text, KindType, p.line(start), text(p.toks[start:end])+" { ... }")
	p.i = end
	if !p.is("{") {
		p.skipStatement()
		return
	}
	p.i++
	for !p.eof() && !p.is("}") {
		before := p.i
		p.member(name.text + ".")
		if p.i == before {
			p.i++
		}
	}
	if p.is("}") {
		p.i++
	}
}

// namedBlock reads an interface or an enum: a type whose members are not listed.
func (p *jsParser) namedBlock(start int) {
	p.i++
	name := p.tok()
	p.i++
	end := p.header()
	p.syms.add(name.text, KindType, p.line(start), text(p.toks[start:end])+" { ... }")
	p.i = end
	if p.is("{") {
		p.skipGroup()
	} else {
		p.skipStatement()
	}
}

// functionValue reports whether the value at the current token is a function expression (not one
// called at once) or an arrow function, and where its signature ends: before the body of a
// function expression, after the "=>" of an arrow function.
func (p *jsParser) functionValue() (bool, int) {
	j := p.i
	if p.is("async") && !p.at(1).nl {
		j++
	}
	if j >= len(p.toks) {
		return false, 0
	}
	switch t := p.toks[j]; {
	case t.kind == tIdent && t.text == "function":
		save := p.i
		p.i = j + 1
		end := p.bodyStart()
		p.i = save
		if p.punctAt(end, "{") && p.punctAt(p.groupEnd(end), "(", ".", "?.", "[") { // called at once: function () {}()
			return false, 0
		}
		return true, end
	case t.kind == tIdent && p.punctAt(j+1, "=>"):
		return true, j + 2
	case p.punctAt(j, "(", "<"):
		if p.punctAt(j, "<") {
			save := p.i
			p.i = j
			p.skipAngles()
			j = p.i
			p.i = save
		}
		if !p.punctAt(j, "(") {
			return false, 0
		}
		j = p.groupEnd(j)
		if p.punctAt(j, ":") { // a return type
			save := p.i
			p.i = j
			j = p.find(true, func(k int) bool {
				return p.punctAt(k, "=>", ",", ";", "=") || p.toks[k].nl && !p.continues(k)
			})
			p.i = save
		}
		if p.punctAt(j, "=>") {
			return true, j + 1
		}
	}
	return false, 0
}

// declarator reads the rest of "name [: type] [= value]" after the name, up to its end (a ',' or
// the end of the statement), and returns the index of its '=' (or of its end when it has no value)
// and of its end.
func (p *jsParser) declarator() (int, int) {
	eq := p.find(true, func(j int) bool {
		return p.punctAt(j, "=", ",", ";") || p.toks[j].nl && !p.continues(j)
	})
	if !p.punctAt(eq, "=") {
		return eq, eq
	}
	save := p.i
	p.i = eq + 1
	end := p.find(false, func(j int) bool {
		return p.punctAt(j, ",", ";") || j > eq+1 && p.toks[j].nl && !p.continues(j)
	})
	p.i = save
	return eq, end
}

// variables reads a const, let or var statement; each declarator is listed, as a function when
// its value is one.
func (p *jsParser) variables(start int) {
	kw := p.tok().text
	p.i++
	for first := true; !p.eof(); first = false {
		if p.is("{") || p.is("[") { // destructuring: no single name
			p.skipStatement()
			return
		}
		name := p.tok()
		if name.kind != tIdent {
			p.skipStatement()
			return
		}
		from, line, lead := start, p.line(start), ""
		if !first {
			from, line, lead = p.i, name.line, kw+" "
		}
		p.i++
		eq, end := p.declarator()
		fn, sigEnd := false, 0
		if p.punctAt(eq, "=") {
			save := p.i
			p.i = eq + 1
			fn, sigEnd = p.functionValue()
			p.i = save
		}
		if fn {
			sig := lead + text(p.toks[from:sigEnd])
			if p.punctAt(sigEnd-1, "=>") {
				sig += " ..."
			}
			p.syms.add(name.text, KindFunction, line, sig)
		} else {
			p.syms.add(name.text, KindVariable, line, lead+assignment(p.toks[from:end]))
		}
		p.i = end
		if !p.is(",") {
			break
		}
		p.i++
	}
	p.endStatement()
}

// jsModifiers are the words that can come before the name of a class member.
var jsModifiers = map[string]bool{"public": true, "private": true, "protected": true, "static": true,
	"readonly": true, "abstract": true, "override": true, "declare": true, "async": true, "get": true,
	"set": true, "accessor": true}

// modifierHere reports whether the modifier word at the current token modifies a member rather than
// names one (static() {}, get = 1).
func (p *jsParser) modifierHere() bool {
	next := p.at(1)
	if next.nl && (p.is("async") || p.is("get") || p.is("set")) {
		return false
	}
	switch next.kind {
	case tIdent, tString, tNumber:
		return true
	case tPunct:
		return next.text == "[" || next.text == "*" || next.text == "{" && p.is("static")
	}
	return false
}

// member reads a member of a class body (a method, an accessor, the constructor, a field or a
// static block), listing it as prefix+name.
func (p *jsParser) member(prefix string) {
	if p.is(";") || p.is(",") {
		p.i++
		return
	}
	p.decorators()
	start, static := p.i, false
	for p.tok().kind == tIdent && jsModifiers[p.tok().text] && p.modifierHere() {
		static = static || p.is("static")
		p.i++
	}
	if p.is("{") { // a static initialization block
		p.skipGroup()
		return
	}
	if p.is("*") {
		p.i++
	}
	name := ""
	switch t := p.tok(); {
	case t.kind == tIdent:
		if !strings.HasPrefix(t.text, "#") { // private names stay unlisted
			name = t.text
		}
		p.i++
	case t.kind == tString:
		name = unquote(t.text)
		p.i++
	case t.kind == tNumber:
		p.i++
	case p.is("["): // a computed name or an index signature
		p.skipGroup()
	default:
		p.skipStatement()
		return
	}
	if p.is("?") || p.is("!") {
		p.i++
	}
	if p.is("<") {
		p.skipAngles()
	}
	if p.is("(") {
		end := p.bodyStart()
		if name != "" {
			p.syms.add(prefix+name, KindFunction, p.line(start), text(p.toks[start:end]))
		}
		p.i = end
		p.body()
		return
	}
	eq, end := p.declarator()
	fn, sigEnd := false, 0
	if p.punctAt(eq, "=") {
		save := p.i
		p.i = eq + 1
		fn, sigEnd = p.functionValue()
		p.i = save
	}
	switch {
	case name == "":
	case fn:
		sig := text(p.toks[start:sigEnd])
		if p.punctAt(sigEnd-1, "=>") {
			sig += " ..."
		}
		p.syms.add(prefix+name, KindFunction, p.line(start), sig)
	case static:
		p.syms.add(prefix+name, KindVariable, p.line(start), assignment(p.toks[start:end]))
	}
	p.i = end
	if p.is(";") || p.is(",") {
		p.i++
	}
}

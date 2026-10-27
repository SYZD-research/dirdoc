package analyze

import "github.com/SYZD-research/dirdoc/internal/model"

// javaOps are the operators of several characters that the parser looks for or must not split.
var javaOps = []string{">>>=", "<<=", "...", "::", "->", "==", "!=", "<=", ">=", "&&", "||", "++", "--",
	"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^="}

// lexJava reads Java, with text blocks.
func lexJava(src []byte) []token {
	s := newScanner(src)
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
		case s.has(`"""`): // a text block
			s.advance(3)
			s.textBlock()
			s.emit(tString, start, line)
		case c == '"' || c == '\'':
			s.quoted(c, true)
			s.emit(tString, start, line)
		case isDigit(c) || c == '.' && isDigit(s.peek(1)):
			s.number()
			s.emit(tNumber, start, line)
		case s.identStart("$"):
			s.ident("$")
			s.emit(tIdent, start, line)
		default:
			s.punct(javaOps)
			s.emit(tPunct, start, line)
		}
	}
}

// textBlock moves past the rest of a """ text block, whose escapes can hide quotes.
func (s *scanner) textBlock() {
	for !s.eof() {
		switch {
		case s.peek(0) == '\\':
			s.advance(2)
		case s.has(`"""`):
			s.advance(3)
			return
		default:
			s.advance(1)
		}
	}
}

// javaModifiers are the modifiers of declarations ("non" starts non-sealed).
var javaModifiers = map[string]bool{"public": true, "protected": true, "private": true, "static": true,
	"final": true, "abstract": true, "native": true, "synchronized": true, "transient": true,
	"volatile": true, "strictfp": true, "default": true, "sealed": true, "non": true}

// javaFile lists the classes, interfaces, enums, records and annotation types of a file, nested ones
// as "Outer.Inner", and their methods and constructors as "Class.method" ("Class.Class" for a
// constructor). Fields are listed when they are static, which all fields of an interface are.
func javaFile(src []byte) []model.Symbol {
	p := &javaParser{parser{toks: lexJava(src)}}
	p.members("", "", false)
	return p.syms.list()
}

type javaParser struct{ parser }

// annotations moves past annotations (@Override, @Retention(RUNTIME)), but not "@interface".
func (p *javaParser) annotations() {
	for p.is("@") && p.at(1).kind == tIdent && p.at(1).text != "interface" {
		p.i += 2
		for p.is(".") && p.at(1).kind == tIdent {
			p.i += 2
		}
		if p.is("(") {
			p.skipGroup()
		}
	}
}

// signature prints the tokens of a declaration without its annotations.
func signature(toks []token) string {
	var kept []token
	for i := 0; i < len(toks); i++ {
		if toks[i].text == "@" && toks[i].kind == tPunct && i+1 < len(toks) && toks[i+1].kind == tIdent && toks[i+1].text != "interface" {
			i++
			for i+2 < len(toks) && toks[i+1].text == "." && toks[i+2].kind == tIdent {
				i += 2
			}
			if i+1 < len(toks) && toks[i+1].text == "(" {
				depth := 0
				for i++; i < len(toks); i++ {
					if toks[i].text == "(" {
						depth++
					} else if toks[i].text == ")" {
						if depth--; depth == 0 {
							break
						}
					}
				}
			}
			continue
		}
		kept = append(kept, toks[i])
	}
	if len(kept) > 0 {
		kept[0].space = false
	}
	return text(kept)
}

// members reads the declarations of a file (cls "") or of the body of the type cls, whose fields
// are static when iface is set.
func (p *javaParser) members(prefix, cls string, iface bool) {
	for !p.eof() && !p.is("}") {
		before := p.i
		p.member(prefix, cls, iface)
		if p.i == before {
			p.i++
		}
	}
}

// member reads a declaration: a type, a method or constructor, fields, or an initializer block.
func (p *javaParser) member(prefix, cls string, iface bool) {
	if p.is(";") {
		p.i++
		return
	}
	if cls == "" && (p.is("package") || p.is("import") || p.is("module") || p.is("open") && p.at(1).text == "module") {
		p.skipItem()
		return
	}
	p.annotations()
	start, static := p.i, iface
	for p.tok().kind == tIdent && javaModifiers[p.tok().text] {
		static = static || p.is("static")
		if p.is("non") && p.at(1).text == "-" && p.at(2).text == "sealed" {
			p.i += 2
		}
		p.i++
		p.annotations()
	}
	switch {
	case p.is("{"): // an initializer block
		p.skipGroup()
		return
	case p.is("class"), p.is("interface"), p.is("enum"), p.is("@") && p.at(1).text == "interface",
		p.is("record") && p.at(1).kind == tIdent && p.at(2).text != "=" && p.at(2).text != ";":
		p.typeDecl(prefix, start)
		return
	}
	// A method or constructor has '(' right after its name; a field has '=', ',' or ';'.
	k := p.find(true, func(j int) bool {
		return p.punctAt(j, "(") && j > p.i && p.toks[j-1].kind == tIdent || p.punctAt(j, "=", ",", ";", "{")
	})
	if k >= len(p.toks) || k == p.i {
		p.skipItem()
		return
	}
	name := p.toks[k-1]
	switch {
	case p.punctAt(k, "("):
		id := prefix + name.text
		end := p.find(false, func(j int) bool { return p.punctAt(j, "{", ";") })
		p.syms.add(id, KindFunction, p.line(start), signature(p.toks[start:end]))
		p.i = end
		p.skipItem()
	case p.punctAt(k, "{"): // a compact record constructor: Point { ... }
		if name.text == cls && k-1 == p.i {
			p.syms.add(prefix+name.text, KindFunction, p.line(start), signature(p.toks[start:k]))
		}
		p.i = k
		p.skipGroup()
	default:
		p.fields(prefix, start, k, static)
	}
}

// fields reads a field declaration whose first declarator name ends just before token k.
func (p *javaParser) fields(prefix string, start, k int, static bool) {
	typ := p.toks[start : k-1]
	for {
		name := p.toks[k-1]
		p.i = k
		end := p.find(false, func(j int) bool { return p.punctAt(j, ",", ";") })
		if static && name.kind == tIdent {
			sig := signature(append(append([]token(nil), typ...), name))
			if p.punctAt(k, "=") {
				sig += " = " + value(p.toks[k+1:end])
			}
			p.syms.add(prefix+name.text, KindVariable, name.line, sig)
		}
		p.i = end
		if !p.is(",") || p.at(1).kind != tIdent {
			break
		}
		k = p.i + 2
	}
	p.skipItem()
}

// typeDecl reads a class, interface, enum, record or annotation type and the members of its body.
func (p *javaParser) typeDecl(prefix string, start int) {
	if p.is("@") {
		p.i++
	}
	kind := p.tok().text
	p.i++
	name := p.tok()
	if name.kind != tIdent {
		p.skipItem()
		return
	}
	end := p.find(true, func(j int) bool { return p.punctAt(j, "{", ";") })
	p.syms.add(prefix+name.text, KindType, p.line(start), signature(p.toks[start:end])+" { ... }")
	p.i = end
	if !p.is("{") {
		p.skipItem()
		return
	}
	p.i++
	if kind == "enum" { // the constants come first, up to a ';'
		p.i = p.find(false, func(j int) bool { return p.punctAt(j, ";") })
		if p.is(";") {
			p.i++
		}
	}
	p.members(prefix+name.text+".", name.text, kind == "interface")
	if p.is("}") {
		p.i++
	}
}

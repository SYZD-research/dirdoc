package analyze

import (
	"bytes"
	"go/ast"
	goparser "go/parser"
	"go/printer"
	gotoken "go/token"
	"unicode/utf8"

	"github.com/SYZD-research/dirdoc/internal/model"
)

// goFile names functions and types as they are written ("Parse", "Config"), methods as
// "Type.Method", and lists each name of a var or const declaration. init functions and blank
// identifiers are left out. It returns nil when the file does not parse.
func goFile(name string, src []byte) []model.Symbol {
	fset := gotoken.NewFileSet()
	f, err := goparser.ParseFile(fset, name, src, goparser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var c collector
	add := func(id, kind string, pos gotoken.Pos, sig string) {
		c.add(id, kind, fset.Position(pos).Line, sig)
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			id := d.Name.Name
			if d.Recv != nil {
				recv := receiverType(d.Recv)
				if recv == "" {
					continue
				}
				id = recv + "." + id
			} else if id == "init" || id == "_" {
				continue
			}
			add(id, KindFunction, d.Pos(), fmtNode(fset, &ast.FuncDecl{Recv: d.Recv, Name: d.Name, Type: d.Type}))
		case *ast.GenDecl:
			var repeated ast.Expr // the type a const spec without type and values repeats
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.Name != "_" {
						add(s.Name.Name, KindType, s.Name.Pos(), typeSignature(fset, s))
					}
				case *ast.ValueSpec:
					typ := s.Type
					if len(s.Values) > 0 {
						repeated = s.Type
					} else if typ == nil && d.Tok == gotoken.CONST {
						typ = repeated
					}
					for i, n := range s.Names {
						if n.Name == "_" {
							continue
						}
						sig := d.Tok.String() + " " + n.Name
						if typ != nil {
							sig += " " + fmtNode(fset, typ)
						}
						switch {
						case len(s.Values) == len(s.Names):
							sig += " = " + shortExpr(fset, s.Values[i])
						case len(s.Values) > 0:
							sig += " = " + shortExpr(fset, s.Values[0])
						}
						add(n.Name, KindVariable, n.Pos(), sig)
					}
				}
			}
		}
	}
	return c.list()
}

// receiverType returns the type name of a method receiver: "Pool" for (p *Pool) and (m Map[K, V]).
func receiverType(recv *ast.FieldList) string {
	if len(recv.List) == 0 {
		return ""
	}
	t := recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.ParenExpr:
			t = x.X
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// typeSignature prints a type declaration with the body of a struct or interface left out.
func typeSignature(fset *gotoken.FileSet, s *ast.TypeSpec) string {
	t := s.Type
	switch x := t.(type) {
	case *ast.StructType:
		t = ast.NewIdent("struct{}")
		if x.Fields != nil && len(x.Fields.List) > 0 {
			t = ast.NewIdent("struct{ ... }")
		}
	case *ast.InterfaceType:
		t = ast.NewIdent("interface{}")
		if x.Methods != nil && len(x.Methods.List) > 0 {
			t = ast.NewIdent("interface{ ... }")
		}
	}
	return "type " + fmtNode(fset, &ast.TypeSpec{Name: s.Name, TypeParams: s.TypeParams, Assign: s.Assign, Type: t})
}

// shortExpr prints the value of a var or const, abbreviating long ones: composite literals and
// function literals lose their bodies, calls their arguments, and anything else becomes "...".
func shortExpr(fset *gotoken.FileSet, e ast.Expr) string {
	s := fmtNode(fset, e)
	if utf8.RuneCountInString(s) <= maxValue {
		return s
	}
	switch x := e.(type) {
	case *ast.CompositeLit:
		if x.Type != nil {
			return fmtNode(fset, x.Type) + "{...}"
		}
		return "{...}"
	case *ast.FuncLit:
		return fmtNode(fset, x.Type) + " {...}"
	case *ast.CallExpr:
		if fn := fmtNode(fset, x.Fun); utf8.RuneCountInString(fn) <= maxValue {
			return fn + "(...)"
		}
	}
	return "..."
}

// fmtNode formats a node on one line.
func fmtNode(fset *gotoken.FileSet, node any) string {
	var b bytes.Buffer
	if err := printer.Fprint(&b, fset, node); err != nil {
		return ""
	}
	return tidy(b.String())
}

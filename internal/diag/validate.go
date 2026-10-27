package diag

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/SYZD-research/dirdoc/internal/analyze"
	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/resolve"
)

// Locator maps parts of a document to lines of the source file it is being compiled from.
type Locator interface {
	TopLine(key string) int
	EntryLine(section string, index int, key string) int
	NodeLine(n *model.TreeNode) int
}

// Validator checks documents against the rules of design §12.
type Validator struct {
	P    *resolve.Project
	Loc  Locator // nil for stored documents
	File string  // source file named in diagnostics together with Loc's lines
	FS   bool    // also compare with the file system (tree drift, orphaned PFDs)

	Stale        bool // with FS, report files changed since their PFD was compiled (check --stale)
	Undocumented bool // with FS, report symbols of the code that no entry documents (check --undocumented)
}

func (v *Validator) place(d *Diagnostic, line int) {
	if v.Loc != nil && v.File != "" {
		d.File, d.Line = v.File, line
	}
}

func (v *Validator) topLine(key string) int {
	if v.Loc == nil {
		return 0
	}
	return v.Loc.TopLine(key)
}

func (v *Validator) entryLine(section string, i int, key string) int {
	if v.Loc == nil {
		return 0
	}
	return v.Loc.EntryLine(section, i, key)
}

func (v *Validator) nodeLine(n *model.TreeNode) int {
	if v.Loc == nil {
		return 0
	}
	return v.Loc.NodeLine(n)
}

const idRule = "use letters, digits and _ . : $ - (1 to 128 characters)"

// DSD validates the DSD of dir.
func (v *Validator) DSD(dir string, d *model.DSD) List {
	var l List
	owner := resolve.DirOwner(dir)
	docLoc := v.P.Ref(owner)
	v.summary(&l, docLoc, d.Summary, v.P.Config.Lint.SummaryMax.DSD, true, v.topLine("summary"))
	v.refs(&l, owner, docLoc, v.topLine("summary"), d.Summary)

	seen := map[string]int{}
	for i, e := range d.Entries {
		loc := docLoc + "#" + e.ID
		if e.ID == "" {
			loc = fmt.Sprintf("%s dsd[%d]", docLoc, i)
		}
		line := v.entryLine(model.SectionDSD, i, "id")
		switch {
		case e.ID == "":
			v.place(l.Errorf(CodeIDInvalid, loc, "id is required (the tree links to dsd entries by id)"), line)
		case !model.ValidID(e.ID):
			v.place(l.Errorf(CodeIDInvalid, loc, "invalid id %q: %s", e.ID, idRule), line)
		default:
			if j, dup := seen[e.ID]; dup {
				v.place(l.Errorf(CodeIDDuplicate, loc, "duplicate id %q (also dsd[%d])", e.ID, j), line)
			} else {
				seen[e.ID] = i
			}
		}
		if strings.ContainsAny(e.Name, "\r\n") {
			v.place(l.Errorf(CodeParse, loc, "name must be a single line"), v.entryLine(model.SectionDSD, i, "name"))
		}
		v.refs(&l, owner, loc, v.entryLine(model.SectionDSD, i, "document"), e.Document)
		v.template(&l, loc, model.SectionKind(model.SectionDSD), e.Document, v.entryLine(model.SectionDSD, i, "document"))
	}

	linked := map[string]bool{}
	if d.Tree != nil {
		v.tree(&l, dir, d)
		d.Tree.Walk(func(n *model.TreeNode, rel string) { linked[n.DSD] = true })
	}
	for i, e := range d.Entries {
		if e.ID != "" && !linked[e.ID] {
			v.place(l.Warnf(CodeDSDUnlinked, docLoc+"#"+e.ID, "the entry is not linked from the tree"), v.entryLine(model.SectionDSD, i, "id"))
		}
	}
	return l
}

func (v *Validator) tree(l *List, dir string, d *model.DSD) {
	root := d.Tree
	nodeLoc := func(rel string, isDir bool) string {
		return v.P.Rel(filepath.Join(dir, filepath.FromSlash(rel)), isDir)
	}
	if root.Name != "." || root.Type != model.NodeDir {
		l.Errorf(CodeTreeSyntax, nodeLoc("", true), "the tree root must be the directory itself, named \".\"")
	}
	var visit func(n *model.TreeNode, rel string)
	visit = func(n *model.TreeNode, rel string) {
		loc, line := nodeLoc(rel, n.IsDir()), v.nodeLine(n)
		if rel != "" {
			if n.Name == "" || n.Name == "." || n.Name == ".." || strings.Contains(n.Name, "/") || strings.IndexFunc(n.Name, unicode.IsControl) >= 0 {
				v.place(l.Errorf(CodeTreeSyntax, loc, "invalid node name %q", n.Name), line)
			}
			if n.Type != model.NodeDir && n.Type != model.NodeFile {
				v.place(l.Errorf(CodeTreeSyntax, loc, "invalid node type %q (want dir or file)", n.Type), line)
			}
		}
		if !n.IsDir() && len(n.Children) > 0 {
			v.place(l.Errorf(CodeTreeSyntax, loc, "a file cannot have children"), line)
		}
		names := map[string]bool{}
		for _, c := range n.Children {
			if names[c.Name] {
				v.place(l.Errorf(CodeTreeSyntax, loc, "%q is listed twice", c.Name), v.nodeLine(c))
			}
			names[c.Name] = true
		}
		if n.DSD != "" {
			if !model.ValidID(n.DSD) {
				v.place(l.Errorf(CodeIDInvalid, loc, "invalid id %q in tree link: %s", n.DSD, idRule), line)
			} else if d.Entry(n.DSD) == nil {
				v.place(l.Errorf(CodeTreeLink, loc, "[[#%s]] does not match any dsd entry", n.DSD), line)
			}
		}
		for _, c := range n.Children {
			visit(c, path.Join(rel, c.Name))
		}
	}
	visit(root, "")
	if v.FS {
		v.treeFS(l, dir, root)
	}
}

// treeFS reports tree nodes that do not match the file system (design §7).
func (v *Validator) treeFS(l *List, dir string, root *model.TreeNode) {
	var visit func(n *model.TreeNode, abs string)
	visit = func(n *model.TreeNode, abs string) {
		if abs != dir {
			loc, line := v.P.Rel(abs, n.IsDir()), v.nodeLine(n)
			st, err := os.Stat(abs)
			if err != nil {
				v.place(l.Warnf(CodeTreeMissing, loc, "listed in the tree but does not exist"), line)
				return
			}
			if n.IsDir() != st.IsDir() {
				what := "a file"
				if st.IsDir() {
					what = "a directory"
				}
				v.place(l.Warnf(CodeTreeType, loc, "listed as a %s but is %s", n.Type, what), line)
				return
			}
		}
		if !n.IsDir() {
			return
		}
		if len(n.Children) > 0 && !n.More {
			if entries, err := v.P.Ignore.List(abs); err == nil {
				for _, e := range entries {
					if e.IsDir && n.Child(e.Name) == nil {
						v.place(l.Warnf(CodeTreeUnlisted, v.P.Rel(filepath.Join(abs, e.Name), true),
							"exists but is not listed in the tree (list it, or add \"...\" to its parent)"), v.nodeLine(n))
					}
				}
			}
		}
		for _, c := range n.Children {
			visit(c, filepath.Join(abs, c.Name))
		}
	}
	visit(root, dir)
}

// PFD validates the PFD of file.
func (v *Validator) PFD(file string, p *model.PFD) List {
	var l List
	owner := resolve.FileOwner(file)
	docLoc := v.P.Ref(owner)
	if p.Target != filepath.Base(file) {
		l.Errorf(CodeTargetMismatch, docLoc, "target %q does not match the file name %q", p.Target, filepath.Base(file))
	}
	lim := v.P.Config.Lint.SummaryMax
	v.summary(&l, docLoc, p.Summary, lim.PFD, true, v.topLine("summary"))
	v.refs(&l, owner, docLoc, v.topLine("summary"), p.Summary)

	seen := map[string]string{}
	for _, sec := range model.PFDSections {
		for i, e := range p.Section(sec) {
			label := fmt.Sprintf("%s[%d]", sec, i)
			loc := docLoc + "#" + e.ID
			if e.ID == "" {
				loc = docLoc + " " + label
			}
			line := func(key string) int { return v.entryLine(sec, i, key) }
			switch {
			case strings.TrimSpace(e.Name) == "":
				v.place(l.Errorf(CodeParse, loc, "%s: name is required", label), line("name"))
			case strings.ContainsAny(e.Name, "\r\n"):
				v.place(l.Errorf(CodeParse, loc, "%s: name must be a single line", label), line("name"))
			}
			switch {
			case e.ID == "" && strings.TrimSpace(e.Name) != "":
				v.place(l.Errorf(CodeIDInvalid, loc, "cannot derive an id from name %q; set id explicitly", e.Name), line("name"))
			case e.ID == "":
				v.place(l.Errorf(CodeIDInvalid, loc, "%s: id is required", label), line("id"))
			case !model.ValidID(e.ID):
				v.place(l.Errorf(CodeIDInvalid, loc, "invalid id %q: %s", e.ID, idRule), line("id"))
			default:
				if prev, dup := seen[e.ID]; dup {
					v.place(l.Errorf(CodeIDDuplicate, loc, "duplicate id %q (also used by %s)", e.ID, prev), line("id"))
				} else {
					seen[e.ID] = label
				}
			}
			if strings.TrimSpace(e.Summary) == "" {
				v.place(l.Errorf(CodeParse, loc, "%s: summary is required", label), line("summary"))
			} else {
				v.summary(&l, loc, e.Summary, lim.Entry, false, line("summary"))
			}
			if sec == model.SectionVariables && e.Document != "" {
				v.place(l.Errorf(CodeParse, loc, "variables have no document"), line("document"))
			}
			v.refs(&l, owner, loc, line("summary"), e.Summary)
			v.refs(&l, owner, loc, line("document"), e.Document)
			v.template(&l, loc, model.SectionKind(sec), e.Document, line("document"))
		}
	}
	if v.FS {
		if _, err := os.Stat(file); err != nil {
			l.Warnf(CodeOrphanPFD, docLoc, "the documented file does not exist")
		} else {
			v.code(&l, file, docLoc, p)
		}
	}
	return l
}

// code compares p with the documented file: whether the file changed since p was compiled, and which
// of its symbols no entry documents.
func (v *Validator) code(l *List, file, docLoc string, p *model.PFD) {
	if v.Stale {
		if s := v.P.Stale(file, p); s != nil && *s {
			l.Warnf(CodeStale, docLoc, "the file has changed since this document was last compiled "+
				"(update the document, or run `dirdoc touch` on the file if it is still accurate)")
		}
	}
	if !v.Undocumented {
		return
	}
	src, err := os.ReadFile(file)
	if err != nil {
		return
	}
	documented := map[string]bool{}
	for _, sec := range model.PFDSections {
		for _, e := range p.Section(sec) {
			documented[e.ID] = true
		}
	}
	for _, s := range analyze.File(file, src) {
		if !documented[s.ID] {
			l.Warnf(CodeUndocumented, docLoc, "%s %s (line %d) has no entry", s.Kind, s.ID, s.Line)
		}
	}
}

// template reports the headings dirdoc.toml requires of documents of kind that doc lacks. An empty
// document is not checked: documents are optional.
func (v *Validator) template(l *List, loc, kind, doc string, line int) {
	required := v.P.Config.Template.Headings(kind)
	if len(required) == 0 || strings.TrimSpace(doc) == "" {
		return
	}
	have := Headings(doc)
	for _, want := range required {
		found := false
		for _, h := range have {
			if strings.EqualFold(h, want) {
				found = true
				break
			}
		}
		if !found {
			v.place(l.Warnf(CodeTemplateHeading, loc, "the document has no %q heading (required by [template] in dirdoc.toml)", want), line)
		}
	}
}

// Headings returns the text of the headings of a Markdown document, in order.
func Headings(md string) []string {
	src := []byte(md)
	doc := goldmark.DefaultParser().Parse(text.NewReader(src))
	var out []string
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !ok || !entering {
			return ast.WalkContinue, nil
		}
		var b strings.Builder
		ast.Walk(h, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
			if t, ok := c.(*ast.Text); ok && entering {
				b.Write(t.Segment.Value(src))
				if t.SoftLineBreak() || t.HardLineBreak() {
					b.WriteByte(' ')
				}
			}
			return ast.WalkContinue, nil
		})
		out = append(out, strings.TrimSpace(b.String()))
		return ast.WalkSkipChildren, nil
	})
	return out
}

func (v *Validator) summary(l *List, loc, s string, max int, docLevel bool, line int) {
	if docLevel && strings.TrimSpace(s) == "" {
		v.place(l.Warnf(CodeSummaryEmpty, loc, "summary is empty"), line)
		return
	}
	if n := utf8.RuneCountInString(s); max > 0 && n > max {
		v.place(l.Warnf(CodeSummaryLong, loc, "summary is %d characters long (limit %d); keep it short", n, max), line)
	}
}

// notRefHint helps with [[...]] written as plain text, such as TOML table names.
const notRefHint = " (if it is not a reference, put it in backticks)"

func (v *Validator) refs(l *List, owner resolve.Owner, loc string, line int, text string) {
	for _, r := range resolve.FindRefs(text) {
		res := v.P.Resolve(owner, r)
		switch res.Status {
		case resolve.RefMissingLocal:
			v.place(l.Errorf(CodeRefLocal, loc, "%s does not exist in this document%s", r.Raw, notRefHint), line)
		case resolve.RefOutside:
			v.place(l.Errorf(CodeRefOutside, loc, "%s points outside the project root", r.Raw), line)
		case resolve.RefBroken:
			v.place(l.Warnf(CodeRefBroken, loc, "cannot resolve %s: %s%s", r.Raw, res.Reason, notRefHint), line)
		}
	}
}

// LoadError converts an error from loading a stored document into a diagnostic.
func LoadError(path string, err error) Diagnostic {
	code := CodeParse
	var ve *model.VersionError
	if errors.As(err, &ve) {
		code = CodeVersion
	}
	msg := err.Error()
	if de, ok := err.(*resolve.DocError); ok {
		msg = de.Err.Error()
	}
	return Diagnostic{Severity: Error, Code: code, Path: path, Message: msg}
}

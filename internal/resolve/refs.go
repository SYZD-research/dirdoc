package resolve

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/SYZD-research/dirdoc/internal/model"
)

// Ref is one [[...]] reference found in Markdown (design §6.2).
type Ref struct {
	Start, End int    // byte range of the whole [[...]]
	Raw        string // the whole [[...]]
	Target     string // before "|"
	Label      string // after "|"
	Path       string // before the last "#"; "" means the same document
	ID         string // after the last "#"
}

var refRe = regexp.MustCompile(`\[\[([^\[\]\n]+)\]\]`)

// FindRefs returns the references in md, skipping code spans, code blocks and \[[ escapes.
func FindRefs(md string) []Ref {
	if !strings.Contains(md, "[[") {
		return nil
	}
	code := CodeRanges(md)
	var refs []Ref
	for _, m := range refRe.FindAllStringSubmatchIndex(md, -1) {
		start, end := m[0], m[1]
		if start > 0 && md[start-1] == '\\' {
			continue
		}
		if inRanges(code, start) {
			continue
		}
		target, label, _ := strings.Cut(md[m[2]:m[3]], "|")
		target, label = strings.TrimSpace(target), strings.TrimSpace(label)
		r := Ref{Start: start, End: end, Raw: md[start:end], Target: target, Label: label, Path: target}
		if i := strings.LastIndex(target, "#"); i >= 0 {
			r.Path, r.ID = target[:i], target[i+1:]
		}
		refs = append(refs, r)
	}
	return refs
}

// CodeRanges returns the byte ranges of code spans, code blocks and HTML blocks in md.
func CodeRanges(md string) [][2]int {
	src := []byte(md)
	doc := goldmark.DefaultParser().Parse(text.NewReader(src))
	var out [][2]int
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindCodeSpan:
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					out = append(out, [2]int{t.Segment.Start, t.Segment.Stop})
				}
			}
			return ast.WalkSkipChildren, nil
		case ast.KindFencedCodeBlock, ast.KindCodeBlock, ast.KindHTMLBlock:
			lines := n.Lines()
			for i := 0; i < lines.Len(); i++ {
				s := lines.At(i)
				out = append(out, [2]int{s.Start, s.Stop})
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return out
}

func inRanges(ranges [][2]int, pos int) bool {
	for _, r := range ranges {
		if pos >= r[0] && pos < r[1] {
			return true
		}
	}
	return false
}

// Status is the outcome of resolving a reference.
type Status int

const (
	RefOK           Status = iota
	RefBroken              // the target does not exist (yet)
	RefMissingLocal        // [[#id]] is not in the same document
	RefOutside             // the target is outside the project root
)

// Resolved describes the target of a reference.
type Resolved struct {
	Status  Status
	Reason  string
	Ref     string // display form relative to the current directory, e.g. "src/main.rs#run"
	Abs     string
	IsDir   bool
	ID      string
	Kind    string // directory, file, type, function, variable or dsd-entry
	Name    string
	Summary string
}

// Text returns the reference rewritten to its display form, keeping the label.
func (r Resolved) Text(ref Ref) string {
	if r.Status == RefOutside {
		return ref.Raw
	}
	if ref.Label != "" {
		return "[[" + r.Ref + "|" + ref.Label + "]]"
	}
	return "[[" + r.Ref + "]]"
}

// Target returns the path r points to from a document of owner and whether it is a directory,
// without loading anything. outside reports a path outside the project root.
func (p *Project) Target(owner Owner, r Ref) (abs string, isDir, outside bool) {
	if r.Path == "" {
		return owner.Path, owner.Kind == model.KindDSD, false
	}
	rel, base := r.Path, owner.Dir()
	if strings.HasPrefix(rel, "/") {
		rel, base = strings.TrimLeft(rel, "/"), p.Root
	}
	abs = filepath.Join(base, filepath.FromSlash(rel))
	isDir = rel == "" || strings.HasSuffix(rel, "/") || path.Base(rel) == "." || path.Base(rel) == ".."
	if !isDir {
		if st, err := os.Stat(abs); err == nil && st.IsDir() {
			isDir = true
		}
	}
	return abs, isDir, !p.Within(abs)
}

// Resolve finds the target of r written in a document of owner.
func (p *Project) Resolve(owner Owner, r Ref) Resolved {
	abs, isDir, outside := p.Target(owner, r)
	res := Resolved{Abs: abs, IsDir: isDir, ID: r.ID}
	if outside {
		res.Status, res.Reason, res.Ref = RefOutside, "outside the project root", r.Target
		return res
	}
	res.Ref = p.Rel(abs, isDir)
	if r.ID != "" {
		res.Ref += "#" + r.ID
	}
	p.lookup(&res)
	if r.Path == "" && r.ID != "" && res.Status == RefBroken {
		res.Status = RefMissingLocal
	}
	return res
}

func (p *Project) lookup(res *Resolved) {
	broken := func(format string, args ...any) {
		res.Status, res.Reason = RefBroken, fmt.Sprintf(format, args...)
	}
	if res.IsDir {
		d, err := p.DSD(res.Abs)
		if err != nil {
			broken("cannot read its DSD: %v", err)
			return
		}
		if d == nil {
			if res.ID != "" {
				broken("the directory has no DSD")
				return
			}
			inh := p.Inherited(res.Abs)
			if inh == nil {
				broken("the directory is not documented")
				return
			}
			res.Kind, res.Name, res.Summary = "directory", p.Rel(res.Abs, true), inh.Node.Note
			return
		}
		if res.ID == "" {
			res.Kind, res.Name, res.Summary = "directory", p.Rel(res.Abs, true), d.Summary
			return
		}
		if d.Entry(res.ID) == nil {
			broken("no dsd entry %q", res.ID)
			return
		}
		res.Kind, res.Name = "dsd-entry", d.DisplayName(res.ID, filepath.Base(res.Abs))
		return
	}
	d, err := p.PFD(res.Abs)
	if err != nil {
		broken("cannot read its PFD: %v", err)
		return
	}
	if d == nil {
		broken("the file has no PFD")
		return
	}
	if res.ID == "" {
		res.Kind, res.Name, res.Summary = "file", p.Rel(res.Abs, false), d.Summary
		return
	}
	sec, e := d.Find(res.ID)
	if e == nil {
		broken("no entry %q", res.ID)
		return
	}
	res.Kind, res.Name, res.Summary = model.SectionKind(sec), e.Name, e.Summary
}

// Backlink is a place in the documentation that refers to a target.
type Backlink struct {
	From   Owner  // the document holding the reference
	FromID string // the entry holding it; "" for the document's summary
	ID     string // the entry of the target it points to; "" for the target itself
}

// Backlinks returns the places in the project's documentation that refer to target (a file or a
// directory) or to one of its entries, in the order check visits documents. A place that refers to
// the same thing more than once is listed once.
func (p *Project) Backlinks(target string) []Backlink {
	target = filepath.Clean(target)
	var out []Backlink
	seen := map[Backlink]bool{}
	p.Documents(p.Root, func(doc Document) {
		doc.Texts(func(id string, text *string) {
			for _, r := range FindRefs(*text) {
				if abs, _, outside := p.Target(doc.Owner, r); outside || abs != target {
					continue
				}
				b := Backlink{From: doc.Owner, FromID: id, ID: r.ID}
				if !seen[b] {
					seen[b] = true
					out = append(out, b)
				}
			}
		})
	})
	return out
}

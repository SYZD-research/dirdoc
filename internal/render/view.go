package render

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/SYZD-research/dirdoc/internal/analyze"
	"github.com/SYZD-research/dirdoc/internal/diag"
	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/resolve"
	"github.com/SYZD-research/dirdoc/internal/source"
)

// ViewVersion is the version of the JSON view schema (schema/view.v1.schema.json).
const ViewVersion = 1

// Level is how much a view includes (design §10.2).
type Level int

const (
	Brief Level = iota
	Normal
	Full
)

// ErrNotDocumented means the requested target has no documentation.
var ErrNotDocumented = errors.New("not documented")

// DirView is the view of a directory.
type DirView struct {
	DirdocView    int            `json:"dirdoc_view"`
	Kind          string         `json:"kind"`
	Path          string         `json:"path"`
	Source        *string        `json:"source"`
	InheritedFrom *InheritedView `json:"inherited_from"`
	Summary       string         `json:"summary"`
	Tree          *TreeNodeView  `json:"tree"`
	DSD           []DSDEntryView `json:"dsd"`
	Files         []FileSummary  `json:"files"`
	Subdirs       []SubdirView   `json:"subdirs"`
	Warnings      diag.List      `json:"warnings"`

	treeText string
}

// InheritedView tells where the description of a directory without its own DSD comes from.
type InheritedView struct {
	Source string `json:"source"`
	Ref    string `json:"ref,omitempty"`
	Note   string `json:"note,omitempty"`
}

// TreeNodeView is a tree node with what the display adds: the entry ref and the (dirdoc)/(pfd) marks.
type TreeNodeView struct {
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	DSD       string          `json:"dsd,omitempty"`
	Ref       string          `json:"ref,omitempty"`
	Note      string          `json:"note,omitempty"`
	More      bool            `json:"more,omitempty"`
	HasDirdoc bool            `json:"has_dirdoc,omitempty"`
	HasPFD    bool            `json:"has_pfd,omitempty"`
	Children  []*TreeNodeView `json:"children,omitempty"`
}

// DSDEntryView is a DSD entry with its resolved name and the tree paths that link to it.
type DSDEntryView struct {
	ID       string   `json:"id"`
	Ref      string   `json:"ref"`
	Name     string   `json:"name"`
	Paths    []string `json:"paths"`
	Document string   `json:"document,omitempty"`
}

// FileSummary is a documented file as listed in a directory view.
type FileSummary struct {
	Path      string        `json:"path"`
	Ref       string        `json:"ref"`
	Source    string        `json:"source"`
	Stale     *bool         `json:"stale"`
	Summary   string        `json:"summary"`
	Overview  *OverviewView `json:"overview,omitempty"`
	Types     *[]EntryBody  `json:"types,omitempty"`
	Functions *[]EntryBody  `json:"functions,omitempty"`
	Variables *[]EntryBody  `json:"variables,omitempty"`
}

// OverviewView is a PFD's table of contents. Undocumented lists the symbols of the file that no entry
// documents; it is absent when the file was not analyzed.
type OverviewView struct {
	Types        []OverviewItem  `json:"types"`
	Functions    []OverviewItem  `json:"functions"`
	Variables    []OverviewItem  `json:"variables"`
	Undocumented *[]model.Symbol `json:"undocumented,omitempty"`
}

// OverviewItem is one line of a table of contents. Line and Signature come from code analysis.
type OverviewItem struct {
	ID        string `json:"id"`
	Ref       string `json:"ref"`
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	Line      int    `json:"line,omitempty"`
	Signature string `json:"signature,omitempty"`
}

// EntryBody is a type, function or variable with its document.
type EntryBody struct {
	ID        string `json:"id"`
	Ref       string `json:"ref"`
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	Line      int    `json:"line,omitempty"`
	Signature string `json:"signature,omitempty"`
	Document  string `json:"document,omitempty"`
}

// SubdirView is a subdirectory that has its own .dirdoc.
type SubdirView struct {
	Path    string `json:"path"`
	Summary string `json:"summary"`
}

// FileView is the view of a file.
type FileView struct {
	DirdocView   int             `json:"dirdoc_view"`
	Kind         string          `json:"kind"`
	Path         string          `json:"path"`
	Ref          string          `json:"ref"`
	Source       string          `json:"source"`
	Stale        *bool           `json:"stale"`
	Summary      string          `json:"summary"`
	Overview     OverviewView    `json:"overview"`
	Types        *[]EntryBody    `json:"types,omitempty"`
	Functions    *[]EntryBody    `json:"functions,omitempty"`
	Variables    *[]EntryBody    `json:"variables,omitempty"`
	ReferencedBy *[]BacklinkView `json:"referenced_by,omitempty"`
	Warnings     diag.List       `json:"warnings"`

	language string // the code fence language of signatures
}

// EntryView is the view of one entry (PATH#ID).
type EntryView struct {
	DirdocView   int            `json:"dirdoc_view"`
	Kind         string         `json:"kind"`
	Ref          string         `json:"ref"`
	Path         string         `json:"path"`
	Source       string         `json:"source"`
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Stale        *bool          `json:"stale,omitempty"`
	Summary      string         `json:"summary,omitempty"`
	Line         int            `json:"line,omitempty"`
	Signature    string         `json:"signature,omitempty"`
	Document     string         `json:"document,omitempty"`
	Paths        []string       `json:"paths,omitempty"`
	Refs         []RefView      `json:"refs"`
	ReferencedBy []BacklinkView `json:"referenced_by"`
	Warnings     diag.List      `json:"warnings"`

	language string
}

// BacklinkView is a place in the documentation that refers to the viewed target. In a file view,
// Target is the part of the file it refers to.
type BacklinkView struct {
	Ref     string `json:"ref"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Summary string `json:"summary,omitempty"`
	Target  string `json:"target,omitempty"`
}

// RefView is a reference made by an entry, with the summary of its target.
type RefView struct {
	Ref      string `json:"ref"`
	Resolved bool   `json:"resolved"`
	Kind     string `json:"kind,omitempty"`
	Name     string `json:"name,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// ListView holds the directory views of `dirdoc -r`.
type ListView struct {
	DirdocView int        `json:"dirdoc_view"`
	Kind       string     `json:"kind"`
	Views      []*DirView `json:"views"`
}

// Renderer builds views for one project.
type Renderer struct {
	P     *resolve.Project
	Level Level
}

type warnings struct{ l diag.List }

func (w *warnings) add(d diag.Diagnostic) { w.l = append(w.l, d) }

func (w *warnings) list() diag.List {
	out := w.l.Dedup()
	if out == nil {
		return diag.List{}
	}
	return out
}

// displayCodes are the validator findings worth showing next to the documentation (design §10.2).
var displayCodes = map[string]bool{
	diag.CodeTreeMissing: true, diag.CodeTreeType: true, diag.CodeTreeUnlisted: true,
	diag.CodeOrphanPFD: true, diag.CodeTargetMismatch: true,
}

// text rewrites references (and shifts headings when top > 0), noting unresolvable references.
func (r *Renderer) text(md string, owner resolve.Owner, loc string, top int, w *warnings) string {
	return transform(md, top, func(ref resolve.Ref) string {
		res := r.P.Resolve(owner, ref)
		var l diag.List
		switch res.Status {
		case resolve.RefBroken:
			l.Warnf(diag.CodeRefBroken, loc, "cannot resolve %s: %s", ref.Raw, res.Reason)
		case resolve.RefMissingLocal:
			l.Errorf(diag.CodeRefLocal, loc, "%s does not exist in this document", ref.Raw)
		case resolve.RefOutside:
			l.Errorf(diag.CodeRefOutside, loc, "%s points outside the project root", ref.Raw)
		}
		for _, d := range l {
			w.add(d)
		}
		return res.Text(ref)
	})
}

// Dir builds the view of dir. With inherit, a directory without its own DSD is described by the
// node of an ancestor tree (design §10.3).
func (r *Renderer) Dir(dir string, inherit bool) (*DirView, error) {
	p := r.P
	listing, err := resolve.List(dir)
	if err != nil {
		return nil, err
	}
	d, err := p.DSD(dir)
	if err != nil {
		return nil, err
	}
	var inh *resolve.Inherited
	if d == nil && inherit {
		inh = p.Inherited(dir)
	}
	if d == nil && len(listing.PFDs) == 0 && inh == nil {
		return nil, ErrNotDocumented
	}
	v := &DirView{
		DirdocView: ViewVersion, Kind: "directory", Path: p.Rel(dir, true),
		DSD: []DSDEntryView{}, Files: []FileSummary{}, Subdirs: []SubdirView{},
	}
	w := &warnings{}
	owner := resolve.DirOwner(dir)
	switch {
	case d != nil:
		src := p.Rel(resolve.DSDFile(dir), false)
		v.Source = &src
		v.Summary = r.text(d.Summary, owner, v.Path, 0, w)
		if d.Tree != nil {
			v.Tree = r.treeView(dir, d.Tree, "")
			v.treeText = r.treeText(dir, d.Tree)
		}
		for _, e := range d.Entries {
			v.DSD = append(v.DSD, r.dsdEntry(dir, d, e, 4, w))
		}
		val := diag.Validator{P: p, FS: true}
		for _, dg := range val.DSD(dir, d) {
			if displayCodes[dg.Code] {
				w.add(dg)
			}
		}
	case inh != nil:
		v.InheritedFrom = &InheritedView{Source: p.Rel(resolve.DSDFile(inh.Dir), false), Note: inh.Node.Note}
		v.Summary = inh.Node.Note
		if e := inh.Entry(); e != nil {
			v.InheritedFrom.Ref = p.EntryRef(resolve.DirOwner(inh.Dir), e.ID)
			v.DSD = append(v.DSD, r.dsdEntry(inh.Dir, inh.DSD, *e, 4, w))
		}
	}
	for _, name := range listing.PFDs {
		file := filepath.Join(dir, name)
		pf, err := p.PFD(file)
		if err != nil {
			w.add(diag.LoadError(p.Rel(resolve.PFDFile(file), false), err))
			continue
		}
		v.Files = append(v.Files, r.fileSummary(file, pf, w))
		val := diag.Validator{P: p, FS: true}
		for _, dg := range val.PFD(file, pf) {
			if displayCodes[dg.Code] {
				w.add(dg)
			}
		}
	}
	if entries, err := p.Ignore.List(dir); err == nil {
		for _, e := range entries {
			child := filepath.Join(dir, e.Name)
			if !e.IsDir || !resolve.HasDirdoc(child) {
				continue
			}
			sd := SubdirView{Path: p.Rel(child, true)}
			if cd, err := p.DSD(child); err == nil && cd != nil {
				sd.Summary = firstLine(cd.Summary)
			}
			v.Subdirs = append(v.Subdirs, sd)
		}
	}
	v.Warnings = w.list()
	return v, nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (r *Renderer) marks(dir string, n *model.TreeNode, rel string) (hasDirdoc, hasPFD bool) {
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if n.IsDir() {
		return rel != "" && resolve.HasDirdoc(abs), false
	}
	return false, exists(resolve.PFDFile(abs))
}

func (r *Renderer) treeView(dir string, n *model.TreeNode, rel string) *TreeNodeView {
	tv := &TreeNodeView{Name: n.Name, Type: n.Type, DSD: n.DSD, Note: n.Note, More: n.More}
	if n.DSD != "" {
		tv.Ref = r.P.EntryRef(resolve.DirOwner(dir), n.DSD)
	}
	tv.HasDirdoc, tv.HasPFD = r.marks(dir, n, rel)
	for _, c := range n.Children {
		tv.Children = append(tv.Children, r.treeView(dir, c, path.Join(rel, c.Name)))
	}
	return tv
}

func (r *Renderer) treeText(dir string, root *model.TreeNode) string {
	return source.FormatTree(root, func(n *model.TreeNode, rel string) string {
		var parts []string
		if n.DSD != "" {
			parts = append(parts, "[["+r.P.EntryRef(resolve.DirOwner(dir), n.DSD)+"]]")
		}
		hasDirdoc, hasPFD := r.marks(dir, n, rel)
		if hasDirdoc {
			parts = append(parts, "(dirdoc)")
		}
		if hasPFD {
			parts = append(parts, "(pfd)")
		}
		return strings.Join(parts, " ")
	})
}

func (r *Renderer) dsdEntry(dir string, d *model.DSD, e model.DSDEntry, top int, w *warnings) DSDEntryView {
	owner := resolve.DirOwner(dir)
	ev := DSDEntryView{ID: e.ID, Ref: r.P.EntryRef(owner, e.ID), Name: d.DisplayName(e.ID, filepath.Base(dir)), Paths: []string{}}
	if d.Tree != nil {
		d.Tree.Walk(func(n *model.TreeNode, rel string) {
			if n.DSD == e.ID {
				ev.Paths = append(ev.Paths, r.P.Rel(filepath.Join(dir, filepath.FromSlash(rel)), n.IsDir()))
			}
		})
	}
	if r.Level >= Normal {
		ev.Document = r.text(e.Document, owner, ev.Ref, top, w)
	}
	return ev
}

func (r *Renderer) overview(owner resolve.Owner, pf *model.PFD, w *warnings) OverviewView {
	ov := pf.Overview
	if ov.Empty() && ov.Undocumented == nil {
		ov = model.BuildOverview(pf)
	}
	items := func(in []model.OverviewItem) []OverviewItem {
		out := make([]OverviewItem, 0, len(in))
		for _, it := range in {
			ref := r.P.EntryRef(owner, it.ID)
			out = append(out, OverviewItem{
				ID: it.ID, Ref: ref, Name: it.Name, Summary: r.text(it.Summary, owner, ref, 0, w),
				Line: it.Line, Signature: it.Signature,
			})
		}
		return out
	}
	v := OverviewView{Types: items(ov.Types), Functions: items(ov.Functions), Variables: items(ov.Variables)}
	if ov.Undocumented != nil {
		undoc := append([]model.Symbol{}, ov.Undocumented...)
		v.Undocumented = &undoc
	}
	return v
}

// analysis returns the line and signature the overview of pf records for entry id.
func analysis(pf *model.PFD, id string) (int, string) {
	for _, sec := range model.PFDSections {
		for _, it := range pf.Overview.Section(sec) {
			if it.ID == id {
				return it.Line, it.Signature
			}
		}
	}
	return 0, ""
}

func (r *Renderer) entries(owner resolve.Owner, pf *model.PFD, sec string, top int, w *warnings) *[]EntryBody {
	out := []EntryBody{}
	for _, e := range pf.Section(sec) {
		ref := r.P.EntryRef(owner, e.ID)
		line, sig := analysis(pf, e.ID)
		out = append(out, EntryBody{
			ID: e.ID, Ref: ref, Name: e.Name, Line: line, Signature: sig,
			Summary:  r.text(e.Summary, owner, ref, 0, w),
			Document: r.text(e.Document, owner, ref, top, w),
		})
	}
	return &out
}

func (r *Renderer) fileSummary(file string, pf *model.PFD, w *warnings) FileSummary {
	owner := resolve.FileOwner(file)
	fs := FileSummary{
		Path: r.P.Rel(file, false), Ref: r.P.Ref(owner), Source: r.P.Rel(resolve.PFDFile(file), false),
		Stale: r.P.Stale(file, pf),
	}
	fs.Summary = r.text(pf.Summary, owner, fs.Ref, 0, w)
	if r.Level >= Normal {
		ov := r.overview(owner, pf, w)
		fs.Overview = &ov
	}
	if r.Level >= Full {
		fs.Types = r.entries(owner, pf, model.SectionTypes, 5, w)
		fs.Functions = r.entries(owner, pf, model.SectionFunctions, 5, w)
		fs.Variables = r.entries(owner, pf, model.SectionVariables, 5, w)
	}
	return fs
}

// File builds the view of file.
func (r *Renderer) File(file string) (*FileView, error) {
	pf, err := r.P.PFD(file)
	if err != nil {
		return nil, err
	}
	if pf == nil {
		return nil, ErrNotDocumented
	}
	owner := resolve.FileOwner(file)
	w := &warnings{}
	v := &FileView{
		DirdocView: ViewVersion, Kind: "file", Path: r.P.Rel(file, false), Ref: r.P.Ref(owner),
		Source: r.P.Rel(resolve.PFDFile(file), false), Stale: r.P.Stale(file, pf),
		language: analyze.Language(file),
	}
	v.Summary = r.text(pf.Summary, owner, v.Ref, 0, w)
	v.Overview = r.overview(owner, pf, w)
	if r.Level >= Normal {
		v.Types = r.entries(owner, pf, model.SectionTypes, 4, w)
		v.Functions = r.entries(owner, pf, model.SectionFunctions, 4, w)
		v.Variables = r.entries(owner, pf, model.SectionVariables, 4, w)
		refs := []BacklinkView{}
		for _, b := range r.P.Backlinks(file) {
			if b.From == owner {
				continue // the file's own document
			}
			bv := r.backlink(b)
			bv.Target = r.P.Ref(owner)
			if b.ID != "" {
				bv.Target += "#" + b.ID
			}
			refs = append(refs, bv)
		}
		v.ReferencedBy = &refs
	}
	val := diag.Validator{P: r.P, FS: true}
	for _, dg := range val.PFD(file, pf) {
		if displayCodes[dg.Code] {
			w.add(dg)
		}
	}
	v.Warnings = w.list()
	return v, nil
}

// Entry builds the view of one entry: a DSD entry when isDir, else a type, function or variable.
func (r *Renderer) Entry(abs string, isDir bool, id string) (*EntryView, error) {
	w := &warnings{}
	if isDir {
		d, err := r.P.DSD(abs)
		if err != nil {
			return nil, err
		}
		if d == nil {
			return nil, ErrNotDocumented
		}
		e := d.Entry(id)
		if e == nil {
			return nil, fmt.Errorf("%w: %s has no dsd entry %q", ErrNotDocumented, r.P.Rel(abs, true), id)
		}
		owner := resolve.DirOwner(abs)
		dv := r.dsdEntry(abs, d, *e, 2, w)
		v := &EntryView{
			DirdocView: ViewVersion, Kind: "dsd-entry", Ref: dv.Ref, Path: r.P.Rel(abs, true),
			Source: r.P.Rel(resolve.DSDFile(abs), false), ID: id, Name: dv.Name,
			Document: r.text(e.Document, owner, dv.Ref, 2, w), Paths: dv.Paths,
			Refs: r.refViews(owner, e.Document), ReferencedBy: r.dsdBacklinks(abs, d, id),
		}
		v.Warnings = w.list()
		return v, nil
	}
	pf, err := r.P.PFD(abs)
	if err != nil {
		return nil, err
	}
	if pf == nil {
		return nil, ErrNotDocumented
	}
	sec, e := pf.Find(id)
	if e == nil {
		return nil, fmt.Errorf("%w: %s has no entry %q", ErrNotDocumented, r.P.Rel(abs, false), id)
	}
	owner := resolve.FileOwner(abs)
	ref := r.P.EntryRef(owner, id)
	v := &EntryView{
		DirdocView: ViewVersion, Kind: model.SectionKind(sec), Ref: ref, Path: r.P.Rel(abs, false),
		Source: r.P.Rel(resolve.PFDFile(abs), false), ID: id, Name: e.Name, Stale: r.P.Stale(abs, pf),
		Summary:  r.text(e.Summary, owner, ref, 0, w),
		Document: r.text(e.Document, owner, ref, 2, w),
		Refs:     r.refViews(owner, e.Summary, e.Document), ReferencedBy: []BacklinkView{},
		language: analyze.Language(abs),
	}
	v.Line, v.Signature = analysis(pf, id)
	for _, b := range r.P.Backlinks(abs) {
		if b.ID == id && !(b.From == owner && b.FromID == id) {
			v.ReferencedBy = append(v.ReferencedBy, r.backlink(b))
		}
	}
	v.Warnings = w.list()
	return v, nil
}

// dsdBacklinks returns the places that refer to the dsd entry id of dir: as dir/#id, or as a
// directory of the tree that has no DSD of its own and so is described by the entry (design §10.3).
func (r *Renderer) dsdBacklinks(dir string, d *model.DSD, id string) []BacklinkView {
	out := []BacklinkView{}
	owner := resolve.DirOwner(dir)
	for _, b := range r.P.Backlinks(dir) {
		if b.ID == id && !(b.From == owner && b.FromID == id) {
			out = append(out, r.backlink(b))
		}
	}
	for _, rel := range d.Paths(id) {
		if rel == "" {
			continue
		}
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if own, err := r.P.DSD(abs); err != nil || own != nil {
			continue
		}
		if inh := r.P.Inherited(abs); inh == nil || inh.Dir != filepath.Clean(dir) || inh.Node.DSD != id {
			continue
		}
		for _, b := range r.P.Backlinks(abs) {
			if b.ID == "" && !(b.From == owner && b.FromID == id) {
				out = append(out, r.backlink(b))
			}
		}
	}
	return out
}

// backlink describes the place a reference was found in.
func (r *Renderer) backlink(b resolve.Backlink) BacklinkView {
	isDir := b.From.Kind == model.KindDSD
	bv := BacklinkView{Ref: r.P.Ref(b.From), Kind: "file", Name: r.P.Rel(b.From.Path, isDir)}
	if isDir {
		bv.Kind = "directory"
	}
	if b.FromID != "" {
		bv.Ref += "#" + b.FromID
	}
	if isDir {
		d, _ := r.P.DSD(b.From.Path)
		switch {
		case d == nil:
		case b.FromID == "":
			bv.Summary = firstLine(d.Summary)
		default:
			bv.Kind, bv.Name = "dsd-entry", d.DisplayName(b.FromID, filepath.Base(b.From.Path))
		}
		return bv
	}
	pf, _ := r.P.PFD(b.From.Path)
	switch {
	case pf == nil:
	case b.FromID == "":
		bv.Summary = firstLine(pf.Summary)
	default:
		if sec, e := pf.Find(b.FromID); e != nil {
			bv.Kind, bv.Name, bv.Summary = model.SectionKind(sec), e.Name, firstLine(e.Summary)
		}
	}
	return bv
}

func (r *Renderer) refViews(owner resolve.Owner, texts ...string) []RefView {
	out := []RefView{}
	seen := map[string]bool{}
	for _, t := range texts {
		for _, ref := range resolve.FindRefs(t) {
			res := r.P.Resolve(owner, ref)
			if seen[res.Ref] {
				continue
			}
			seen[res.Ref] = true
			rv := RefView{Ref: res.Ref, Resolved: res.Status == resolve.RefOK}
			if rv.Resolved {
				rv.Kind, rv.Name, rv.Summary = res.Kind, res.Name, firstLine(res.Summary)
			} else {
				rv.Reason = res.Reason
			}
			out = append(out, rv)
		}
	}
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

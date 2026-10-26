// Package model defines the documents dirdoc stores (DSD and PFD) and the rules for entry IDs.
package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// Format identifiers and the schema version written to every stored document.
const (
	FormatDSD = "dirdoc/dsd"
	FormatPFD = "dirdoc/pfd"
	Version   = 1
)

// File names used inside a .dirdoc directory.
const (
	DirName   = ".dirdoc"
	DSDName   = ".dirdoc_dsd"
	PFDSuffix = ".dirdoc_pfd"
)

// Kind tells a directory document (DSD) from a file document (PFD).
type Kind int

const (
	KindDSD Kind = iota + 1
	KindPFD
)

func (k Kind) String() string {
	switch k {
	case KindDSD:
		return "dsd"
	case KindPFD:
		return "pfd"
	}
	return "unknown"
}

// Section names of the entry arrays.
const (
	SectionDSD       = "dsd"
	SectionTypes     = "types"
	SectionFunctions = "functions"
	SectionVariables = "variables"
)

// PFDSections lists the PFD entry sections in display order.
var PFDSections = []string{SectionTypes, SectionFunctions, SectionVariables}

// SectionKind returns the singular kind name of a PFD section ("function" for "functions").
func SectionKind(section string) string {
	switch section {
	case SectionTypes:
		return "type"
	case SectionFunctions:
		return "function"
	case SectionVariables:
		return "variable"
	case SectionDSD:
		return "dsd-entry"
	}
	return section
}

// Tree node types.
const (
	NodeDir  = "dir"
	NodeFile = "file"
)

// Extra holds fields this version does not know about, including "x-" extensions.
// They are kept as raw JSON so that rewriting a document never drops them.
type Extra map[string]json.RawMessage

// TreeNode is one line of a DSD tree. The root node is the owner directory itself and is named ".".
type TreeNode struct {
	Name     string
	Type     string
	DSD      string
	Note     string
	More     bool
	Children []*TreeNode
	Extra    Extra
}

// IsDir reports whether the node is a directory.
func (n *TreeNode) IsDir() bool { return n.Type == NodeDir }

// Child returns the direct child with the given name, or nil.
func (n *TreeNode) Child(name string) *TreeNode {
	for _, c := range n.Children {
		if c != nil && c.Name == name {
			return c
		}
	}
	return nil
}

// Find returns the node at rel, a slash-separated path relative to n, or nil. An empty rel returns n.
func (n *TreeNode) Find(rel string) *TreeNode {
	cur := n
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." {
			continue
		}
		cur = cur.Child(seg)
		if cur == nil {
			return nil
		}
	}
	return cur
}

// Walk visits n and its descendants in order. rel is the slash path relative to n ("" for n itself).
func (n *TreeNode) Walk(fn func(node *TreeNode, rel string)) {
	var visit func(node *TreeNode, rel string)
	visit = func(node *TreeNode, rel string) {
		fn(node, rel)
		for _, c := range node.Children {
			if c == nil {
				continue
			}
			child := c.Name
			if rel != "" {
				child = rel + "/" + c.Name
			}
			visit(c, child)
		}
	}
	visit(n, "")
}

// DSDEntry documents one directory of a DSD tree.
type DSDEntry struct {
	ID       string
	Name     string
	Document string
	Extra    Extra
}

// DSD is a Directory Structure Document (.dirdoc/.dirdoc_dsd).
type DSD struct {
	Summary string
	Tree    *TreeNode
	Entries []DSDEntry
	Extra   Extra
}

// Entry returns the entry with the given id, or nil.
func (d *DSD) Entry(id string) *DSDEntry {
	for i := range d.Entries {
		if d.Entries[i].ID == id {
			return &d.Entries[i]
		}
	}
	return nil
}

// Paths returns the tree paths (relative to the owner directory, "" for the root) that link to id.
func (d *DSD) Paths(id string) []string {
	var out []string
	if d.Tree == nil || id == "" {
		return out
	}
	d.Tree.Walk(func(n *TreeNode, rel string) {
		if n.DSD == id {
			out = append(out, rel)
		}
	})
	return out
}

// Texts calls fn with every Markdown text of d that may hold references: the summary (id "") and
// the document of each entry.
func (d *DSD) Texts(fn func(id string, text *string)) {
	fn("", &d.Summary)
	for i := range d.Entries {
		fn(d.Entries[i].ID, &d.Entries[i].Document)
	}
}

// DisplayName returns the name shown for entry id: its own name, else the name of the tree node
// that links to it (dirName for the root), else the id itself.
func (d *DSD) DisplayName(id, dirName string) string {
	if e := d.Entry(id); e != nil && e.Name != "" {
		return e.Name
	}
	names := map[string]bool{}
	if d.Tree != nil {
		d.Tree.Walk(func(n *TreeNode, rel string) {
			if n.DSD != id {
				return
			}
			if rel == "" {
				names[dirName] = true
			} else {
				names[n.Name] = true
			}
		})
	}
	if len(names) == 1 {
		for name := range names {
			return name
		}
	}
	return id
}

// Entry is one type, function or variable of a PFD. Variables never have a Document.
type Entry struct {
	ID       string
	Name     string
	Summary  string
	Document string
	Extra    Extra
}

// OverviewItem is one line of the generated table of contents. Line and Signature come from code
// analysis and are empty when the file was not analyzed or has no symbol with the item's id.
type OverviewItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	Line      int    `json:"line,omitempty"`
	Signature string `json:"signature,omitempty"`
}

// Symbol is a top-level declaration found by code analysis.
type Symbol struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"` // type, function or variable
	Line      int    `json:"line"`
	Signature string `json:"signature"`
}

// Overview is the table of contents compile generates for a PFD. Undocumented lists the symbols of
// the file that no entry documents; it is nil when the file was not analyzed.
type Overview struct {
	Types        []OverviewItem
	Functions    []OverviewItem
	Variables    []OverviewItem
	Undocumented []Symbol
}

// Section returns the overview items of a PFD section.
func (o Overview) Section(name string) []OverviewItem {
	switch name {
	case SectionTypes:
		return o.Types
	case SectionFunctions:
		return o.Functions
	case SectionVariables:
		return o.Variables
	}
	return nil
}

// Empty reports whether the overview lists nothing.
func (o Overview) Empty() bool {
	return len(o.Types) == 0 && len(o.Functions) == 0 && len(o.Variables) == 0
}

// PFD is a Program File Document (.dirdoc/<file>.dirdoc_pfd).
type PFD struct {
	Target     string
	TargetHash string
	Summary    string
	Overview   Overview
	Types      []Entry
	Functions  []Entry
	Variables  []Entry
	Extra      Extra
}

// Section returns the entries of the named section.
func (p *PFD) Section(name string) []Entry {
	switch name {
	case SectionTypes:
		return p.Types
	case SectionFunctions:
		return p.Functions
	case SectionVariables:
		return p.Variables
	}
	return nil
}

// SetSection replaces the entries of the named section.
func (p *PFD) SetSection(name string, entries []Entry) {
	switch name {
	case SectionTypes:
		p.Types = entries
	case SectionFunctions:
		p.Functions = entries
	case SectionVariables:
		p.Variables = entries
	}
}

// Texts calls fn with every Markdown text of p that may hold references: the summary (id "") and
// the summary and document of each entry. The overview copies the summaries; rebuild it after
// changing them.
func (p *PFD) Texts(fn func(id string, text *string)) {
	fn("", &p.Summary)
	for _, sec := range PFDSections {
		entries := p.Section(sec)
		for i := range entries {
			fn(entries[i].ID, &entries[i].Summary)
			fn(entries[i].ID, &entries[i].Document)
		}
	}
}

// Find returns the section and entry with the given id, or ("", nil).
func (p *PFD) Find(id string) (string, *Entry) {
	for _, sec := range PFDSections {
		entries := p.Section(sec)
		for i := range entries {
			if entries[i].ID == id {
				return sec, &entries[i]
			}
		}
	}
	return "", nil
}

// BuildOverview lists every entry of p in order, as compile stores it.
func BuildOverview(p *PFD) Overview {
	items := func(entries []Entry) []OverviewItem {
		out := make([]OverviewItem, 0, len(entries))
		for _, e := range entries {
			out = append(out, OverviewItem{ID: e.ID, Name: e.Name, Summary: e.Summary})
		}
		return out
	}
	return Overview{Types: items(p.Types), Functions: items(p.Functions), Variables: items(p.Variables)}
}

// AnnotateOverview adds code analysis to ov: the line and signature of every item whose id names a
// symbol, and the symbols no item names, in line order. A nil syms (not analyzed) changes nothing.
func AnnotateOverview(ov *Overview, syms []Symbol) {
	if syms == nil {
		return
	}
	byID := map[string]Symbol{}
	for _, s := range syms {
		if _, dup := byID[s.ID]; !dup {
			byID[s.ID] = s
		}
	}
	named := map[string]bool{}
	for _, items := range [][]OverviewItem{ov.Types, ov.Functions, ov.Variables} {
		for i := range items {
			if s, ok := byID[items[i].ID]; ok {
				items[i].Line, items[i].Signature = s.Line, s.Signature
				named[s.ID] = true
			}
		}
	}
	undoc := []Symbol{}
	for _, s := range syms {
		if !named[s.ID] {
			undoc = append(undoc, s)
		}
	}
	sort.SliceStable(undoc, func(i, j int) bool { return undoc[i].Line < undoc[j].Line })
	ov.Undocumented = undoc
}

// OverviewSymbols returns the code analysis an overview holds, or nil when the file was not analyzed.
// A documented symbol takes the kind of the section its entry is in.
func OverviewSymbols(ov Overview) []Symbol {
	if ov.Undocumented == nil {
		return nil
	}
	syms := append([]Symbol{}, ov.Undocumented...)
	for _, sec := range PFDSections {
		for _, it := range ov.Section(sec) {
			if it.Line > 0 {
				syms = append(syms, Symbol{ID: it.ID, Kind: SectionKind(sec), Line: it.Line, Signature: it.Signature})
			}
		}
	}
	return syms
}

// Reindex rebuilds p's overview from its entries and keeps the code analysis it held. It serves
// changes made without the documented file at hand (dirdoc rm, the merge driver).
func Reindex(p *PFD) {
	syms := OverviewSymbols(p.Overview)
	p.Overview = BuildOverview(p)
	AnnotateOverview(&p.Overview, syms)
}

// Hash returns the target_hash of a documented file: "sha256:" and the hex digest of its content with
// CRLF and CR line endings converted to LF, so that checkouts with other line endings match.
func Hash(content []byte) string {
	if bytes.IndexByte(content, '\r') >= 0 {
		content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
		content = bytes.ReplaceAll(content, []byte("\r"), []byte("\n"))
	}
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

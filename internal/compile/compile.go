// Package compile turns a parsed source into the document to store: it merges with the existing
// document when asked, normalizes text and generates the overview. Validation is diag.Validator's job.
package compile

import (
	"strings"

	"dirdoc/internal/diag"
	"dirdoc/internal/model"
	"dirdoc/internal/source"
)

// Newlines converts CRLF and CR line endings to LF.
func Newlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// Summary normalizes a summary: LF line endings, no surrounding blank space.
func Summary(s string) string { return strings.TrimSpace(Newlines(s)) }

// Name normalizes a name or an id.
func Name(s string) string { return strings.TrimSpace(Newlines(s)) }

// Document normalizes a Markdown body: LF line endings, no leading blank lines and no trailing blank
// space. Indentation of the first line is kept (it may start an indented code block).
func Document(s string) string {
	s = Newlines(s)
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 || strings.TrimSpace(s[:i]) != "" {
			break
		}
		s = s[i+1:]
	}
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return strings.TrimRight(s, " \t\n")
}

// Locator maps the compiled document back to lines of the source; it implements diag.Locator.
type Locator struct {
	top      map[string]int
	treeLine int
	nodes    map[*model.TreeNode]int
	entries  map[string]map[int]*source.Entry
}

func newLocator(src *source.Doc) *Locator {
	return &Locator{top: src.TopLines, treeLine: src.TreeLine, nodes: src.NodeLines, entries: map[string]map[int]*source.Entry{}}
}

func (l *Locator) TopLine(key string) int { return l.top[key] }

// EntryLine returns the line of key in the source table that produced the entry. It returns 0 when
// the source did not set key, because the value then comes from the stored document (--merge).
func (l *Locator) EntryLine(section string, index int, key string) int {
	se := l.entries[section][index]
	if se == nil {
		return 0
	}
	present := map[string]bool{"id": se.ID != nil, "name": se.Name != nil, "summary": se.Summary != nil, "document": se.Document != nil}
	if key == "id" && se.ID == nil && se.Name != nil {
		key = "name" // the id was derived from the name
	}
	if set, known := present[key]; known && !set {
		return 0
	}
	if ln := se.KeyLines[key]; ln > 0 {
		return ln
	}
	return se.Line
}

func (l *Locator) NodeLine(n *model.TreeNode) int {
	ln, ok := l.nodes[n]
	if !ok || ln == 0 || l.treeLine == 0 {
		return 0
	}
	return l.treeLine + ln - 1
}

func fail(l *diag.List, src *source.Doc, line int, format string, args ...any) {
	d := l.Errorf(diag.CodeMerge, "", format, args...)
	d.File, d.Line = src.File, line
}

func mergeExtra(merge bool, base, src model.Extra) model.Extra {
	if !merge {
		return src
	}
	out := model.Extra{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range src {
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func lineOf(se *source.Entry, key string) int {
	if ln := se.KeyLines[key]; ln > 0 {
		return ln
	}
	return se.Line
}

// DSD builds the DSD to store from src. With merge, base is the stored DSD (or nil) and src only
// carries the changes: given keys replace, entries are matched by id, delete = true removes one.
func DSD(src *source.Doc, base *model.DSD, merge bool) (*model.DSD, *Locator, diag.List) {
	var l diag.List
	loc := newLocator(src)
	out := &model.DSD{}
	if merge && base != nil {
		if c, err := model.CloneDSD(base); err == nil {
			out = c
		}
	}
	if src.Summary != nil {
		out.Summary = Summary(*src.Summary)
	}
	if src.HasTree {
		out.Tree = src.Tree
	}
	out.Extra = mergeExtra(merge, out.Extra, src.Extra)

	origin := map[int]*source.Entry{}
	newEntry := func(se *source.Entry) model.DSDEntry {
		return model.DSDEntry{ID: Name(source.Str(se.ID)), Name: Name(source.Str(se.Name)), Document: Document(source.Str(se.Document)), Extra: se.Extra}
	}
	if !merge {
		out.Entries = nil
		for _, se := range src.Sections[model.SectionDSD] {
			if se.Delete {
				fail(&l, src, lineOf(se, "delete"), "delete is only allowed with --merge")
				continue
			}
			origin[len(out.Entries)] = se
			out.Entries = append(out.Entries, newEntry(se))
		}
	} else {
		touched := map[string]*source.Entry{}
		for _, se := range src.Sections[model.SectionDSD] {
			id := Name(source.Str(se.ID))
			if id == "" {
				fail(&l, src, se.Line, "dsd entries are matched by id; set id")
				continue
			}
			idx := -1
			for i := range out.Entries {
				if out.Entries[i].ID == id {
					idx = i
				}
			}
			switch {
			case se.Delete && idx < 0:
				fail(&l, src, lineOf(se, "delete"), "no dsd entry with id %q to delete", id)
			case se.Delete:
				out.Entries = append(out.Entries[:idx], out.Entries[idx+1:]...)
			case idx >= 0:
				e := &out.Entries[idx]
				if se.Name != nil {
					e.Name = Name(*se.Name)
				}
				if se.Document != nil {
					e.Document = Document(*se.Document)
				}
				e.Extra = mergeExtra(true, e.Extra, se.Extra)
				touched[id] = se
			default:
				out.Entries = append(out.Entries, newEntry(se))
				touched[id] = se
			}
		}
		for i, e := range out.Entries {
			if se := touched[e.ID]; se != nil {
				origin[i] = se
			}
		}
	}
	loc.entries[model.SectionDSD] = origin
	return out, loc, l
}

// PFD builds the PFD to store for the file named target. See DSD for how merge works; entries
// without an id are matched by the id derived from their name.
func PFD(src *source.Doc, base *model.PFD, target string, merge bool) (*model.PFD, *Locator, diag.List) {
	var l diag.List
	loc := newLocator(src)
	out := &model.PFD{}
	if merge && base != nil {
		if c, err := model.ClonePFD(base); err == nil {
			out = c
		}
	}
	out.Target = target
	out.TargetHash = "" // recorded from Phase 2 on (design §10.5)
	if src.Target != nil && *src.Target != target {
		d := l.Errorf(diag.CodeTargetMismatch, "", "target %q does not match the file name %q", *src.Target, target)
		d.File, d.Line = src.File, src.TopLines["target"]
	}
	if src.Summary != nil {
		out.Summary = Summary(*src.Summary)
	}
	out.Extra = mergeExtra(merge, out.Extra, src.Extra)
	for _, sec := range model.PFDSections {
		entries, origin := buildEntries(&l, src, sec, out.Section(sec), merge)
		out.SetSection(sec, entries)
		loc.entries[sec] = origin
	}
	out.Overview = model.BuildOverview(out)
	return out, loc, l
}

func entryID(se *source.Entry) string {
	if se.ID != nil {
		return Name(*se.ID)
	}
	if se.Name != nil {
		id, _ := model.DeriveID(Name(*se.Name))
		return id
	}
	return ""
}

func newEntry(se *source.Entry) model.Entry {
	return model.Entry{
		ID:       entryID(se),
		Name:     Name(source.Str(se.Name)),
		Summary:  Summary(source.Str(se.Summary)),
		Document: Document(source.Str(se.Document)),
		Extra:    se.Extra,
	}
}

func buildEntries(l *diag.List, src *source.Doc, sec string, existing []model.Entry, merge bool) ([]model.Entry, map[int]*source.Entry) {
	origin := map[int]*source.Entry{}
	if !merge {
		var out []model.Entry
		for _, se := range src.Sections[sec] {
			if se.Delete {
				fail(l, src, lineOf(se, "delete"), "delete is only allowed with --merge")
				continue
			}
			origin[len(out)] = se
			out = append(out, newEntry(se))
		}
		return out, origin
	}
	out := append([]model.Entry(nil), existing...)
	touched := map[string]*source.Entry{}
	for _, se := range src.Sections[sec] {
		id := entryID(se)
		if id == "" {
			if se.Name != nil {
				fail(l, src, lineOf(se, "name"), "cannot derive an id from name %q; set id to say which entry to update", *se.Name)
			} else {
				fail(l, src, se.Line, "set id (or name) to say which entry to update")
			}
			continue
		}
		idx := -1
		for i := range out {
			if out[i].ID == id {
				idx = i
			}
		}
		switch {
		case se.Delete && idx < 0:
			fail(l, src, lineOf(se, "delete"), "no %s entry with id %q to delete", model.SectionKind(sec), id)
		case se.Delete:
			out = append(out[:idx], out[idx+1:]...)
		case idx >= 0:
			e := &out[idx]
			if se.Name != nil {
				e.Name = Name(*se.Name)
			}
			if se.Summary != nil {
				e.Summary = Summary(*se.Summary)
			}
			if se.Document != nil {
				e.Document = Document(*se.Document)
			}
			e.Extra = mergeExtra(true, e.Extra, se.Extra)
			touched[id] = se
		default:
			out = append(out, newEntry(se))
			touched[id] = se
		}
	}
	for i, e := range out {
		if se := touched[e.ID]; se != nil {
			origin[i] = se
		}
	}
	return out, origin
}

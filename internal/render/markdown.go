package render

import (
	"bytes"
	"encoding/json"
	"path"
	"strings"

	"dirdoc/internal/diag"
	"dirdoc/internal/model"
)

// JSON encodes a view as indented JSON without HTML escaping.
func JSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type md struct{ blocks []string }

func (b *md) add(s string) {
	if s = strings.TrimRight(s, "\n"); s != "" {
		b.blocks = append(b.blocks, s)
	}
}

func (b *md) h(level int, text string) { b.add(strings.Repeat("#", level) + " " + text) }

func (b *md) quote(s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("> "+l, " ")
	}
	b.add(strings.Join(lines, "\n"))
}

func (b *md) list(items []string) {
	if len(items) == 0 {
		return
	}
	for i, it := range items {
		items[i] = "- " + it
	}
	b.add(strings.Join(items, "\n"))
}

func (b *md) fence(lang, s string) {
	f := "```"
	for strings.Contains(s, f) {
		f += "`"
	}
	b.add(f + lang + "\n" + s + "\n" + f)
}

func (b *md) String() string { return strings.Join(b.blocks, "\n\n") + "\n" }

// code formats s as inline code, choosing a fence longer than any backtick run inside it.
func code(s string) string {
	f := "`"
	for strings.Contains(s, f) {
		f += "`"
	}
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return f + " " + s + " " + f
	}
	return f + s + f
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func dash(s string) string {
	if s = oneLine(s); s != "" {
		return " — " + s
	}
	return ""
}

var kindLabels = map[string]string{
	model.SectionTypes: "type", model.SectionFunctions: "fn", model.SectionVariables: "var",
	"type": "type", "function": "fn", "variable": "var", "dsd-entry": "dsd", "directory": "dir", "file": "file",
}

func overviewLines(ov *OverviewView) []string {
	var out []string
	for _, sec := range model.PFDSections {
		var items []OverviewItem
		switch sec {
		case model.SectionTypes:
			items = ov.Types
		case model.SectionFunctions:
			items = ov.Functions
		case model.SectionVariables:
			items = ov.Variables
		}
		for _, it := range items {
			out = append(out, kindLabels[sec]+" "+code(it.Name)+" "+code("#"+it.ID)+dash(it.Summary))
		}
	}
	return out
}

func (b *md) warnings(l diag.List) {
	if len(l) == 0 {
		return
	}
	b.h(2, "Warnings")
	var items []string
	for _, d := range l {
		item := d.Code
		if d.Path != "" {
			item += " " + code(d.Path)
		}
		items = append(items, item+": "+d.Message)
	}
	b.list(items)
}

// DirMarkdown renders a directory view.
func (r *Renderer) DirMarkdown(v *DirView) string {
	var b md
	r.dir(&b, v)
	return b.String()
}

// ListMarkdown renders the views of `dirdoc -r` one after the other.
func (r *Renderer) ListMarkdown(l *ListView) string {
	var b md
	for _, v := range l.Views {
		r.dir(&b, v)
	}
	return b.String()
}

func (r *Renderer) dir(b *md, v *DirView) {
	b.h(1, v.Path)
	b.quote(v.Summary)
	if v.InheritedFrom != nil {
		line := "No DSD of its own; described in " + code(v.InheritedFrom.Source)
		if v.InheritedFrom.Ref != "" {
			line += " as [[" + v.InheritedFrom.Ref + "]]"
		}
		b.add(line + ".")
	}
	if v.Tree != nil {
		b.h(2, "Tree")
		b.fence("text", v.treeText)
	}
	if r.Level >= Normal && len(v.DSD) > 0 {
		b.h(2, "Directories")
		for _, e := range v.DSD {
			b.h(3, e.Name+" "+code(e.Ref))
			b.add(e.Document)
		}
	}
	if len(v.Files) > 0 {
		b.h(2, "Files")
		if r.Level == Brief {
			var items []string
			for _, f := range v.Files {
				items = append(items, code(f.Path)+dash(firstLine(f.Summary)))
			}
			b.list(items)
		}
		for _, f := range v.Files {
			if r.Level == Brief {
				break
			}
			b.h(3, path.Base(f.Path)+" "+code(f.Ref))
			b.quote(f.Summary)
			if f.Overview != nil {
				b.list(overviewLines(f.Overview))
			}
			for _, sec := range []struct {
				kind    string
				entries *[]EntryBody
			}{{"type", f.Types}, {"fn", f.Functions}} {
				if sec.entries == nil {
					continue
				}
				for _, e := range *sec.entries {
					if e.Document == "" {
						continue
					}
					b.h(4, sec.kind+" "+e.Name+" "+code("#"+e.ID))
					b.add(e.Document)
				}
			}
		}
	}
	if len(v.Subdirs) > 0 {
		b.h(2, "Subdirectories")
		var items []string
		for _, s := range v.Subdirs {
			items = append(items, code(s.Path)+dash(s.Summary))
		}
		b.list(items)
	}
	b.warnings(v.Warnings)
}

// FileMarkdown renders a file view.
func (r *Renderer) FileMarkdown(v *FileView) string {
	var b md
	b.h(1, v.Path)
	b.quote(v.Summary)
	if lines := overviewLines(&v.Overview); len(lines) > 0 {
		b.h(2, "Overview")
		b.list(lines)
	}
	for _, sec := range []struct {
		title, kind string
		entries     *[]EntryBody
	}{{"Types", "type", v.Types}, {"Functions", "fn", v.Functions}} {
		if sec.entries == nil || len(*sec.entries) == 0 {
			continue
		}
		b.h(2, sec.title)
		for _, e := range *sec.entries {
			b.h(3, e.Name+" "+code("#"+e.ID))
			b.quote(e.Summary)
			b.add(e.Document)
		}
	}
	if v.Variables != nil && len(*v.Variables) > 0 {
		b.h(2, "Variables")
		var items []string
		for _, e := range *v.Variables {
			items = append(items, code(e.Name)+" "+code("#"+e.ID)+dash(e.Summary))
		}
		b.list(items)
	}
	b.warnings(v.Warnings)
	return b.String()
}

// EntryMarkdown renders an entry view.
func (r *Renderer) EntryMarkdown(v *EntryView) string {
	var b md
	b.h(1, kindLabels[v.Kind]+" "+v.Name+" "+code(v.Ref))
	b.quote(v.Summary)
	if len(v.Paths) > 0 {
		var paths []string
		for _, p := range v.Paths {
			paths = append(paths, code(p))
		}
		b.add("Describes " + strings.Join(paths, ", ") + ".")
	}
	b.add(v.Document)
	if len(v.Refs) > 0 {
		b.h(2, "References")
		var items []string
		for _, rv := range v.Refs {
			item := "[[" + rv.Ref + "]]"
			if rv.Resolved {
				item += " — " + kindLabels[rv.Kind] + " " + rv.Name
				if rv.Summary != "" {
					item += ": " + oneLine(rv.Summary)
				}
			} else {
				item += " — unresolved (" + rv.Reason + ")"
			}
			items = append(items, item)
		}
		b.list(items)
	}
	b.warnings(v.Warnings)
	return b.String()
}

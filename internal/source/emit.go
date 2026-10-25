package source

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/SYZD-research/dirdoc/internal/model"
)

// EmitDSD prints d as a TOML source. header is written as a comment at the top.
func EmitDSD(d *model.DSD, header string) string {
	var w tomlWriter
	w.comment(header)
	w.kv("summary", d.Summary)
	if d.Tree != nil {
		w.blank()
		w.raw("tree = " + multiline(FormatTree(d.Tree, LinkAnnotator)))
	}
	w.extra(d.Extra)
	for _, e := range d.Entries {
		w.blank()
		w.raw("[[dsd]]")
		w.kv("id", e.ID)
		if e.Name != "" {
			w.kv("name", e.Name)
		}
		if e.Document != "" {
			w.raw("document = " + multiline(e.Document))
		}
		w.extra(e.Extra)
	}
	return w.String()
}

// EmitPFD prints p as a TOML source. The overview and target_hash are left out: compile generates them.
func EmitPFD(p *model.PFD, header string) string {
	var w tomlWriter
	w.comment(header)
	w.kv("summary", p.Summary)
	w.extra(p.Extra)
	for _, sec := range model.PFDSections {
		for _, e := range p.Section(sec) {
			w.blank()
			w.raw("[[" + sec + "]]")
			w.kv("id", e.ID)
			w.kv("name", e.Name)
			w.kv("summary", e.Summary)
			if e.Document != "" {
				w.raw("document = " + multiline(e.Document))
			}
			w.extra(e.Extra)
		}
	}
	return w.String()
}

// EmitJSON prints a document as an indented JSON source.
func EmitJSON(d *model.DSD, p *model.PFD) (string, error) {
	var b []byte
	var err error
	if d != nil {
		b, err = model.Marshal(d)
	} else {
		b, err = p.MarshalSource()
	}
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		return "", err
	}
	return buf.String() + "\n", nil
}

// ScaffoldDSD is the template `dirdoc export` prints for a directory that has no DSD yet.
func ScaffoldDSD(header, compileCmd string, tree *model.TreeNode) string {
	var w tomlWriter
	w.comment(header)
	w.comment("Fill in the summary and the entries, then run:\n  " + compileCmd)
	w.blank()
	w.kv("summary", "")
	w.blank()
	w.raw("tree = " + multiline(FormatTree(tree, LinkAnnotator)))
	w.blank()
	w.comment(`One [[dsd]] table per directory you describe; link it from the tree with [[#id]].
[[dsd]]
id = "cli"
name = "CLI"     # optional; defaults to the name of the tree node that links to it
document = '''
Markdown body. Link to other entries with [[#id]], [[file#id]] or [[dir/]].
'''`)
	return w.String()
}

// ScaffoldPFD is the template `dirdoc export` prints for a file that has no PFD yet.
func ScaffoldPFD(header, compileCmd string) string {
	var w tomlWriter
	w.comment(header)
	w.comment("Fill in the summary and the entries, then run:\n  " + compileCmd)
	w.blank()
	w.kv("summary", "")
	w.blank()
	w.comment(`[[types]]
name = "Config"
summary = "One-line description"
document = '''
Markdown body (optional)
'''

[[functions]]
name = "check_stdout()"   # the id defaults to the part before "(" (set id = "..." to override)
summary = "One-line description"
document = '''
Markdown body (optional). Link with [[#id]], [[other.rs#id]] or [[dir/]].
'''

[[variables]]
name = "MAX_AGENTS"
summary = "What it is for"`)
	return w.String()
}

type tomlWriter struct{ b strings.Builder }

func (w *tomlWriter) String() string { return w.b.String() }

func (w *tomlWriter) raw(s string) {
	w.b.WriteString(s)
	w.b.WriteByte('\n')
}

func (w *tomlWriter) blank() { w.b.WriteByte('\n') }

func (w *tomlWriter) comment(text string) {
	if text == "" {
		return
	}
	for _, line := range strings.Split(text, "\n") {
		w.raw(strings.TrimRight("# "+line, " "))
	}
}

func (w *tomlWriter) kv(key, value string) {
	if strings.Contains(value, "\n") {
		w.raw(key + " = " + multiline(value))
		return
	}
	w.raw(key + " = " + basicString(value))
}

// extra prints "x-" fields. Other unknown fields cannot be compiled back, so they are only noted.
func (w *tomlWriter) extra(e model.Extra) {
	keys := make([]string, 0, len(e))
	for k := range e {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !strings.HasPrefix(k, "x-") {
			w.comment(fmt.Sprintf("dirdoc: field %q is not shown (unknown to this version)", k))
			continue
		}
		v, ok := tomlValue(e[k])
		if !ok {
			w.comment(fmt.Sprintf("dirdoc: field %q has no TOML form (null)", k))
			continue
		}
		w.raw(tomlKey(k) + " = " + v)
	}
}

var bareKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func tomlKey(k string) string {
	if bareKeyRe.MatchString(k) {
		return k
	}
	return basicString(k)
}

func tomlValue(raw json.RawMessage) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", false
	}
	return tomlInline(v)
}

func tomlInline(v any) (string, bool) {
	switch x := v.(type) {
	case bool:
		return strconv.FormatBool(x), true
	case json.Number:
		return x.String(), true
	case string:
		return basicString(x), true
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			s, ok := tomlInline(item)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		return "[" + strings.Join(parts, ", ") + "]", true
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			s, ok := tomlInline(x[k])
			if !ok {
				return "", false
			}
			parts = append(parts, tomlKey(k)+" = "+s)
		}
		if len(parts) == 0 {
			return "{}", true
		}
		return "{ " + strings.Join(parts, ", ") + " }", true
	}
	return "", false
}

func basicString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func hasControl(s string) bool {
	for _, r := range s {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return true
		}
	}
	return false
}

// multiline prints s as a multi-line string: literal ”' when possible (no escaping needed),
// otherwise basic """ with escapes. The newline after the opening quotes is dropped by TOML.
func multiline(s string) string {
	if !strings.Contains(s, "'''") && !hasControl(s) {
		return "'''\n" + s + "\n'''"
	}
	var b strings.Builder
	b.WriteString("\"\"\"\n")
	quotes := 0
	for _, r := range s {
		if r == '"' {
			quotes++
			if quotes == 3 {
				b.WriteString(`\"`)
				quotes = 0
				continue
			}
			b.WriteRune(r)
			continue
		}
		quotes = 0
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString("\n\"\"\"")
	return b.String()
}

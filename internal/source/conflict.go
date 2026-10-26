package source

import (
	"encoding/json"
	"sort"

	"github.com/SYZD-research/dirdoc/internal/model"
)

// Conflict is a part of a document that a three-way merge could not decide: a top-level key
// (Section and ID empty), a key of an entry, or a whole entry (Key empty). Ours and Theirs hold the
// two sides: a string, a *model.TreeNode (the tree), a json.RawMessage (an "x-" field), a
// *model.Entry or a *model.DSDEntry (a whole entry), or nil for a side without it.
type Conflict struct {
	Section, ID, Key string
	Ours, Theirs     any
}

// Conflict markers, as git writes them.
const (
	MarkerOurs   = "<<<<<<< ours"
	MarkerSep    = "======="
	MarkerTheirs = ">>>>>>> theirs"
)

// EmitConflicts prints the merged document d or p (the other is nil) as a TOML source in which each
// conflict shows both sides between git's conflict markers, for the author to choose.
func EmitConflicts(d *model.DSD, p *model.PFD, header string, list []Conflict) string {
	cs := conflicts{}
	for _, c := range list {
		cs[[3]string{c.Section, c.ID, c.Key}] = c
	}
	if d != nil {
		return emitDSD(d, header, cs)
	}
	return emitPFD(p, header, cs)
}

type conflicts map[[3]string]Conflict

func (cs conflicts) has(sec, id, key string) bool {
	_, ok := cs[[3]string{sec, id, key}]
	return ok
}

// keys returns the conflicting keys of a table.
func (cs conflicts) keys(sec, id string) []string {
	var out []string
	for k := range cs {
		if k[0] == sec && k[1] == id && k[2] != "" {
			out = append(out, k[2])
		}
	}
	sort.Strings(out)
	return out
}

// or writes the conflict on key of a table when there is one, else what write writes.
func (w *tomlWriter) or(cs conflicts, sec, id, key string, write func()) {
	c, ok := cs[[3]string{sec, id, key}]
	if !ok {
		write()
		return
	}
	w.raw(MarkerOurs)
	w.side(sec, key, c.Ours)
	w.raw(MarkerSep)
	w.side(sec, key, c.Theirs)
	w.raw(MarkerTheirs)
}

func (w *tomlWriter) side(sec, key string, v any) {
	switch x := v.(type) {
	case string:
		if key == "document" {
			w.document(x)
		} else if x != "" || key == "summary" {
			w.kv(key, x)
		}
	case *model.TreeNode:
		if x != nil {
			w.raw("tree = " + multiline(FormatTree(x, LinkAnnotator)))
		}
	case json.RawMessage:
		w.extra(key, x)
	case *model.Entry:
		if x != nil {
			w.entry(nil, sec, *x)
		}
	case *model.DSDEntry:
		if x != nil {
			w.dsdEntry(nil, *x)
		}
	}
}

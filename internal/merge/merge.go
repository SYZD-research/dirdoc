// Package merge merges two versions of a document with their common ancestor, entry by entry and
// field by field: the git merge driver of design §16. What both sides changed differently becomes
// a conflict; the merged document then holds our side.
package merge

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/source"
)

type merger struct {
	hasBase   bool
	conflicts []source.Conflict
}

func (m *merger) conflict(sec, id, key string, ours, theirs any) {
	m.conflicts = append(m.conflicts, source.Conflict{Section: sec, ID: id, Key: key, Ours: ours, Theirs: theirs})
}

// value merges one value three ways: a side that left it as in the base gives way to the other.
func value[T comparable](base, ours, theirs T, hasBase bool) (T, bool) {
	switch {
	case ours == theirs:
		return ours, true
	case hasBase && ours == base:
		return theirs, true
	case hasBase && theirs == base:
		return ours, true
	}
	return ours, false
}

func (m *merger) str(sec, id, key, base, ours, theirs string) string {
	v, ok := value(base, ours, theirs, m.hasBase)
	if !ok {
		m.conflict(sec, id, key, ours, theirs)
	}
	return v
}

func (m *merger) extra(sec, id string, base, ours, theirs model.Extra) model.Extra {
	keys := map[string]bool{}
	for _, e := range []model.Extra{base, ours, theirs} {
		for k := range e {
			keys[k] = true
		}
	}
	out := model.Extra{}
	for _, k := range sortedKeys(keys) {
		v, ok := value(string(base[k]), string(ours[k]), string(theirs[k]), m.hasBase)
		if !ok {
			m.conflict(sec, id, k, raw(ours[k]), raw(theirs[k]))
		}
		if v != "" {
			out[k] = []byte(v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// raw keeps a missing field as an untyped nil, so that the conflict shows that side as empty.
func raw(b json.RawMessage) any {
	if b == nil {
		return nil
	}
	return b
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// item is an entry of either kind, with its fields by key.
type item struct {
	id     string
	fields map[string]string
	extra  model.Extra
}

func (it item) same(other item) bool {
	if len(it.fields) != len(other.fields) || len(it.extra) != len(other.extra) {
		return false
	}
	for k, v := range it.fields {
		if other.fields[k] != v {
			return false
		}
	}
	for k, v := range it.extra {
		if string(other.extra[k]) != string(v) {
			return false
		}
	}
	return true
}

// items merges the entries of a section by id. For an entry that one side deleted and the other
// changed, toEntry gives the whole entry of each side for the conflict.
func (m *merger) items(sec string, keys []string, base, ours, theirs []item, toEntry func(item) any) []item {
	index := func(items []item) map[string]item {
		out := map[string]item{}
		for _, it := range items {
			out[it.id] = it
		}
		return out
	}
	b, o, t := index(base), index(ours), index(theirs)
	kept := map[string]item{}
	all := map[string]bool{}
	for _, list := range [][]item{base, ours, theirs} {
		for _, it := range list {
			all[it.id] = true
		}
	}
	for _, id := range sortedKeys(all) {
		bi, inB := b[id]
		oi, inO := o[id]
		ti, inT := t[id]
		switch {
		case inO && inT:
			out := item{id: id, fields: map[string]string{}}
			hasBase := m.hasBase
			m.hasBase = inB
			for _, k := range keys {
				if v := m.str(sec, id, k, bi.fields[k], oi.fields[k], ti.fields[k]); v != "" {
					out.fields[k] = v
				}
			}
			out.extra = m.extra(sec, id, bi.extra, oi.extra, ti.extra)
			m.hasBase = hasBase
			kept[id] = out
		case inO && (!inB || !oi.same(bi)):
			if inB { // changed by us, deleted by them
				m.conflict(sec, id, "", toEntry(oi), nil)
			}
			kept[id] = oi
		case inT && (!inB || !ti.same(bi)):
			if inB {
				m.conflict(sec, id, "", nil, toEntry(ti))
			}
			kept[id] = ti
		}
		// Otherwise one side deleted an entry the other left alone, or both deleted it.
	}
	var out []item
	for _, id := range order(ids(ours), ids(theirs), func(id string) bool { _, ok := kept[id]; return ok }) {
		out = append(out, kept[id])
	}
	return out
}

func ids(items []item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.id
	}
	return out
}

// order merges two orders of keys: ours, with each key only theirs has placed after the key it
// follows in theirs. keep filters both.
func order(ours, theirs []string, keep func(string) bool) []string {
	var out []string
	pos := map[string]bool{}
	for _, k := range ours {
		if keep(k) && !pos[k] {
			out = append(out, k)
			pos[k] = true
		}
	}
	prev := ""
	for _, k := range theirs {
		if !keep(k) {
			continue
		}
		if !pos[k] {
			at := 0
			for i, x := range out {
				if x == prev {
					at = i + 1
				}
			}
			out = append(out[:at:at], append([]string{k}, out[at:]...)...)
			pos[k] = true
		}
		prev = k
	}
	return out
}

func dsdItems(entries []model.DSDEntry) []item {
	out := make([]item, len(entries))
	for i, e := range entries {
		out[i] = item{id: e.ID, fields: nonEmpty(map[string]string{"name": e.Name, "document": e.Document}), extra: e.Extra}
	}
	return out
}

func dsdEntry(it item) model.DSDEntry {
	return model.DSDEntry{ID: it.id, Name: it.fields["name"], Document: it.fields["document"], Extra: it.extra}
}

func pfdItems(entries []model.Entry) []item {
	out := make([]item, len(entries))
	for i, e := range entries {
		out[i] = item{id: e.ID, fields: nonEmpty(map[string]string{"name": e.Name, "summary": e.Summary, "document": e.Document}), extra: e.Extra}
	}
	return out
}

func pfdEntry(it item) model.Entry {
	return model.Entry{ID: it.id, Name: it.fields["name"], Summary: it.fields["summary"], Document: it.fields["document"], Extra: it.extra}
}

func nonEmpty(m map[string]string) map[string]string {
	for k, v := range m {
		if v == "" {
			delete(m, k)
		}
	}
	return m
}

// DSD merges two versions of a DSD with their common ancestor base (nil when there is none).
func DSD(base, ours, theirs *model.DSD) (*model.DSD, []source.Conflict) {
	m := &merger{hasBase: base != nil}
	if base == nil {
		base = &model.DSD{}
	}
	out := &model.DSD{
		Summary: m.str("", "", "summary", base.Summary, ours.Summary, theirs.Summary),
		Tree:    m.tree(base.Tree, ours.Tree, theirs.Tree),
		Extra:   m.extra("", "", base.Extra, ours.Extra, theirs.Extra),
	}
	toEntry := func(it item) any { e := dsdEntry(it); return &e }
	for _, it := range m.items(model.SectionDSD, []string{"name", "document"}, dsdItems(base.Entries), dsdItems(ours.Entries), dsdItems(theirs.Entries), toEntry) {
		out.Entries = append(out.Entries, dsdEntry(it))
	}
	return out, m.conflicts
}

// PFD merges two versions of a PFD with their common ancestor base (nil when there is none). The
// overview keeps the code analysis of the side whose target_hash the result takes.
func PFD(base, ours, theirs *model.PFD) (*model.PFD, []source.Conflict) {
	m := &merger{hasBase: base != nil}
	if base == nil {
		base = &model.PFD{}
	}
	out := &model.PFD{
		Target:  ours.Target,
		Summary: m.str("", "", "summary", base.Summary, ours.Summary, theirs.Summary),
		Extra:   m.extra("", "", base.Extra, ours.Extra, theirs.Extra),
	}
	// When both sides recompiled, the merged file matches neither hash: keep ours, so that the
	// document shows as stale until someone checks it (and runs dirdoc touch).
	out.TargetHash, _ = value(base.TargetHash, ours.TargetHash, theirs.TargetHash, m.hasBase)
	out.Overview = ours.Overview
	if out.TargetHash != ours.TargetHash {
		out.Overview = theirs.Overview
	}
	toEntry := func(it item) any { e := pfdEntry(it); return &e }
	for _, sec := range model.PFDSections {
		var entries []model.Entry
		for _, it := range m.items(sec, []string{"name", "summary", "document"}, pfdItems(base.Section(sec)), pfdItems(ours.Section(sec)), pfdItems(theirs.Section(sec)), toEntry) {
			entries = append(entries, pfdEntry(it))
		}
		out.SetSection(sec, entries)
	}
	m.collisions(out, ours, theirs)
	model.Reindex(out)
	return out, m.conflicts
}

// collisions reports ids that the merge put in two sections (one side added a function, the other
// a type of the same name): each such entry becomes a conflict, kept on the side that has it.
func (m *merger) collisions(out, ours, theirs *model.PFD) {
	secs := map[string][]string{}
	for _, sec := range model.PFDSections {
		for _, e := range out.Section(sec) {
			secs[e.ID] = append(secs[e.ID], sec)
		}
	}
	whole := map[[2]string]bool{}
	for _, c := range m.conflicts {
		if c.Key == "" {
			whole[[2]string{c.Section, c.ID}] = true
		}
	}
	for _, id := range sortedKeys(keysOf(secs)) {
		if len(secs[id]) < 2 {
			continue
		}
		for _, sec := range secs[id] {
			if whole[[2]string{sec, id}] {
				continue
			}
			var o, t any
			if s, e := ours.Find(id); e != nil && s == sec {
				o = e
			}
			if s, e := theirs.Find(id); e != nil && s == sec {
				t = e
			}
			m.conflict(sec, id, "", o, t)
		}
	}
}

func keysOf(m map[string][]string) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

// node is a tree node without its children's content, keyed by its path.
type node struct {
	attrs    string // type, dsd, note, more and extra, comparable at once
	n        *model.TreeNode
	children []string
}

func flatten(root *model.TreeNode) map[string]*node {
	out := map[string]*node{}
	if root == nil {
		return out
	}
	root.Walk(func(n *model.TreeNode, rel string) {
		fl := &node{n: n, attrs: fmt.Sprintf("%s\x00%s\x00%s\x00%v", n.Type, n.DSD, n.Note, n.More)}
		keys := make([]string, 0, len(n.Extra))
		for k := range n.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fl.attrs += "\x00" + k + "=" + string(n.Extra[k])
		}
		for _, c := range n.Children {
			fl.children = append(fl.children, path.Join(rel, c.Name))
		}
		out[rel] = fl
	})
	return out
}

// tree merges three trees node by node, keyed by path: the attributes of a node three ways, its
// children like entries. Any conflict makes the whole tree one, as the tree is edited as one text.
func (m *merger) tree(base, ours, theirs *model.TreeNode) *model.TreeNode {
	if ours == nil && theirs == nil {
		return nil
	}
	b, o, t := flatten(base), flatten(ours), flatten(theirs)
	paths := map[string]bool{}
	for _, f := range []map[string]*node{b, o, t} {
		for p := range f {
			paths[p] = true
		}
	}
	kept := map[string]*node{}
	ok := true
	for p := range paths {
		bn, inB := b[p]
		on, inO := o[p]
		tn, inT := t[p]
		switch {
		case inO && inT:
			attrs, same := value(attr(bn), on.attrs, tn.attrs, m.hasBase && inB)
			if !same {
				ok = false
			}
			if attrs == on.attrs {
				kept[p] = on
			} else {
				kept[p] = tn
			}
		case inO && (!inB || on.attrs != bn.attrs):
			if inB && m.hasBase {
				ok = false // changed by us, deleted by them
			}
			kept[p] = on
		case inT && (!inB || tn.attrs != bn.attrs):
			if inB && m.hasBase {
				ok = false
			}
			kept[p] = tn
		}
	}
	if _, root := kept[""]; !root && len(kept) > 0 {
		ok = false
	}
	for p := range kept {
		if p != "" {
			if _, parent := kept[parentOf(p)]; !parent {
				ok = false // a node added below one the other side deleted
			}
		}
	}
	if !ok {
		m.conflict("", "", "tree", ours, theirs)
		return ours
	}
	if len(kept) == 0 {
		return nil
	}
	var build func(p string) *model.TreeNode
	build = func(p string) *model.TreeNode {
		src := kept[p].n
		n := &model.TreeNode{Name: src.Name, Type: src.Type, DSD: src.DSD, Note: src.Note, More: src.More, Extra: src.Extra}
		var oc, tc []string
		if x := o[p]; x != nil {
			oc = x.children
		}
		if x := t[p]; x != nil {
			tc = x.children
		}
		for _, c := range order(oc, tc, func(c string) bool { _, ok := kept[c]; return ok }) {
			n.Children = append(n.Children, build(c))
		}
		return n
	}
	return build("")
}

func attr(n *node) string {
	if n == nil {
		return ""
	}
	return n.attrs
}

func parentOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

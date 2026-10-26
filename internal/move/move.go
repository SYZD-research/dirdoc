// Package move plans the documentation changes of renaming or moving a file or directory (dirdoc
// mv): the PFD moves with its file, references to the moved path are rewritten everywhere, relative
// references of moved documents are rewritten so they still point to the same place, and tree nodes
// follow the move.
package move

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/resolve"
)

// Write is a document to store after the move.
type Write struct {
	Path    string // where it is stored after the move
	From    string // where it was stored before, when that differs (the PFD of a moved file)
	DSD     *model.DSD
	PFD     *model.PFD
	Changes []string // what changed, for the report
}

// Plan is what moving Src to Dst changes in the documentation.
type Plan struct {
	Src, Dst string
	IsDir    bool
	Writes   []Write
	Warnings []string
}

// Mover maps paths from before the move to after it.
type mover struct {
	p        *resolve.Project
	src, dst string
	isDir    bool
	done     bool // the file system already shows the move
}

// to returns where abs is after the move.
func (m *mover) to(abs string) string {
	if abs == m.src {
		return m.dst
	}
	if m.isDir && strings.HasPrefix(abs, m.src+string(filepath.Separator)) {
		return m.dst + abs[len(m.src):]
	}
	return abs
}

// from returns where abs, found after the move, was before it.
func (m *mover) from(abs string) string {
	if abs == m.dst {
		return m.src
	}
	if m.isDir && strings.HasPrefix(abs, m.dst+string(filepath.Separator)) {
		return m.src + abs[len(m.dst):]
	}
	return abs
}

// Make plans moving src to dst (absolute, cleaned paths inside the project). done tells that the
// file or directory was already moved (by git mv or an editor) and only the documentation is left.
func Make(p *resolve.Project, src, dst string, isDir, done bool) (*Plan, error) {
	m := &mover{p: p, src: src, dst: dst, isDir: isDir, done: done}
	plan := &Plan{Src: src, Dst: dst, IsDir: isDir}
	var err error
	p.Documents(p.Root, func(doc resolve.Document) {
		if err != nil {
			return
		}
		if doc.Err != nil {
			err = doc.Err
			return
		}
		m.document(plan, doc)
	})
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func (m *mover) document(plan *Plan, doc resolve.Document) {
	// The owner as the document's references were written, and after the move.
	oldOwner, stored := doc.Owner, doc.Owner
	if m.done {
		oldOwner.Path = m.from(stored.Path)
	}
	newOwner := oldOwner
	newOwner.Path = m.to(oldOwner.Path)

	var w Write
	var changed int
	rewrite := func(id string, text *string) {
		out, n := m.rewrite(*text, oldOwner, newOwner)
		*text, changed = out, changed+n
	}
	if doc.DSD != nil {
		d, err := model.CloneDSD(doc.DSD)
		if err != nil {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s: %v", m.p.Ref(stored), err))
			return
		}
		d.Texts(rewrite)
		if note := m.tree(plan, d, oldOwner.Path); note != "" {
			w.Changes = append(w.Changes, note)
		}
		w.DSD, w.Path = d, resolve.DSDFile(newOwner.Path)
	} else {
		pf, err := model.ClonePFD(doc.PFD)
		if err != nil {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s: %v", m.p.Ref(stored), err))
			return
		}
		pf.Texts(rewrite)
		model.Reindex(pf)
		w.PFD, w.Path = pf, resolve.PFDFile(newOwner.Path)
		if oldOwner.Path == m.src && !m.isDir {
			// The PFD of the moved file itself: it changes directory or name.
			pf.Target = filepath.Base(m.dst)
			w.From = resolve.PFDFile(stored.Path)
			w.Changes = append(w.Changes, "moved from "+m.p.Rel(w.From, false))
		}
	}
	if changed > 0 {
		w.Changes = append(w.Changes, plural(changed, "reference"))
	}
	if len(w.Changes) > 0 {
		plan.Writes = append(plan.Writes, w)
	}
}

// rewrite updates the references in md so that they point after the move where they pointed before.
// Same-document references never change; a reference whose meaning is unchanged keeps its spelling.
func (m *mover) rewrite(md string, oldOwner, newOwner resolve.Owner) (string, int) {
	refs := resolve.FindRefs(md)
	if len(refs) == 0 {
		return md, 0
	}
	var b strings.Builder
	pos, n := 0, 0
	for _, r := range refs {
		if r.Path == "" {
			continue
		}
		newPath, ok := m.refPath(r.Path, oldOwner, newOwner)
		if !ok {
			continue
		}
		target := newPath
		if r.ID != "" {
			target += "#" + r.ID
		}
		if r.Label != "" {
			target += "|" + r.Label
		}
		b.WriteString(md[pos:r.Start])
		b.WriteString("[[" + target + "]]")
		pos = r.End
		n++
	}
	if n == 0 {
		return md, 0
	}
	b.WriteString(md[pos:])
	return b.String(), n
}

// refPath returns the new spelling of a reference path written in a document of oldOwner, and false
// when it needs no change. Root-relative paths stay root-relative; a trailing slash is kept.
func (m *mover) refPath(p string, oldOwner, newOwner resolve.Owner) (string, bool) {
	rootRel := strings.HasPrefix(p, "/")
	rel, base := p, oldOwner.Dir()
	if rootRel {
		rel, base = strings.TrimLeft(p, "/"), m.p.Root
	}
	oldTarget := filepath.Join(base, filepath.FromSlash(rel))
	newTarget := m.to(oldTarget)
	if !m.p.Within(oldTarget) {
		return "", false // outside the project: an error either way, left as written
	}
	slash := strings.HasSuffix(p, "/")
	if rootRel {
		if newTarget == oldTarget {
			return "", false
		}
		return "/" + spell(m.p.Root, newTarget, slash), true
	}
	if newTarget == oldTarget && newOwner.Dir() == oldOwner.Dir() {
		return "", false
	}
	if spell(newOwner.Dir(), newTarget, slash) == spell(oldOwner.Dir(), oldTarget, slash) {
		return "", false
	}
	return spell(newOwner.Dir(), newTarget, slash), true
}

// spell writes target relative to dir with forward slashes, adding a trailing slash when asked.
func spell(dir, target string, slash bool) string {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		rel = target
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		rel = ""
		if slash {
			return "./"
		}
		return "."
	}
	if slash {
		rel += "/"
	}
	return rel
}

// tree moves the node of the moved path in the tree of the DSD of dir (its location before the
// move), when the tree lists it. It returns a note for the report.
func (m *mover) tree(plan *Plan, d *model.DSD, dir string) string {
	if d.Tree == nil || !within(dir, m.src) || dir == m.src {
		return "" // the tree moves along with dir, or does not reach the moved path
	}
	oldRel := relSlash(dir, m.src)
	node := d.Tree.Find(oldRel)
	if node == nil || oldRel == "" {
		return ""
	}
	oldParent := d.Tree.Find(path.Dir(oldRel))
	if oldParent == nil {
		return ""
	}
	if !within(dir, m.dst) {
		detach(oldParent, node)
		if node.DSD != "" || node.Note != "" {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s: removed %s from the tree; its link or note did not move (%s is outside the tree)",
				m.p.Rel(resolve.DSDFile(dir), false), oldRel, m.p.Rel(m.dst, m.isDir)))
		}
		return "tree: removed " + oldRel
	}
	newRel := relSlash(dir, m.dst)
	name, parentRel := path.Base(newRel), path.Dir(newRel)
	if d.Tree.Find(parentRel) == oldParent {
		if oldParent.Child(name) == nil {
			node.Name = name // a rename keeps its place in the list
			return "tree: " + oldRel + " -> " + newRel
		}
	} else if parent := listedDir(d.Tree, parentRel); parent != nil && parent.Child(name) == nil {
		detach(oldParent, node)
		node.Name = name
		insert(parent, node, -1)
		return "tree: " + oldRel + " -> " + newRel
	}
	detach(oldParent, node)
	if node.DSD != "" || node.Note != "" {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s: removed %s from the tree; move its link or note to %s by hand",
			m.p.Rel(resolve.DSDFile(dir), false), oldRel, newRel))
	}
	return "tree: removed " + oldRel
}

// listedDir returns the directory node at rel, creating the missing directories on the way below a
// node that lists its children. It returns nil when the tree does not show that place: a folded
// directory (one that lists no children) or a file is in the way, or rel itself is folded.
func listedDir(root *model.TreeNode, rel string) *model.TreeNode {
	cur, created := root, false
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." {
			continue
		}
		if !created && len(cur.Children) == 0 {
			return nil
		}
		next := cur.Child(seg)
		if next == nil {
			next, created = &model.TreeNode{Name: seg, Type: model.NodeDir}, true
			insert(cur, next, -1)
		}
		if !next.IsDir() {
			return nil
		}
		cur = next
	}
	if !created && len(cur.Children) == 0 {
		return nil
	}
	return cur
}

// insert adds node to the children of parent at index at, or when at < 0 where the tree command
// would list it: a directory before the first file, a file last.
func insert(parent, node *model.TreeNode, at int) {
	if at < 0 {
		at = len(parent.Children)
		if node.IsDir() {
			for i, c := range parent.Children {
				if !c.IsDir() {
					at = i
					break
				}
			}
		}
	}
	parent.Children = append(parent.Children[:at:at], append([]*model.TreeNode{node}, parent.Children[at:]...)...)
}

// detach removes node from the children of parent and returns where it was.
func detach(parent, node *model.TreeNode) int {
	for i, c := range parent.Children {
		if c == node {
			parent.Children = append(parent.Children[:i:i], parent.Children[i+1:]...)
			return i
		}
	}
	return len(parent.Children)
}

// within reports whether abs is dir or below it.
func within(dir, abs string) bool {
	return abs == dir || strings.HasPrefix(abs, dir+string(filepath.Separator))
}

func relSlash(base, abs string) string {
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

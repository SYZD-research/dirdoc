package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SYZD-research/dirdoc/internal/container"
	"github.com/SYZD-research/dirdoc/internal/diag"
	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/resolve"
)

func (a *App) rmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm PATH[#ID]...",
		Short: "Remove a document (PATH) or one of its entries (PATH#ID)",
		Long: `Remove the DSD of a directory (dirdoc rm src/), the PFD of a file (dirdoc rm src/main.rs), or one
entry: 'dirdoc rm src/main.rs#parse', 'dirdoc rm src/#cli'. A PFD can be removed after its file is
gone. Removing a dsd entry also removes the tree links to it.

The document must stay valid, so remove references to an entry from its own document first.
References from other documents are reported as warnings.`,
		Args: usageArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.rm(args)
		},
	}
}

// rmTarget is a document and what to remove from it: the whole document when ids is nil.
type rmTarget struct {
	abs   string
	isDir bool
	ids   []string
	whole bool
}

func (a *App) rm(args []string) error {
	var targets []*rmTarget
	byPath := map[string]*rmTarget{}
	for _, arg := range args {
		pathArg, id := a.splitPathID(arg)
		abs := a.abs(pathArg)
		if resolve.InsideDirdoc(abs) {
			return exitf(exitError, "%s is inside a .dirdoc directory; name the file or directory it documents", arg)
		}
		st, err := os.Stat(abs)
		isDir := err == nil && st.IsDir() || strings.HasSuffix(pathArg, "/")
		switch {
		case isDir && !exists(resolve.DSDFile(abs)):
			return exitf(exitError, "%s has no DSD", pathArg)
		case !isDir && !exists(resolve.PFDFile(abs)):
			if err != nil {
				return exitf(exitError, "%s: no such file or directory, and no PFD", pathArg)
			}
			return exitf(exitError, "%s has no PFD", pathArg)
		}
		t := byPath[abs]
		if t == nil {
			t = &rmTarget{abs: abs, isDir: isDir}
			byPath[abs] = t
			targets = append(targets, t)
		}
		if id == "" {
			t.whole = true
		} else if !contains(t.ids, id) {
			t.ids = append(t.ids, id)
		}
	}
	for _, t := range targets {
		if err := a.rmTarget(t); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) rmTarget(t *rmTarget) error {
	p, err := a.openProject(t.abs)
	if err != nil {
		return err
	}
	owner := resolve.FileOwner(t.abs)
	docPath := resolve.PFDFile(t.abs)
	if t.isDir {
		owner, docPath = resolve.DirOwner(t.abs), resolve.DSDFile(t.abs)
	}
	backlinks := p.Backlinks(t.abs)
	var warn diag.List
	// stillReferred warns about references from other documents to what was removed: entry id, or
	// anything in the document when id is "".
	stillReferred := func(id string) {
		for _, b := range backlinks {
			if b.From == owner || (id != "" && b.ID != id) {
				continue
			}
			if b.ID == "" && t.isDir && p.Inherited(t.abs) != nil {
				continue // the directory is still described by an ancestor's tree
			}
			from, target := p.Ref(b.From), p.Ref(owner)
			if b.FromID != "" {
				from += "#" + b.FromID
			}
			if b.ID != "" {
				target += "#" + b.ID
			}
			warn.Warnf(diag.CodeRefBroken, from, "refers to %s, which was removed", target)
		}
	}

	if t.whole {
		if err := os.Remove(docPath); err != nil {
			return exitf(exitError, "%v", err)
		}
		os.Remove(filepath.Dir(docPath)) // only succeeds when .dirdoc/ is now empty
		if t.isDir {
			p.SetDSD(t.abs, nil)
		} else {
			p.SetPFD(t.abs, nil)
		}
		fmt.Fprintf(a.Stdout, "removed %s\n", p.Rel(docPath, false))
		stillReferred("")
		a.printDiags(a.Stderr, warn.Dedup())
		return nil
	}

	// Refuse only the errors the removal introduces, such as a [[#id]] left pointing to it.
	var doc any
	var before, after diag.List
	val := diag.Validator{P: p, FS: true}
	if t.isDir {
		d, err := p.DSD(t.abs)
		if err != nil {
			return exitf(exitError, "%v", err)
		}
		before = val.DSD(t.abs, d)
		for _, id := range t.ids {
			if d.Entry(id) == nil {
				return exitf(exitError, "%s has no dsd entry %q", p.Ref(owner), id)
			}
			for i := range d.Entries {
				if d.Entries[i].ID == id {
					d.Entries = append(d.Entries[:i], d.Entries[i+1:]...)
					break
				}
			}
			if d.Tree != nil {
				d.Tree.Walk(func(n *model.TreeNode, _ string) {
					if n.DSD == id {
						n.DSD = ""
					}
				})
			}
		}
		p.SetDSD(t.abs, d)
		after, doc = val.DSD(t.abs, d), d
	} else {
		pf, err := p.PFD(t.abs)
		if err != nil {
			return exitf(exitError, "%v", err)
		}
		before = val.PFD(t.abs, pf)
		for _, id := range t.ids {
			sec, e := pf.Find(id)
			if e == nil {
				return exitf(exitError, "%s has no entry %q", p.Ref(owner), id)
			}
			entries := pf.Section(sec)
			for i := range entries {
				if entries[i].ID == id {
					pf.SetSection(sec, append(entries[:i:i], entries[i+1:]...))
					break
				}
			}
		}
		model.Reindex(pf)
		p.SetPFD(t.abs, pf)
		after, doc = val.PFD(t.abs, pf), pf
	}
	had := map[diag.Diagnostic]bool{}
	for _, d := range before {
		had[d] = true
	}
	var errs diag.List
	for _, d := range after {
		if d.Severity == diag.Error && !had[d] {
			errs = append(errs, d)
		}
	}
	if len(errs) > 0 {
		a.printDiags(a.Stderr, errs)
		return exitf(exitError, "nothing removed: the document would have %s", plural(len(errs), "error"))
	}
	js, err := model.Marshal(doc)
	if err != nil {
		return exitf(exitError, "%v", err)
	}
	if _, err := container.WriteFile(docPath, js, p.Config.Compress(), container.Level(p.Config.Storage.Level)); err != nil {
		return exitf(exitError, "%v", err)
	}
	for _, id := range t.ids {
		fmt.Fprintf(a.Stdout, "removed %s from %s\n", p.EntryRef(owner, id), p.Rel(docPath, false))
		stillReferred(id)
	}
	a.printDiags(a.Stderr, warn.Dedup())
	return nil
}

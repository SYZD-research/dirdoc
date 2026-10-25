package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"dirdoc/internal/compile"
	"dirdoc/internal/container"
	"dirdoc/internal/diag"
	"dirdoc/internal/model"
	"dirdoc/internal/resolve"
	"dirdoc/internal/source"
)

func (a *App) compileCmd() *cobra.Command {
	var merge, dryRun, strict bool
	c := &cobra.Command{
		Use:   "compile TARGET SOURCE|-",
		Short: "Validate a TOML (or JSON) source and store it as TARGET's documentation",
		Long: `Validate a source and store it as the documentation of TARGET: the DSD when TARGET is a
directory, the PFD when it is a file. SOURCE "-" reads standard input, so a heredoc works.

With --merge, SOURCE holds only the changes: the keys it sets replace the stored ones, entries
are matched by id (or by the id derived from their name), and delete = true removes an entry.`,
		Args: usageArgs(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.compile(args[0], args[1], merge, dryRun, strict)
		},
	}
	c.Flags().BoolVar(&merge, "merge", false, "apply SOURCE as a partial update of the stored document")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "validate only; do not write")
	c.Flags().BoolVar(&strict, "strict", false, "treat warnings as errors")
	return c
}

func targetKind(abs string) (model.Kind, error) {
	st, err := os.Stat(abs)
	if err != nil {
		return 0, err
	}
	if st.IsDir() {
		return model.KindDSD, nil
	}
	return model.KindPFD, nil
}

func countNodes(n *model.TreeNode) int {
	if n == nil {
		return 0
	}
	count := 0
	n.Walk(func(*model.TreeNode, string) { count++ })
	return count - 1
}

func (a *App) compile(target, srcArg string, merge, dryRun, strict bool) error {
	abs := a.abs(target)
	if resolve.InsideDirdoc(abs) {
		return exitf(exitError, "%s is inside a .dirdoc directory; compile the file or directory it documents", target)
	}
	kind, err := targetKind(abs)
	if err != nil {
		return exitf(exitError, "%s: no such file or directory (compile documents existing files and directories)", target)
	}
	p, err := a.openProject(abs)
	if err != nil {
		return err
	}
	var data []byte
	srcName := srcArg
	if srcArg == "-" {
		srcName = "<stdin>"
		data, err = io.ReadAll(a.Stdin)
	} else {
		data, err = os.ReadFile(a.abs(srcArg))
	}
	if err != nil {
		return exitf(exitError, "%v", err)
	}

	doc, all := source.Parse(data, srcName, kind)
	if doc == nil {
		a.printDiags(a.Stderr, all)
		return exitf(exitError, "nothing written: %s is not valid", srcName)
	}
	// Keep going after key errors so that one run reports every problem.
	val := diag.Validator{P: p, File: srcName, FS: true}
	var js []byte
	var outPath, counts string
	if kind == model.KindDSD {
		var base *model.DSD
		if merge {
			if base, err = p.DSD(abs); err != nil {
				return exitf(exitError, "cannot merge into the stored DSD: %v", err)
			}
		}
		d, loc, l := compile.DSD(doc, base, merge)
		all = append(all, l...)
		p.SetDSD(abs, d)
		val.Loc = loc
		all = append(all, val.DSD(abs, d)...)
		js, err = model.Marshal(d)
		outPath = resolve.DSDFile(abs)
		counts = fmt.Sprintf("dsd: %d, tree nodes: %d", len(d.Entries), countNodes(d.Tree))
	} else {
		var base *model.PFD
		if merge {
			if base, err = p.PFD(abs); err != nil {
				return exitf(exitError, "cannot merge into the stored PFD: %v", err)
			}
		}
		pf, loc, l := compile.PFD(doc, base, filepath.Base(abs), merge)
		all = append(all, l...)
		p.SetPFD(abs, pf)
		val.Loc = loc
		all = append(all, val.PFD(abs, pf)...)
		js, err = model.Marshal(pf)
		outPath = resolve.PFDFile(abs)
		counts = fmt.Sprintf("types: %d, functions: %d, variables: %d", len(pf.Types), len(pf.Functions), len(pf.Variables))
	}
	if err != nil {
		return exitf(exitError, "%v", err)
	}
	all.SortByLine()
	a.printDiags(a.Stderr, all)
	rel := p.Rel(outPath, false)
	if all.Failed(strict) {
		e, w := all.Count()
		if e == 0 {
			return exitf(exitError, "nothing written: %s with --strict", plural(w, "warning"))
		}
		return exitf(exitError, "nothing written: %s", plural(e, "error"))
	}
	if dryRun {
		fmt.Fprintf(a.Stdout, "dry run: %s is valid (%s)\n", rel, counts)
		return nil
	}
	written, err := container.WriteFile(outPath, js, p.Config.Compress(), container.Level(p.Config.Storage.Level))
	if err != nil {
		return exitf(exitError, "%v", err)
	}
	if written {
		fmt.Fprintf(a.Stdout, "wrote %s (%s)\n", rel, counts)
	} else {
		fmt.Fprintf(a.Stdout, "unchanged %s (%s)\n", rel, counts)
	}
	return nil
}

func (a *App) exportCmd() *cobra.Command {
	var out, format string
	var depth int
	c := &cobra.Command{
		Use:   "export TARGET",
		Short: "Print the editable source of TARGET's documentation (a template when there is none)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "toml" && format != "json" {
				return usagef(cmd, "unknown format %q (use toml or json)", format)
			}
			return a.export(args[0], out, format, depth)
		},
	}
	c.Flags().StringVarP(&out, "output", "o", "", "write to FILE instead of standard output")
	c.Flags().StringVar(&format, "format", "toml", "source format: toml or json")
	c.Flags().IntVar(&depth, "depth", 1, "levels of the tree a new DSD template lists")
	return c
}

func (a *App) export(target, out, format string, depth int) error {
	abs := a.abs(target)
	if resolve.InsideDirdoc(abs) {
		return exitf(exitError, "%s is inside a .dirdoc directory", target)
	}
	kind, err := targetKind(abs)
	if err != nil {
		if _, perr := os.Stat(resolve.PFDFile(abs)); perr != nil {
			return exitf(exitError, "%s: no such file or directory", target)
		}
		kind = model.KindPFD // the file is gone but its PFD remains
	}
	p, err := a.openProject(abs)
	if err != nil {
		return err
	}
	isDir := kind == model.KindDSD
	rel := p.Rel(abs, isDir)
	header := fmt.Sprintf("dirdoc %s: %s", kind, rel)
	compileCmd := fmt.Sprintf("dirdoc compile %s <this file>", shellArg(rel))

	var text string
	if isDir {
		d, err := p.DSD(abs)
		if err != nil {
			return exitf(exitError, "%v", err)
		}
		switch {
		case d != nil && format == "json":
			text, err = source.EmitJSON(d, nil)
		case d != nil:
			text = source.EmitDSD(d, header)
		case format == "json":
			text, err = source.EmitJSON(&model.DSD{Tree: scaffoldTree(p, abs, depth)}, nil)
		default:
			text = source.ScaffoldDSD(header, compileCmd, scaffoldTree(p, abs, depth))
		}
		if err != nil {
			return exitf(exitError, "%v", err)
		}
	} else {
		pf, err := p.PFD(abs)
		if err != nil {
			return exitf(exitError, "%v", err)
		}
		switch {
		case pf != nil && format == "json":
			text, err = source.EmitJSON(nil, pf)
		case pf != nil:
			text = source.EmitPFD(pf, header)
		case format == "json":
			text, err = source.EmitJSON(nil, &model.PFD{Target: filepath.Base(abs)})
		default:
			text = source.ScaffoldPFD(header, compileCmd)
		}
		if err != nil {
			return exitf(exitError, "%v", err)
		}
	}
	if out == "" {
		_, err = fmt.Fprint(a.Stdout, text)
		return err
	}
	if err := os.WriteFile(a.abs(out), []byte(text), 0o644); err != nil {
		return exitf(exitError, "%v", err)
	}
	fmt.Fprintf(a.Stderr, "wrote %s\n", out)
	return nil
}

// scaffoldTree lists dir from the file system for a new DSD, down to depth levels. Subdirectories
// that have their own DSD get its summary as their note.
func scaffoldTree(p *resolve.Project, dir string, depth int) *model.TreeNode {
	root := &model.TreeNode{Name: ".", Type: model.NodeDir}
	var fill func(n *model.TreeNode, abs string, level int)
	fill = func(n *model.TreeNode, abs string, level int) {
		entries, err := p.Ignore.List(abs)
		if err != nil {
			return
		}
		for _, e := range entries {
			child := &model.TreeNode{Name: e.Name, Type: model.NodeFile}
			if e.IsDir {
				child.Type = model.NodeDir
				cabs := filepath.Join(abs, e.Name)
				if d, err := p.DSD(cabs); err == nil && d != nil {
					child.Note = firstLine(d.Summary)
				}
				if level < depth {
					fill(child, cabs, level+1)
				}
			}
			n.Children = append(n.Children, child)
		}
	}
	fill(root, dir, 1)
	return root
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}

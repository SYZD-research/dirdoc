package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SYZD-research/dirdoc/internal/container"
	"github.com/SYZD-research/dirdoc/internal/diag"
	"github.com/SYZD-research/dirdoc/internal/merge"
	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/resolve"
	"github.com/SYZD-research/dirdoc/internal/source"
)

func (a *App) mergeDriverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "merge-driver BASE OURS THEIRS [PATH]",
		Short: "Merge two versions of a .dirdoc_* file with their ancestor, entry by entry (git merge driver)",
		Long: `The git merge driver for .dirdoc_* files, which 'dirdoc init --git' sets up: git runs
"dirdoc merge-driver %O %A %B %P". Entries are matched by id and merged field by field, the tree
node by node, and the result replaces OURS.

When both sides changed the same thing differently, OURS is replaced by the TOML source of the
merged document with git's conflict markers around both sides, and the exit code is 1. Resolve the
markers, then store the file with: dirdoc compile TARGET FILE`,
		Args: usageArgs(cobra.RangeArgs(3, 4)),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ""
			if len(args) == 4 {
				path = args[3]
			}
			return a.mergeDriver(args[0], args[1], args[2], path)
		},
	}
}

type mergeSide struct {
	kind       model.Kind
	dsd        *model.DSD
	pfd        *model.PFD
	compressed bool
}

func (a *App) readSide(name string, optional bool) (*mergeSide, error) {
	data, err := os.ReadFile(a.abs(name))
	if err != nil {
		return nil, err
	}
	if optional && len(bytes.TrimSpace(data)) == 0 {
		return nil, nil // no common ancestor: both sides added the file
	}
	js, compressed, err := container.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	kind, d, pf, err := model.Decode(js)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &mergeSide{kind: kind, dsd: d, pfd: pf, compressed: compressed}, nil
}

func (a *App) mergeDriver(basePath, oursPath, theirsPath, path string) error {
	base, err := a.readSide(basePath, true)
	if err != nil {
		return exitf(exitError, "%v", err)
	}
	ours, err := a.readSide(oursPath, false)
	if err != nil {
		return exitf(exitError, "%v", err)
	}
	theirs, err := a.readSide(theirsPath, false)
	if err != nil {
		return exitf(exitError, "%v", err)
	}
	if ours.kind != theirs.kind || base != nil && base.kind != ours.kind {
		return exitf(exitError, "cannot merge a DSD with a PFD")
	}

	var d *model.DSD
	var pf *model.PFD
	var conflicts []source.Conflict
	if ours.kind == model.KindDSD {
		var b *model.DSD
		if base != nil {
			b = base.dsd
		}
		d, conflicts = merge.DSD(b, ours.dsd, theirs.dsd)
	} else {
		var b *model.PFD
		if base != nil {
			b = base.pfd
		}
		pf, conflicts = merge.PFD(b, ours.pfd, theirs.pfd)
	}

	// With PATH, dirdoc knows which document this is: it checks the result and names the target.
	level, target, stored := container.LevelBest, "", path
	var errs diag.List
	if path != "" {
		abs := a.abs(path)
		owner := filepath.Dir(filepath.Dir(abs))
		if pf != nil {
			owner = filepath.Join(owner, strings.TrimSuffix(filepath.Base(abs), model.PFDSuffix))
		}
		if p, err := resolve.Open(a.Cwd, owner); err == nil {
			level, stored = container.Level(p.Config.Storage.Level), p.Rel(abs, false)
			val := diag.Validator{P: p}
			var l diag.List
			if pf != nil {
				p.SetPFD(owner, pf)
				target, l = p.Rel(owner, false), val.PFD(owner, pf)
			} else {
				p.SetDSD(owner, d)
				target, l = p.Rel(owner, true), val.DSD(owner, d)
			}
			for _, dg := range l {
				if dg.Severity == diag.Error {
					errs = append(errs, dg)
				}
			}
		}
	}
	if target == "" {
		target = "TARGET"
		if pf != nil {
			target = pf.Target
		}
	}
	if stored == "" {
		stored = "FILE"
	}

	if len(conflicts) == 0 && len(errs) == 0 {
		var js []byte
		if pf != nil {
			js, err = model.Marshal(pf)
		} else {
			js, err = model.Marshal(d)
		}
		if err == nil {
			_, err = container.WriteFile(a.abs(oursPath), js, ours.compressed, level)
		}
		if err != nil {
			return exitf(exitError, "%v", err)
		}
		return nil
	}

	kind := model.KindDSD
	if pf != nil {
		kind = model.KindPFD
	}
	header := []string{fmt.Sprintf("dirdoc %s: %s", kind, target)}
	if len(conflicts) > 0 {
		header = append(header, "Merge conflict: keep one side of each block between "+source.MarkerOurs+" and "+source.MarkerTheirs,
			"(or combine them) and delete the markers.")
	}
	if len(errs) > 0 {
		header = append(header, "The merged document has errors to fix:")
		for _, dg := range errs {
			header = append(header, "  "+dg.String())
		}
	}
	compile := fmt.Sprintf("dirdoc compile %s %s", shellArg(target), shellArg(stored))
	header = append(header, "Then store this file again with:", "  "+compile)
	text := source.EmitConflicts(d, pf, strings.Join(header, "\n"), conflicts)
	if err := os.WriteFile(a.abs(oursPath), []byte(text), 0o644); err != nil {
		return exitf(exitError, "%v", err)
	}
	fmt.Fprintf(a.Stderr, "dirdoc: cannot merge %s automatically:\n", stored)
	for _, c := range conflicts {
		fmt.Fprintln(a.Stderr, "  conflict: "+describeConflict(target, kind, c))
	}
	for _, dg := range errs {
		fmt.Fprintln(a.Stderr, "  "+dg.String())
	}
	fmt.Fprintf(a.Stderr, "dirdoc: resolve the file, then run: %s\n", compile)
	return &exitErr{code: exitError}
}

func describeConflict(target string, kind model.Kind, c source.Conflict) string {
	if c.ID == "" {
		return c.Key + " changed on both sides"
	}
	ref := target + "#" + c.ID
	if kind == model.KindDSD {
		ref = strings.TrimSuffix(target, "/") + "/#" + c.ID
	}
	if c.Key == "" {
		return ref + " (" + model.SectionKind(c.Section) + ") changed on one side and removed or moved on the other"
	}
	return ref + " " + c.Key + " changed on both sides"
}

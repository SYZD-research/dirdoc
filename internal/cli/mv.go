package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SYZD-research/dirdoc/internal/container"
	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/move"
	"github.com/SYZD-research/dirdoc/internal/resolve"
)

func (a *App) mvCmd() *cobra.Command {
	var dryRun bool
	c := &cobra.Command{
		Use:   "mv SOURCE DEST",
		Short: "Move or rename a file or directory together with its documentation",
		Long: `Move SOURCE to DEST (into DEST when it is a directory), then update the documentation: the PFD of
a moved file goes with it, references to the moved path are rewritten in every document, relative
references in moved documents keep pointing to the same places, and trees that list SOURCE follow it.

When SOURCE was already moved (by git mv or an editor) and DEST exists, only the documentation is
updated.`,
		Args: usageArgs(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.mv(args[0], args[1], dryRun)
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print what would change; do not move or write anything")
	return c
}

func (a *App) mv(srcArg, dstArg string, dryRun bool) error {
	src, dst := a.abs(srcArg), a.abs(dstArg)
	if resolve.InsideDirdoc(src) || resolve.InsideDirdoc(dst) {
		return exitf(exitError, "dirdoc mv moves files and directories, not the files in .dirdoc/")
	}
	srcSt, srcErr := os.Stat(src)
	dstSt, dstErr := os.Stat(dst)
	done := false
	switch {
	case srcErr == nil && dstErr == nil && dstSt.IsDir():
		dst = filepath.Join(dst, filepath.Base(src))
		if exists(dst) {
			return exitf(exitError, "%s already exists", filepath.Join(dstArg, filepath.Base(src)))
		}
	case srcErr == nil && dstErr == nil:
		return exitf(exitError, "%s already exists", dstArg)
	case srcErr == nil:
		if st, err := os.Stat(filepath.Dir(dst)); err != nil || !st.IsDir() {
			return exitf(exitError, "%s: no such directory", filepath.Dir(dstArg))
		}
	case dstErr == nil:
		done = true // already moved: only the documentation follows
	default:
		return exitf(exitError, "%s: no such file or directory", srcArg)
	}
	isDir := srcErr == nil && srcSt.IsDir() || srcErr != nil && dstSt.IsDir()
	if src == dst {
		return exitf(exitError, "%s and %s are the same", srcArg, dstArg)
	}
	if isDir && strings.HasPrefix(dst, src+string(filepath.Separator)) {
		return exitf(exitError, "cannot move %s into itself", srcArg)
	}
	p, err := a.openProject(src)
	if err != nil {
		return err
	}
	if !p.Within(src) || !p.Within(dst) || src == p.Root {
		return exitf(exitError, "dirdoc mv moves paths inside the project %s", p.Rel(p.Root, true))
	}
	if !isDir && exists(resolve.PFDFile(dst)) {
		return exitf(exitError, "%s already has a PFD (%s)", p.Rel(dst, false), p.Rel(resolve.PFDFile(dst), false))
	}
	plan, err := move.Make(p, src, dst, isDir, done)
	if err != nil {
		return exitf(exitError, "%v", err)
	}

	verb := "moved"
	if dryRun {
		verb = "would move"
	}
	if done {
		fmt.Fprintf(a.Stdout, "%s the documentation of %s to %s\n", verb, p.Rel(src, isDir), p.Rel(dst, isDir))
	} else {
		fmt.Fprintf(a.Stdout, "%s %s to %s\n", verb, p.Rel(src, isDir), p.Rel(dst, isDir))
	}
	if !dryRun && !done {
		if err := os.Rename(src, dst); err != nil {
			return exitf(exitError, "%v", err)
		}
	}
	for _, w := range plan.Writes {
		line := p.Rel(w.Path, false) + " (" + strings.Join(w.Changes, ", ") + ")"
		if dryRun {
			fmt.Fprintln(a.Stdout, "would update "+line)
			continue
		}
		var doc any = w.DSD
		if w.PFD != nil {
			doc = w.PFD
		}
		js, err := model.Marshal(doc)
		if err == nil {
			_, err = container.WriteFile(w.Path, js, p.Config.Compress(), container.Level(p.Config.Storage.Level))
		}
		if err == nil && w.From != "" {
			err = os.Remove(w.From)
			os.Remove(filepath.Dir(w.From)) // only succeeds when .dirdoc/ is now empty
		}
		if err != nil {
			return exitf(exitError, "%v", err)
		}
		fmt.Fprintln(a.Stdout, "updated "+line)
	}
	for _, warning := range plan.Warnings {
		fmt.Fprintln(a.Stderr, "warning: "+warning)
	}
	return nil
}

package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/SYZD-research/dirdoc/internal/config"
	"github.com/SYZD-research/dirdoc/internal/resolve"
	"github.com/SYZD-research/dirdoc/internal/store"
)

func (a *App) convertCmd() *cobra.Command {
	var to string
	var dryRun bool
	c := &cobra.Command{
		Use:   "convert [PATH]",
		Short: "Rewrite the stored documentation below PATH in another storage format (zstd, json or toml)",
		Long: `Rewrite every stored document below PATH (default: the whole project), or the PFD of a file,
in the format --to names: zstd (compressed JSON), json or toml. Without --to, the documents take
the format [storage] format sets in dirdoc.toml. Only the storage changes: the descriptions, the
target_hash and the code analysis stay as they are.

To switch the project to another format, set [storage] format in dirdoc.toml first, then run
dirdoc convert. With a --to that differs from dirdoc.toml, the files dirdoc writes later take the
format of dirdoc.toml again.`,
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if to != "" && !store.Format(to).Valid() {
				return usagef(cmd, "unknown format %q (use zstd, json or toml)", to)
			}
			return a.convert(args, store.Format(to), dryRun)
		},
	}
	c.Flags().StringVar(&to, "to", "", "zstd, json or toml (default: [storage] format in dirdoc.toml)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "list what would be converted; do not write")
	return c
}

func (a *App) convert(args []string, to store.Format, dryRun bool) error {
	start := a.Cwd
	if len(args) == 1 {
		start = a.abs(args[0])
		if _, err := os.Stat(start); err != nil {
			if _, perr := os.Stat(resolve.PFDFile(start)); perr != nil {
				return exitf(exitError, "%s: no such file or directory", args[0])
			}
		}
	}
	p, err := a.openProject(start)
	if err != nil {
		return err
	}
	start = a.startPath(args, p)
	configured := store.Configured(p.Config)
	s := configured
	if to != "" {
		s.Format = to
	}
	verb := "converted"
	if dryRun {
		verb = "would convert"
	}

	converted, already, failed := 0, 0, 0
	one := func(path string) {
		rel := p.Rel(path, false)
		f, err := store.ReadFile(path)
		if err == nil && f.Format == s.Format {
			already++
			return
		}
		if err == nil {
			if dryRun {
				_, err = store.Encode(f.DSD, f.PFD, s) // so that a document TOML cannot hold shows up
			} else {
				_, err = store.WriteFile(path, f.DSD, f.PFD, s)
			}
		}
		if err != nil {
			failed++
			fmt.Fprintf(a.Stderr, "dirdoc: %s: %v\n", rel, err)
			return
		}
		converted++
		fmt.Fprintf(a.Stdout, "%s %s (%s -> %s)\n", verb, rel, f.Format, s.Format)
	}
	found := eachDocument(p, start,
		func(dir string) { one(resolve.DSDFile(dir)) },
		func(file string) { one(resolve.PFDFile(file)) },
		func(string) {})
	if !found {
		fmt.Fprintf(a.Stdout, "%s has no PFD; nothing to convert\n", p.Rel(start, false))
		return nil
	}

	line := fmt.Sprintf("%s %s to %s", verb, plural(converted, "document"), s.Format)
	switch {
	case already > 0 && failed > 0:
		line += fmt.Sprintf(" (%d already %s, %d failed)", already, s.Format, failed)
	case already > 0:
		line += fmt.Sprintf(" (%d already %s)", already, s.Format)
	case failed > 0:
		line += fmt.Sprintf(" (%d failed)", failed)
	}
	fmt.Fprintln(a.Stdout, line)
	if s.Format != configured.Format {
		where := "dirdoc.toml stores documents as " + string(configured.Format)
		if _, err := os.Stat(filepath.Join(p.Root, config.FileName)); err != nil {
			where = "documents are stored as " + string(configured.Format) + " by default"
		}
		fmt.Fprintf(a.Stderr, "warning: %s, so files dirdoc writes later become %s again (set [storage] format = %q in %s to keep %s)\n",
			where, configured.Format, s.Format, config.FileName, s.Format)
	}
	if failed > 0 {
		return &exitErr{code: exitError}
	}
	return nil
}

package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"dirdoc/internal/render"
	"dirdoc/internal/resolve"
)

type showOptions struct {
	format    string
	brief     bool
	full      bool
	recursive bool
	depth     int
}

func (o *showOptions) register(c *cobra.Command) {
	f := c.Flags()
	f.StringVarP(&o.format, "format", "f", "", "output format: md or json (default from dirdoc.toml, else md)")
	f.BoolVar(&o.brief, "brief", false, "only summaries, the tree and one line per file")
	f.BoolVar(&o.full, "full", false, "also the documents of every type and function")
	f.BoolVarP(&o.recursive, "recursive", "r", false, "also show subdirectories that have their own .dirdoc")
	f.IntVar(&o.depth, "depth", -1, "with -r, how many levels below PATH to show (implies -r)")
}

// splitPathID splits "PATH#ID" unless the whole argument names an existing path.
func (a *App) splitPathID(arg string) (string, string) {
	if _, err := os.Stat(a.abs(arg)); err == nil {
		return arg, ""
	}
	if i := strings.LastIndex(arg, "#"); i >= 0 {
		p := arg[:i]
		if p == "" {
			p = "."
		}
		return p, arg[i+1:]
	}
	return arg, ""
}

func (a *App) show(cmd *cobra.Command, args []string, o *showOptions) error {
	arg := "."
	if len(args) == 1 {
		arg = args[0]
	}
	if o.brief && o.full {
		return usagef(cmd, "--brief and --full cannot be used together")
	}
	pathArg, id := a.splitPathID(arg)
	abs := a.abs(pathArg)
	if resolve.InsideDirdoc(abs) {
		return exitf(exitError, "%s is inside a .dirdoc directory; read stored files with `dirdoc cat FILE`", arg)
	}
	st, statErr := os.Stat(abs)
	isDir := statErr == nil && st.IsDir()
	if statErr != nil {
		// A deleted file may still have its PFD (an orphan); anything else does not exist.
		if _, err := os.Stat(resolve.PFDFile(abs)); err != nil {
			if len(args) == 1 && !strings.ContainsAny(arg, "/#.") {
				if s := cmd.Root().SuggestionsFor(arg); len(s) > 0 {
					return usagef(cmd, "unknown command or path %q (did you mean %q?)", arg, s[0])
				}
			}
			return exitf(exitError, "%s: no such file or directory", pathArg)
		}
	}
	p, err := a.openProject(abs)
	if err != nil {
		return err
	}
	format := o.format
	if format == "" {
		format = p.Config.Show.Format
	}
	if format != "md" && format != "json" {
		return usagef(cmd, "unknown format %q (use md or json)", format)
	}
	r := &render.Renderer{P: p, Level: render.Normal}
	if o.brief {
		r.Level = render.Brief
	} else if o.full {
		r.Level = render.Full
	}

	notDocumented := func(target string, err error) error {
		msg := fmt.Sprintf("%s is not documented", target)
		if err != nil && err != render.ErrNotDocumented {
			msg = strings.TrimPrefix(err.Error(), render.ErrNotDocumented.Error()+": ")
		}
		if id == "" {
			msg += fmt.Sprintf(" (run `dirdoc export %s` for a template)", shellArg(target))
		}
		return exitf(exitNotDocumented, "%s", msg)
	}

	if o.recursive || cmd.Flags().Changed("depth") {
		if id != "" {
			return usagef(cmd, "-r cannot be used with PATH#ID")
		}
		if !isDir {
			return usagef(cmd, "-r needs a directory")
		}
		list := &render.ListView{DirdocView: render.ViewVersion, Kind: "list", Views: []*render.DirView{}}
		err := p.Ignore.Dirs(abs, o.depth, func(dir string, depth int) error {
			if !resolve.HasDirdoc(dir) {
				return nil
			}
			v, err := r.Dir(dir, false)
			if errors.Is(err, render.ErrNotDocumented) {
				return nil
			}
			if err != nil {
				return err
			}
			list.Views = append(list.Views, v)
			return nil
		})
		if err != nil {
			return exitf(exitError, "%v", err)
		}
		if len(list.Views) == 0 {
			return exitf(exitNotDocumented, "nothing under %s is documented", p.Rel(abs, true))
		}
		return a.output(format, list, func() string { return r.ListMarkdown(list) })
	}

	if id != "" {
		v, err := r.Entry(abs, isDir, id)
		if errors.Is(err, render.ErrNotDocumented) {
			return notDocumented(p.Rel(abs, isDir), err)
		}
		if err != nil {
			return exitf(exitError, "%v", err)
		}
		return a.output(format, v, func() string { return r.EntryMarkdown(v) })
	}
	if isDir {
		v, err := r.Dir(abs, true)
		if errors.Is(err, render.ErrNotDocumented) {
			return notDocumented(p.Rel(abs, true), nil)
		}
		if err != nil {
			return exitf(exitError, "%v", err)
		}
		return a.output(format, v, func() string { return r.DirMarkdown(v) })
	}
	v, err := r.File(abs)
	if errors.Is(err, render.ErrNotDocumented) {
		return notDocumented(p.Rel(abs, false), nil)
	}
	if err != nil {
		return exitf(exitError, "%v", err)
	}
	return a.output(format, v, func() string { return r.FileMarkdown(v) })
}

func (a *App) output(format string, v any, markdown func() string) error {
	if format == "json" {
		b, err := render.JSON(v)
		if err != nil {
			return exitf(exitError, "%v", err)
		}
		_, err = a.Stdout.Write(b)
		return err
	}
	_, err := fmt.Fprint(a.Stdout, markdown())
	return err
}

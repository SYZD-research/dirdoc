package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SYZD-research/dirdoc/internal/compile"
	"github.com/SYZD-research/dirdoc/internal/container"
	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/render"
	"github.com/SYZD-research/dirdoc/internal/resolve"
)

func (a *App) touchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "touch FILE...",
		Short: "Mark FILE's documentation as up to date without changing it (updates its target_hash)",
		Long: `Record the current content of each FILE in its PFD: the target_hash, and the line numbers and
signatures the overview takes from the code. The description stays as it is. Use it when the code
changed but its documentation is still accurate, so that it is no longer shown as stale.`,
		Args: usageArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			failed := false
			for _, arg := range args {
				if err := a.touch(arg); err != nil {
					fmt.Fprintln(a.Stderr, "dirdoc: "+err.Error())
					failed = true
				}
			}
			if failed {
				return &exitErr{code: exitError}
			}
			return nil
		},
	}
}

func (a *App) touch(arg string) error {
	abs := a.abs(arg)
	if resolve.InsideDirdoc(abs) {
		return fmt.Errorf("%s is inside a .dirdoc directory; touch the file it documents", arg)
	}
	st, err := os.Stat(abs)
	switch {
	case err != nil && exists(resolve.PFDFile(abs)):
		return fmt.Errorf("%s does not exist (remove its PFD with `dirdoc rm %s`)", arg, shellArg(arg))
	case err != nil:
		return fmt.Errorf("%s: no such file", arg)
	case st.IsDir():
		return fmt.Errorf("%s is a directory; only a file's PFD records a hash", arg)
	}
	p, err := resolve.Open(a.Cwd, abs)
	if err != nil {
		return err
	}
	rel := p.Rel(abs, false)
	pf, err := p.PFD(abs)
	if err != nil {
		return err
	}
	if pf == nil {
		return fmt.Errorf("%s has no PFD (run `dirdoc export %s` for a template)", rel, shellArg(rel))
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	compile.Generate(pf, content)
	js, err := model.Marshal(pf)
	if err != nil {
		return err
	}
	written, err := container.WriteFile(resolve.PFDFile(abs), js, p.Config.Compress(), container.Level(p.Config.Storage.Level))
	if err != nil {
		return err
	}
	if written {
		fmt.Fprintf(a.Stdout, "touched %s\n", rel)
	} else {
		fmt.Fprintf(a.Stdout, "unchanged %s (already up to date)\n", rel)
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Status states of directories and files.
const (
	stateDSD       = "dsd"       // the directory has its own DSD
	stateInherited = "inherited" // an ancestor's tree describes the directory (design §10.3)
	statePFD       = "pfd"       // the file has a PFD
	stateStale     = "stale"     // the file changed since its PFD was compiled
	stateOrphan    = "orphan"    // a PFD whose file no longer exists
	stateNote      = "note"      // an ancestor's tree has a note on the file
	stateNone      = "none"
)

var (
	dirStates  = []string{stateDSD, stateInherited, stateNone}
	fileStates = []string{statePFD, stateStale, stateOrphan, stateNote, stateNone}
)

type statusItem struct {
	Path  string `json:"path"`
	Type  string `json:"type"`
	State string `json:"state"`
}

type statusReport struct {
	DirdocView int          `json:"dirdoc_view"`
	Kind       string       `json:"kind"`
	Path       string       `json:"path"`
	Items      []statusItem `json:"items"`
	Counts     struct {
		Directories map[string]int `json:"directories"`
		Files       map[string]int `json:"files"`
	} `json:"counts"`
}

func (a *App) statusCmd() *cobra.Command {
	var only []string
	var depth int
	var format string
	c := &cobra.Command{
		Use:   "status [PATH]",
		Short: "List which directories and files below PATH are documented, stale or undocumented",
		Long: `List every directory and file below PATH (default: the whole project) that is not ignored, with
its state:

  dsd        the directory has its own DSD
  inherited  the directory is described in an ancestor's tree
  pfd        the file has a PFD
  stale      the file changed since its PFD was compiled
  orphan     a PFD whose file no longer exists
  note       the file has no PFD but a note in an ancestor's tree
  none       not documented`,
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "text" && format != "json" {
				return usagef(cmd, "unknown format %q (use text or json)", format)
			}
			want := map[string]bool{}
			for _, s := range only {
				if !contains(dirStates, s) && !contains(fileStates, s) {
					return usagef(cmd, "unknown state %q for --only (use %s)", s, strings.Join(append(append([]string{}, dirStates[:2]...), fileStates...), ", "))
				}
				want[s] = true
			}
			return a.status(args, want, depth, format)
		},
	}
	c.Flags().StringSliceVar(&only, "only", nil, "list only these states, e.g. --only stale,none")
	c.Flags().IntVar(&depth, "depth", -1, "how many levels of directories below PATH to list")
	c.Flags().StringVarP(&format, "format", "f", "text", "output format: text or json")
	return c
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (a *App) status(args []string, only map[string]bool, depth int, format string) error {
	start := a.Cwd
	if len(args) == 1 {
		start = a.abs(args[0])
		if resolve.InsideDirdoc(start) {
			return exitf(exitError, "%s is inside a .dirdoc directory", args[0])
		}
		if !exists(start) && !exists(resolve.PFDFile(start)) {
			return exitf(exitError, "%s: no such file or directory", args[0])
		}
	}
	p, err := a.openProject(start)
	if err != nil {
		return err
	}
	start = a.startPath(args, p)
	rep := statusReport{DirdocView: render.ViewVersion, Kind: "status", Items: []statusItem{}}
	rep.Counts.Directories, rep.Counts.Files = map[string]int{}, map[string]int{}
	for _, s := range dirStates {
		rep.Counts.Directories[s] = 0
	}
	for _, s := range fileStates {
		rep.Counts.Files[s] = 0
	}
	add := func(abs string, isDir bool, state string) {
		item := statusItem{Path: p.Rel(abs, isDir), Type: model.NodeFile, State: state}
		if isDir {
			item.Type = model.NodeDir
			rep.Counts.Directories[state]++
		} else {
			rep.Counts.Files[state]++
		}
		if len(only) == 0 || only[state] {
			rep.Items = append(rep.Items, item)
		}
	}
	fileState := func(file string) string {
		if !exists(resolve.PFDFile(file)) {
			if exists(file) && p.Described(file, false) != nil {
				return stateNote
			}
			return stateNone
		}
		if !exists(file) {
			return stateOrphan
		}
		if pf, err := p.PFD(file); err == nil {
			if s := p.Stale(file, pf); s != nil && *s {
				return stateStale
			}
		}
		return statePFD
	}

	if st, err := os.Stat(start); err != nil || !st.IsDir() {
		rep.Path = p.Rel(start, false)
		add(start, false, fileState(start))
	} else {
		rep.Path = p.Rel(start, true)
		err := p.Ignore.Dirs(start, depth, func(dir string, _ int) error {
			switch {
			case exists(resolve.DSDFile(dir)):
				add(dir, true, stateDSD)
			case p.Inherited(dir) != nil:
				add(dir, true, stateInherited)
			default:
				add(dir, true, stateNone)
			}
			names := map[string]bool{}
			if entries, err := p.Ignore.List(dir); err == nil {
				for _, e := range entries {
					if !e.IsDir {
						names[e.Name] = true
					}
				}
			}
			if l, err := resolve.List(dir); err == nil {
				for _, name := range l.PFDs {
					names[name] = true
				}
			}
			sorted := make([]string, 0, len(names))
			for name := range names {
				sorted = append(sorted, name)
			}
			sort.Strings(sorted)
			for _, name := range sorted {
				file := filepath.Join(dir, name)
				add(file, false, fileState(file))
			}
			return nil
		})
		if err != nil {
			return exitf(exitError, "%v", err)
		}
	}

	if format == "json" {
		b, err := render.JSON(rep)
		if err != nil {
			return exitf(exitError, "%v", err)
		}
		_, err = a.Stdout.Write(b)
		return err
	}
	for _, it := range rep.Items {
		fmt.Fprintf(a.Stdout, "%-9s  %s\n", it.State, it.Path)
	}
	counts := func(m map[string]int, states []string) string {
		var parts []string
		for _, s := range states {
			parts = append(parts, fmt.Sprintf("%d %s", m[s], s))
		}
		return strings.Join(parts, ", ")
	}
	fmt.Fprintf(a.Stdout, "directories: %s; files: %s\n", counts(rep.Counts.Directories, dirStates), counts(rep.Counts.Files, fileStates))
	return nil
}

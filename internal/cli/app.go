// Package cli implements the dirdoc command line.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SYZD-research/dirdoc/internal/diag"
	"github.com/SYZD-research/dirdoc/internal/resolve"
)

// Version is the dirdoc version.
const Version = "0.2.0"

// Exit codes (design §14).
const (
	exitOK            = 0
	exitError         = 1
	exitUsage         = 2
	exitNotDocumented = 3
)

// App is one invocation of the command line, with its own I/O and working directory.
type App struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Cwd    string
	Getenv func(string) string
}

// exitErr ends the command with a code. A nil err means the details were already printed.
type exitErr struct {
	code int
	err  error
}

func (e *exitErr) Error() string {
	if e.err == nil {
		return fmt.Sprintf("exit status %d", e.code)
	}
	return e.err.Error()
}

func exitf(code int, format string, args ...any) error {
	return &exitErr{code: code, err: fmt.Errorf(format, args...)}
}

func usagef(cmd *cobra.Command, format string, args ...any) error {
	return &exitErr{code: exitUsage, err: fmt.Errorf("%s\nRun '%s --help' for usage.", fmt.Sprintf(format, args...), cmd.CommandPath())}
}

func usageArgs(fn cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := fn(cmd, args); err != nil {
			return usagef(cmd, "%v", err)
		}
		return nil
	}
}

// Run executes the command line and returns the exit code.
func (a *App) Run(args []string) int {
	if a.Getenv == nil {
		a.Getenv = os.Getenv
	}
	root := a.rootCmd()
	if args == nil {
		args = []string{} // cobra reads os.Args when given nil
	}
	root.SetArgs(args)
	root.SetIn(a.Stdin)
	root.SetOut(a.Stdout)
	root.SetErr(a.Stderr)
	err := root.Execute()
	if err == nil {
		return exitOK
	}
	var ee *exitErr
	if errors.As(err, &ee) {
		if ee.err != nil {
			fmt.Fprintln(a.Stderr, "dirdoc: "+ee.err.Error())
		}
		return ee.code
	}
	fmt.Fprintln(a.Stderr, "dirdoc: "+err.Error())
	return exitError
}

func (a *App) rootCmd() *cobra.Command {
	var opts showOptions
	root := &cobra.Command{
		Use:   "dirdoc [PATH[#ID]]",
		Short: "Read and write directory and file documentation for AI agents",
		Long: `dirdoc keeps a description of each directory (DSD) and file (PFD) in .dirdoc/ directories
and prints it as Markdown or JSON.

  dirdoc [PATH]          show the documentation of a directory or file (default: .)
  dirdoc 'FILE#ID'       show one entry (type, function, variable or dsd entry)
  dirdoc export TARGET   print the editable TOML source (a template when there is none)
  dirdoc compile TARGET SOURCE|-
                         validate a source and store it
  dirdoc status          list what is documented, stale or undocumented
  dirdoc touch FILE      mark a changed file's documentation as still accurate

Run 'dirdoc guide' for the workflow to put in AGENTS.md or CLAUDE.md.`,
		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.show(cmd, args, &opts)
		},
	}
	opts.register(root)
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return usagef(cmd, "%v", err) })
	root.CompletionOptions.DisableDefaultCmd = true

	var showOpts showOptions
	show := &cobra.Command{
		Use:   "show [PATH[#ID]]",
		Short: "Show documentation (same as dirdoc PATH; use it for paths named like a subcommand)",
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.show(cmd, args, &showOpts)
		},
	}
	showOpts.register(show)

	root.AddCommand(show, a.compileCmd(), a.exportCmd(), a.editCmd(), a.checkCmd(), a.searchCmd(), a.statusCmd(),
		a.touchCmd(), a.rmCmd(), a.mvCmd(), a.catCmd(), a.initCmd(), a.guideCmd(), a.mergeDriverCmd(), a.mcpCmd())
	return root
}

// abs resolves a command-line path against the working directory.
func (a *App) abs(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(a.Cwd, p)
}

func (a *App) printDiags(w io.Writer, l diag.List) {
	for _, d := range l {
		fmt.Fprintln(w, d.String())
	}
}

// openProject opens the project containing abs.
func (a *App) openProject(abs string) (*resolve.Project, error) {
	p, err := resolve.Open(a.Cwd, abs)
	if err != nil {
		return nil, exitf(exitError, "%v", err)
	}
	return p, nil
}

// shellArg quotes s for display in a suggested command when it contains special characters.
func shellArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t'\"#$&*;|<>()[]{}?!~`\\") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

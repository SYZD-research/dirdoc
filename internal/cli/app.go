// Package cli implements the dirdoc command line.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SYZD-research/dirdoc/internal/diag"
	"github.com/SYZD-research/dirdoc/internal/render"
	"github.com/SYZD-research/dirdoc/internal/resolve"
)

// Version is the dirdoc version. Release builds set it with
// -ldflags "-X github.com/SYZD-research/dirdoc/internal/cli.Version=1.2.0"; when it is empty,
// version reads the module version that go install and go build record.
var Version string

// version returns Version, or else the recorded module version without its "v" (1.2.0 after
// go install ...@v1.2.0, a pseudo-version for a build between tags), or else "devel".
func version() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && strings.HasPrefix(info.Main.Version, "v") {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return "devel"
}

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
	// LookupEnv tells an unset variable from an empty one; when it is nil, an empty Getenv value
	// counts as unset.
	LookupEnv func(string) (string, bool)
	// Terminal reports whether w is a terminal; when it is nil, w must be an *os.File on one.
	Terminal func(w io.Writer) bool

	noPager   bool   // --no-pager
	colorMode string // --color
	color     bool   // the command colors its output (design §11.3)
	pager     *pager // the output a pager will show when the command ends
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
	if a.LookupEnv == nil {
		getenv := a.Getenv
		a.LookupEnv = func(k string) (string, bool) {
			v := getenv(k)
			return v, v != ""
		}
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
	a.endPager() // before the error, which then shows below the output
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
		Version:       version(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.show(cmd, args, &opts)
		},
	}
	opts.register(root)
	root.PersistentFlags().BoolVar(&a.noPager, "no-pager", false, "do not page the output (on a terminal, reading commands use $DIRDOC_PAGER, $PAGER or less)")
	root.PersistentFlags().StringVar(&a.colorMode, "color", "auto", "color the output: auto (on a terminal), always or never")
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		switch a.colorMode {
		case "auto", "always", "never":
			return nil
		}
		return usagef(cmd, "unknown --color %q (use auto, always or never)", a.colorMode)
	}
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

	a.reading(root, true)
	root.AddCommand(a.reading(show, true), a.compileCmd(), a.reading(a.exportCmd(), false), a.editCmd(),
		a.reading(a.checkCmd(), true), a.reading(a.searchCmd(), true), a.reading(a.statusCmd(), true),
		a.touchCmd(), a.rmCmd(), a.mvCmd(), a.convertCmd(), a.reading(a.catCmd(), false), a.initCmd(),
		a.reading(a.guideCmd(), true), a.mergeDriverCmd(), a.mcpCmd())
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
		line := d.String()
		if a.color && w == a.Stdout {
			p := render.NewPainter(line)
			sev := render.StyleWarning
			if d.Severity == diag.Error {
				sev = render.StyleError
			}
			p.Paint(0, len(d.Severity.String()), sev)
			p.Refs()
			line = p.String()
		}
		fmt.Fprintln(w, line)
	}
}

// count prints n things, in style st when the output is colored and there are any.
func (a *App) count(n int, word string, st render.Style) string {
	s := plural(n, word)
	if a.color && n > 0 {
		s = render.Paint(s, st)
	}
	return s
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

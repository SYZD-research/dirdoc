package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Paging and color for people at a terminal (design §11.3). Output that goes anywhere else (a pipe,
// a file, an MCP client) stays exactly as it is.

// reading marks a command whose output is for reading: at a terminal it goes through a pager, and
// when color is true it is colored.
func (a *App) reading(c *cobra.Command, color bool) *cobra.Command {
	run := c.RunE
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if color {
			a.color = a.colorful()
		}
		a.startPager()
		return run(cmd, args)
	}
	return c
}

// terminal reports whether w is a terminal.
func (a *App) terminal(w io.Writer) bool {
	if a.Terminal != nil {
		return a.Terminal(w)
	}
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// dumbTerminal reports a terminal that shows neither a pager nor colors: TERM=dumb, or no TERM at
// all outside Windows.
func (a *App) dumbTerminal() bool {
	t, _ := a.LookupEnv("TERM")
	return t == "dumb" || t == "" && runtime.GOOS != "windows"
}

// colorful applies --color to the standard output of this command.
func (a *App) colorful() bool {
	switch a.colorMode {
	case "always":
		return true
	case "never":
		return false
	}
	if v, _ := a.LookupEnv("NO_COLOR"); v != "" || a.dumbTerminal() || !a.terminal(a.Stdout) {
		return false
	}
	f, ok := a.Stdout.(*os.File)
	return !ok || enableVT(f)
}

// pager holds the output of a reading command until the command ends.
type pager struct {
	command string    // run by the shell, as the editor is
	out     io.Writer // the terminal
	buf     bytes.Buffer
}

// startPager collects the rest of the command's standard output for a pager when it goes to a
// terminal.
func (a *App) startPager() {
	if a.noPager || a.pager != nil || !a.terminal(a.Stdout) || a.dumbTerminal() {
		return
	}
	command := a.pagerCommand()
	if command == "" {
		return
	}
	a.pager = &pager{command: command, out: a.Stdout}
	a.Stdout = &a.pager.buf
}

// pagerCommand returns $DIRDOC_PAGER, else $PAGER, else less when it is installed. "" means no
// pager, also for a variable set to "" or "cat" (as git does).
func (a *App) pagerCommand() string {
	for _, k := range []string{"DIRDOC_PAGER", "PAGER"} {
		if v, ok := a.LookupEnv(k); ok {
			if v = strings.TrimSpace(v); v == "cat" {
				return ""
			}
			return v
		}
	}
	if _, err := exec.LookPath("less"); err != nil {
		return ""
	}
	return "less -R" // -R shows the colors even when $LESS does not ask for them
}

// endPager shows what a paged command printed: through the pager, or directly when the pager cannot
// be started. Nothing is shown for an empty output, such as that of a command that failed.
func (a *App) endPager() {
	p := a.pager
	if p == nil {
		return
	}
	a.pager, a.Stdout = nil, p.out
	if p.buf.Len() == 0 {
		return
	}
	if err := a.runPager(p); err != nil {
		fmt.Fprintf(a.Stderr, "dirdoc: warning: cannot run the pager %q: %v\n", p.command, err)
		a.Stdout.Write(p.buf.Bytes())
	}
}

// pagerEnv holds what the pager gets unless the variable is set: less shows a short output as it is
// and leaves it on the screen (F, X) and passes colors (R), as git sets it; lv passes colors; the
// output is UTF-8 whatever the locale.
var pagerEnv = [][2]string{{"LESS", "FRX"}, {"LV", "-c"}, {"LESSCHARSET", "utf-8"}}

// shellChars make a pager command run through the shell; without them it runs directly, so that a
// missing pager is found out (as git does).
const shellChars = "|&;<>()$`\\\"'\t\n*?[#~=%"

// runPager runs the pager on the output and waits for it. It fails when the pager cannot be started:
// a command that does not exist, or one the shell cannot find or run.
func (a *App) runPager(p *pager) error {
	var cmd *exec.Cmd
	shell := runtime.GOOS != "windows" && strings.ContainsAny(p.command, shellChars)
	if shell {
		cmd = exec.Command("sh", "-c", p.command)
	} else {
		fields := strings.Fields(p.command)
		cmd = exec.Command(fields[0], fields[1:]...)
	}
	cmd.Stdin = bytes.NewReader(p.buf.Bytes())
	cmd.Stdout, cmd.Stderr, cmd.Dir = p.out, a.Stderr, a.Cwd
	cmd.Env = os.Environ()
	for _, kv := range pagerEnv {
		if _, ok := a.LookupEnv(kv[0]); ok || kv[0] == "LESSCHARSET" && a.isSet("LESSCHARDEF") {
			continue
		}
		cmd.Env = append(cmd.Env, kv[0]+"="+kv[1])
	}
	// Ctrl-C is for the pager. dirdoc waits for it, as an exit would leave the terminal to a pager
	// whose parent is gone.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Quitting the pager before the end is no error, but the shell's "not found" (127) and "cannot
	// run" (126) mean nothing was shown.
	var exit *exec.ExitError
	if err := cmd.Wait(); shell && errors.As(err, &exit) && (exit.ExitCode() == 127 || exit.ExitCode() == 126) {
		return err
	}
	return nil
}

func (a *App) isSet(key string) bool {
	_, ok := a.LookupEnv(key)
	return ok
}

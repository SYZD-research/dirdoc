package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

func (a *App) editCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit TARGET",
		Short: "Edit TARGET's documentation in $EDITOR, then validate and store it (for people)",
		Long: `Export TARGET's documentation (a template when it has none) to a temporary file, open it in
$VISUAL or $EDITOR (vi when neither is set) and compile it when the editor exits. When the source
has errors, dirdoc shows them and asks whether to edit it again.`,
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.edit(args[0])
		},
	}
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (a *App) edit(target string) error {
	original, err := a.exportText(target, "toml", 1)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "dirdoc-"+unsafeName.ReplaceAllString(filepath.Base(a.abs(target)), "_")+"-*.toml")
	if err != nil {
		return exitf(exitError, "%v", err)
	}
	path := f.Name()
	_, err = f.WriteString(original)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return exitf(exitError, "%v", err)
	}
	answers := bufio.NewReader(a.Stdin)
	for {
		if err := a.runEditor(path); err != nil {
			return exitf(exitError, "%v; nothing written (your edits are in %s)", err, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return exitf(exitError, "%v", err)
		}
		if string(data) == original {
			os.Remove(path)
			fmt.Fprintln(a.Stdout, "no changes; nothing written")
			return nil
		}
		err = a.compile(target, path, false, false, false)
		if err == nil {
			os.Remove(path)
			return nil
		}
		var ee *exitErr
		if errors.As(err, &ee) && ee.err != nil {
			fmt.Fprintln(a.Stderr, "dirdoc: "+ee.err.Error())
		}
		fmt.Fprint(a.Stderr, "Edit again? [Y/n] ")
		line, rerr := answers.ReadString('\n')
		if answer := strings.ToLower(strings.TrimSpace(line)); answer == "n" || answer == "no" || rerr != nil && line == "" {
			if rerr != nil {
				fmt.Fprintln(a.Stderr)
			}
			return exitf(exitError, "nothing written; your edits are in %s", path)
		}
	}
}

// runEditor opens path in the user's editor and waits for it to exit. The editor setting may carry
// arguments ("code --wait"), so it runs through the shell as git does.
func (a *App) runEditor(path string) error {
	editor := a.Getenv("VISUAL")
	if editor == "" {
		editor = a.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		fields := strings.Fields(editor)
		cmd = exec.Command(fields[0], append(fields[1:], path)...)
	} else {
		cmd = exec.Command("sh", "-c", editor+` "$@"`, editor, path)
	}
	// Only a terminal is passed on: copying another reader would swallow the answers to our prompt.
	if in, ok := a.Stdin.(*os.File); ok {
		cmd.Stdin = in
	}
	cmd.Stdout, cmd.Stderr, cmd.Dir = a.Stdout, a.Stderr, a.Cwd
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %q failed: %v", editor, err)
	}
	return nil
}

package cli

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SYZD-research/dirdoc/internal/config"
	"github.com/SYZD-research/dirdoc/internal/container"
	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/source"
)

func (a *App) catCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cat FILE|-",
		Short: "Print a stored .dirdoc_* file as TOML (used as the git diff textconv)",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			var data []byte
			var err error
			if args[0] == "-" {
				data, err = io.ReadAll(a.Stdin)
			} else {
				data, err = os.ReadFile(a.abs(args[0]))
			}
			if err != nil {
				return exitf(exitError, "%v", err)
			}
			js, _, err := container.Decode(data)
			if errors.Is(err, container.ErrUnknownFormat) && bytes.HasPrefix(data, []byte("# dirdoc ")) {
				_, err = a.Stdout.Write(data) // a TOML source the merge driver left; show it as it is
				return err
			}
			if err != nil {
				return exitf(exitError, "%s: %v", args[0], err)
			}
			kind, d, pf, err := model.Decode(js)
			if err != nil {
				return exitf(exitError, "%s: %v", args[0], err)
			}
			if kind == model.KindDSD {
				_, err = fmt.Fprint(a.Stdout, source.EmitDSD(d, "dirdoc dsd"))
			} else {
				header := "dirdoc pfd: " + pf.Target
				if pf.TargetHash != "" {
					header += "\ntarget_hash: " + pf.TargetHash // so that git diff shows dirdoc touch
				}
				_, err = fmt.Fprint(a.Stdout, source.EmitPFD(pf, header))
			}
			return err
		},
	}
}

var gitattributesLines = []string{
	".dirdoc_dsd   -text merge=dirdoc diff=dirdoc",
	"*.dirdoc_pfd  -text merge=dirdoc diff=dirdoc",
}

// phase1Lines are the .gitattributes lines dirdoc init --git wrote before the merge driver existed.
var phase1Lines = []string{
	".dirdoc_dsd   -text -merge diff=dirdoc",
	"*.dirdoc_pfd  -text -merge diff=dirdoc",
}

// gitConfig is what dirdoc init --git sets in the clone's git config.
var gitConfig = [][2]string{
	{"diff.dirdoc.textconv", "dirdoc cat"},
	{"diff.dirdoc.cachetextconv", "true"},
	{"merge.dirdoc.name", "dirdoc: merge .dirdoc_* files by entry id"},
	{"merge.dirdoc.driver", "dirdoc merge-driver %O %A %B %P"},
}

func (a *App) initCmd() *cobra.Command {
	var git bool
	c := &cobra.Command{
		Use:   "init",
		Short: "Create dirdoc.toml in the current directory (with --git, also set up git diff for .dirdoc_* files)",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.init(git)
		},
	}
	c.Flags().BoolVar(&git, "git", false, "add .gitattributes entries and configure the dirdoc diff and merge drivers for this clone")
	return c
}

func (a *App) init(git bool) error {
	cfgPath := filepath.Join(a.Cwd, config.FileName)
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Fprintf(a.Stdout, "%s already exists; left unchanged\n", config.FileName)
	} else {
		if err := os.WriteFile(cfgPath, []byte(config.Template), 0o644); err != nil {
			return exitf(exitError, "%v", err)
		}
		fmt.Fprintf(a.Stdout, "created %s\n", config.FileName)
	}
	if !git {
		return nil
	}

	attrPath := filepath.Join(a.Cwd, ".gitattributes")
	existing, err := os.ReadFile(attrPath)
	if err != nil && !os.IsNotExist(err) {
		return exitf(exitError, "%v", err)
	}
	normalize := func(line string) string { return strings.Join(strings.Fields(line), " ") }
	// Lines from before the merge driver are replaced where they are.
	old := map[string]string{}
	for i, line := range phase1Lines {
		old[normalize(line)] = gitattributesLines[i]
	}
	lines := strings.Split(string(existing), "\n")
	have, replaced := map[string]bool{}, false
	for i, line := range lines {
		if repl, ok := old[normalize(line)]; ok {
			lines[i], replaced = repl, true
		}
		have[normalize(lines[i])] = true
	}
	var add []string
	for _, line := range gitattributesLines {
		if !have[normalize(line)] {
			add = append(add, line)
		}
	}
	if len(add) > 0 || replaced {
		var b bytes.Buffer
		b.WriteString(strings.Join(lines, "\n"))
		if len(add) > 0 {
			if b.Len() > 0 && !bytes.HasSuffix(b.Bytes(), []byte("\n")) {
				b.WriteByte('\n')
			}
			b.WriteString("# dirdoc: show .dirdoc_* files as TOML in git diff, and merge them by entry id\n")
			for _, line := range add {
				b.WriteString(line + "\n")
			}
		}
		if err := os.WriteFile(attrPath, b.Bytes(), 0o644); err != nil {
			return exitf(exitError, "%v", err)
		}
		fmt.Fprintln(a.Stdout, "updated .gitattributes")
	} else {
		fmt.Fprintln(a.Stdout, ".gitattributes already has the dirdoc entries")
	}

	for _, kv := range gitConfig {
		cmd := exec.Command("git", "config", kv[0], kv[1])
		cmd.Dir = a.Cwd
		if out, err := cmd.CombinedOutput(); err != nil {
			var manual []string
			for _, kv := range gitConfig {
				manual = append(manual, fmt.Sprintf("  git config %s %s", kv[0], shellArg(kv[1])))
			}
			return exitf(exitError, "git config %s failed: %v %s\nRun these in the repository yourself:\n%s",
				kv[0], err, strings.TrimSpace(string(out)), strings.Join(manual, "\n"))
		}
	}
	fmt.Fprintln(a.Stdout, `configured git for this clone: diff.dirdoc.textconv = "dirdoc cat", `+
		`merge.dirdoc.driver = "dirdoc merge-driver %O %A %B %P" (every clone needs "dirdoc init --git")`)
	return nil
}

//go:embed guide_ja.md
var guideJA string

//go:embed guide_en.md
var guideEN string

func (a *App) guideCmd() *cobra.Command {
	var lang string
	c := &cobra.Command{
		Use:   "guide",
		Short: "Print the dirdoc workflow for AI agents, to put in AGENTS.md or CLAUDE.md",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			if lang == "" {
				lang = "en"
				if strings.HasPrefix(a.Getenv("LANG"), "ja") {
					lang = "ja"
				}
			}
			switch lang {
			case "ja":
				fmt.Fprint(a.Stdout, guideJA)
			case "en":
				fmt.Fprint(a.Stdout, guideEN)
			default:
				return usagef(cmd, "unknown language %q (use ja or en)", lang)
			}
			return nil
		},
	}
	c.Flags().StringVar(&lang, "lang", "", "ja or en (default: ja when $LANG starts with ja, else en)")
	return c
}

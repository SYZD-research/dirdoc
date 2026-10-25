package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/SYZD-research/dirdoc/internal/diag"
	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/render"
	"github.com/SYZD-research/dirdoc/internal/resolve"
)

func (a *App) checkCmd() *cobra.Command {
	var strict bool
	var format string
	c := &cobra.Command{
		Use:   "check [PATH]",
		Short: "Validate the documentation below PATH (default: the whole project)",
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "text" && format != "json" {
				return usagef(cmd, "unknown format %q (use text or json)", format)
			}
			return a.check(args, strict, format)
		},
	}
	c.Flags().BoolVar(&strict, "strict", false, "treat warnings as errors (for CI)")
	c.Flags().StringVarP(&format, "format", "f", "text", "output format: text or json")
	return c
}

type checkReport struct {
	DirdocView  int       `json:"dirdoc_view"`
	Kind        string    `json:"kind"`
	Documents   int       `json:"documents"`
	Errors      int       `json:"errors"`
	Warnings    int       `json:"warnings"`
	Diagnostics diag.List `json:"diagnostics"`
}

// eachDocument calls the callbacks for every stored document below start (a directory) or, when
// start is a file, for its PFD only. It returns false when start is a file without a PFD.
func eachDocument(p *resolve.Project, start string, onDSD func(dir string), onPFD func(file string), onUnknown func(path string)) bool {
	if st, err := os.Stat(start); err == nil && st.IsDir() {
		p.Ignore.Dirs(start, -1, func(dir string, _ int) error {
			l, err := resolve.List(dir)
			if err != nil || !l.Exists {
				return nil
			}
			if l.HasDSD {
				onDSD(dir)
			}
			for _, name := range l.PFDs {
				onPFD(filepath.Join(dir, name))
			}
			for _, u := range l.Unknown {
				onUnknown(filepath.Join(dir, model.DirName, u))
			}
			return nil
		})
		return true
	}
	if _, err := os.Stat(resolve.PFDFile(start)); err != nil {
		return false
	}
	onPFD(start)
	return true
}

func (a *App) startPath(args []string, p *resolve.Project) string {
	if len(args) > 0 {
		return a.abs(args[len(args)-1])
	}
	return p.Root
}

func (a *App) check(args []string, strict bool, format string) error {
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
	all := diag.List{}
	docs := 0
	val := diag.Validator{P: p, FS: true}
	found := eachDocument(p, start,
		func(dir string) {
			docs++
			d, err := p.DSD(dir)
			if err != nil {
				all = append(all, diag.LoadError(p.Rel(resolve.DSDFile(dir), false), err))
				return
			}
			all = append(all, val.DSD(dir, d)...)
		},
		func(file string) {
			docs++
			pf, err := p.PFD(file)
			if err != nil {
				all = append(all, diag.LoadError(p.Rel(resolve.PFDFile(file), false), err))
				return
			}
			all = append(all, val.PFD(file, pf)...)
		},
		func(path string) {
			all.Warnf(diag.CodeUnknownFile, p.Rel(path, false), "unknown file in .dirdoc/ (expected .dirdoc_dsd or <file>.dirdoc_pfd)")
		})
	if !found {
		fmt.Fprintf(a.Stdout, "%s has no PFD; nothing to check\n", p.Rel(start, false))
		return nil
	}
	e, w := all.Count()
	if format == "json" {
		b, err := render.JSON(checkReport{DirdocView: render.ViewVersion, Kind: "check", Documents: docs, Errors: e, Warnings: w, Diagnostics: all})
		if err != nil {
			return exitf(exitError, "%v", err)
		}
		a.Stdout.Write(b)
	} else {
		a.printDiags(a.Stdout, all)
		fmt.Fprintf(a.Stdout, "checked %s: %s, %s\n", plural(docs, "document"), plural(e, "error"), plural(w, "warning"))
	}
	if all.Failed(strict) {
		return &exitErr{code: exitError}
	}
	return nil
}

func (a *App) searchCmd() *cobra.Command {
	var regex bool
	var in []string
	var format string
	var limit int
	c := &cobra.Command{
		Use:   "search QUERY [PATH]",
		Short: "Search the documentation below PATH (the stored files are compressed, so grep cannot)",
		Long: `Search names, summaries, documents and tree notes below PATH (default: the whole project).
The search ignores case unless QUERY contains an upper-case letter.`,
		Args: usageArgs(cobra.RangeArgs(1, 2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "text" && format != "json" {
				return usagef(cmd, "unknown format %q (use text or json)", format)
			}
			fields := map[string]bool{}
			for _, f := range in {
				switch f {
				case "name", "summary", "document", "note":
					fields[f] = true
				default:
					return usagef(cmd, "unknown field %q for --in (use name, summary, document, note)", f)
				}
			}
			pattern := args[0]
			if !regex {
				pattern = regexp.QuoteMeta(pattern)
			}
			if !hasUpper(args[0]) {
				pattern = "(?i)" + pattern
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return usagef(cmd, "invalid regular expression: %v", err)
			}
			return a.search(args, re, fields, format, limit)
		},
	}
	c.Flags().BoolVar(&regex, "regex", false, "QUERY is a regular expression (RE2 syntax)")
	c.Flags().StringSliceVar(&in, "in", []string{"name", "summary", "document", "note"}, "fields to search")
	c.Flags().StringVarP(&format, "format", "f", "text", "output format: text or json")
	c.Flags().IntVar(&limit, "limit", 50, "maximum number of results to print (0 for no limit)")
	return c
}

type searchResult struct {
	Ref     string `json:"ref"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Summary string `json:"summary,omitempty"`
	Field   string `json:"field"`
	Snippet string `json:"snippet"`
}

type searchReport struct {
	DirdocView int            `json:"dirdoc_view"`
	Kind       string         `json:"kind"`
	Query      string         `json:"query"`
	Total      int            `json:"total"`
	Results    []searchResult `json:"results"`
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// snippet returns the line around a match, shortened with "…" on long lines.
func snippet(text string, start, end int) string {
	ls := strings.LastIndexByte(text[:start], '\n') + 1
	le := len(text)
	if i := strings.IndexByte(text[end:], '\n'); i >= 0 {
		le = end + i
	}
	pre, match, post := text[ls:start], text[start:end], text[end:le]
	if n := utf8.RuneCountInString(pre); n > 40 {
		r := []rune(pre)
		pre = "…" + string(r[n-40:])
	}
	if n := utf8.RuneCountInString(post); n > 60 {
		post = string([]rune(post)[:60]) + "…"
	}
	return strings.TrimSpace(pre + match + post)
}

var searchKinds = map[string]string{
	"directory": "dir", "file": "file", "type": "type", "function": "fn", "variable": "var",
	"dsd-entry": "dsd", "tree-node": "tree",
}

func (a *App) search(args []string, re *regexp.Regexp, fields map[string]bool, format string, limit int) error {
	start := a.Cwd
	if len(args) == 2 {
		start = a.abs(args[1])
		if _, err := os.Stat(start); err != nil {
			return exitf(exitError, "%s: no such file or directory", args[1])
		}
	}
	p, err := a.openProject(start)
	if err != nil {
		return err
	}
	start = a.startPath(args[1:], p)
	results := []searchResult{}
	add := func(ref, kind, name, summary string, texts ...[2]string) {
		for _, t := range texts {
			if !fields[t[0]] {
				continue
			}
			if loc := re.FindStringIndex(t[1]); loc != nil {
				results = append(results, searchResult{
					Ref: ref, Kind: kind, Name: name, Summary: firstLine(summary),
					Field: t[0], Snippet: snippet(t[1], loc[0], loc[1]),
				})
				return
			}
		}
	}
	eachDocument(p, start,
		func(dir string) {
			d, err := p.DSD(dir)
			if err != nil {
				fmt.Fprintln(a.Stderr, diag.LoadError(p.Rel(resolve.DSDFile(dir), false), err).String())
				return
			}
			owner := resolve.DirOwner(dir)
			ref := p.Ref(owner)
			add(ref, "directory", ref, d.Summary, [2]string{"summary", d.Summary})
			for _, e := range d.Entries {
				name := d.DisplayName(e.ID, filepath.Base(dir))
				add(p.EntryRef(owner, e.ID), "dsd-entry", name, "", [2]string{"name", name}, [2]string{"document", e.Document})
			}
			if d.Tree != nil {
				d.Tree.Walk(func(n *model.TreeNode, rel string) {
					if rel == "" || n.Note == "" {
						return
					}
					name := n.Name
					if n.IsDir() {
						name += "/"
					}
					add(p.Rel(filepath.Join(dir, filepath.FromSlash(rel)), n.IsDir()), "tree-node", name, n.Note, [2]string{"note", n.Note})
				})
			}
		},
		func(file string) {
			pf, err := p.PFD(file)
			if err != nil {
				fmt.Fprintln(a.Stderr, diag.LoadError(p.Rel(resolve.PFDFile(file), false), err).String())
				return
			}
			owner := resolve.FileOwner(file)
			ref := p.Ref(owner)
			add(ref, "file", ref, pf.Summary, [2]string{"summary", pf.Summary})
			for _, sec := range model.PFDSections {
				for _, e := range pf.Section(sec) {
					add(p.EntryRef(owner, e.ID), model.SectionKind(sec), e.Name, e.Summary,
						[2]string{"name", e.Name}, [2]string{"summary", e.Summary}, [2]string{"document", e.Document})
				}
			}
		},
		func(string) {})

	total := len(results)
	if limit > 0 && total > limit {
		results = results[:limit]
	}
	if format == "json" {
		b, err := render.JSON(searchReport{DirdocView: render.ViewVersion, Kind: "search", Query: args[0], Total: total, Results: results})
		if err != nil {
			return exitf(exitError, "%v", err)
		}
		_, err = a.Stdout.Write(b)
		return err
	}
	if total == 0 {
		fmt.Fprintln(a.Stdout, "no matches")
		return nil
	}
	for _, r := range results {
		line := r.Ref + "  " + searchKinds[r.Kind]
		if r.Name != r.Ref {
			line += " " + r.Name
		}
		if r.Summary != "" {
			line += " — " + r.Summary
		}
		fmt.Fprintln(a.Stdout, line)
		if r.Field == "document" {
			fmt.Fprintln(a.Stdout, "    "+r.Snippet)
		}
	}
	if total > len(results) {
		fmt.Fprintf(a.Stdout, "… %d more (raise --limit to see them)\n", total-len(results))
	}
	return nil
}

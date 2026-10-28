package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SYZD-research/dirdoc/internal/mcp"
)

func (a *App) mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve the documentation to AI agents over MCP (Model Context Protocol) on stdin/stdout",
		Long: `Run a Model Context Protocol server on standard input and output. Its tools (show, search,
status, check, export, compile, touch, rm, mv) run the dirdoc commands of the same names in the
directory the server was started in, and only accept paths inside that project.

Register it with an MCP client, for example: claude mcp add dirdoc -- dirdoc mcp`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.mcpServer()
			if err != nil {
				return err
			}
			return s.Serve(a.Stdin, a.Stdout)
		},
	}
}

const mcpInstructions = `dirdoc keeps short descriptions of this project's directories and files. Read them before opening code: show a directory for its summary, tree and the table of contents of each file; show a file for its details; show "file#id" for one type or function; search finds words in all descriptions. [[...]] in the output is a reference: pass what is inside to show.
When you change code, update its description: compile with merge=true and only the changed entries, or export, edit and compile the whole source. A view that starts with "stale:" means the file changed after its description was written: check the code, update the description, or run touch when it is still accurate. Use mv to rename documented files and rm to remove descriptions. Finish with check.`

// mcpArgs decodes the arguments of a tool call, refusing unknown ones.
func mcpArgs(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid arguments: %v", err)
	}
	return nil
}

func (a *App) mcpServer() (*mcp.Server, error) {
	p, err := a.openProject(a.Cwd)
	if err != nil {
		return nil, err
	}
	// inside checks that a path argument (PATH or PATH#ID) stays in the project.
	inside := func(args ...string) error {
		for _, arg := range args {
			pathArg, _ := a.splitPathID(arg)
			if !p.Within(a.abs(pathArg)) {
				return fmt.Errorf("%s is outside the project %s", arg, p.Root)
			}
		}
		return nil
	}
	// run runs the dirdoc command name with its flags and positional arguments, which go after "--"
	// so that none is taken for a flag. The paths among them must stay in the project.
	run := func(stdin, name string, flags, positional, paths []string) (string, bool) {
		if err := inside(paths...); err != nil {
			return err.Error(), true
		}
		var out, errb bytes.Buffer
		sub := &App{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errb, Cwd: a.Cwd, Getenv: a.Getenv}
		args := append(append([]string{name}, flags...), "--")
		code := sub.Run(append(args, positional...))
		text := out.String()
		if e := errb.String(); e != "" {
			if text != "" && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			text += e
		}
		if text == "" {
			text = fmt.Sprintf("dirdoc %s finished with exit status %d", name, code)
		}
		return text, code != exitOK
	}
	optional := func(path string) []string {
		if path == "" {
			return nil
		}
		return []string{path}
	}
	flag := func(flags *[]string, on bool, name string) {
		if on {
			*flags = append(*flags, name)
		}
	}
	value := func(flags *[]string, name, v string) {
		if v != "" {
			*flags = append(*flags, name, v)
		}
	}
	number := func(flags *[]string, name string, n *int) {
		if n != nil {
			*flags = append(*flags, name, strconv.Itoa(*n))
		}
	}
	fail := func(err error) (string, bool) { return err.Error(), true }

	tools := []mcp.Tool{
		{
			Name: "show", Title: "Show documentation", ReadOnly: true, Idempotent: true,
			Description: `Show the documentation of a directory (summary, tree, the table of contents of each file), of a file (summary, entries with their documents, symbols without entries, places that refer to it), or of one entry with "path#id". Paths are relative to the project directory; "." is the project root.`,
			InputSchema: json.RawMessage(`{"type":"object","properties":{
"path":{"type":"string","description":"A directory or file, or \"file#id\" / \"dir/#id\" for one entry. Default \".\"."},
"level":{"type":"string","enum":["brief","normal","full"],"description":"brief: summaries and one line per file; full: also every document of a directory's files. Default normal."},
"recursive":{"type":"boolean","description":"Also show the subdirectories that have their own documentation."},
"depth":{"type":"integer","minimum":0,"description":"With recursive, how many levels below path to show."},
"format":{"type":"string","enum":["md","json"],"description":"Default md (Markdown)."}},"additionalProperties":false}`),
			Call: func(raw json.RawMessage) (string, bool) {
				var in struct {
					Path      string `json:"path"`
					Level     string `json:"level"`
					Recursive bool   `json:"recursive"`
					Depth     *int   `json:"depth"`
					Format    string `json:"format"`
				}
				if err := mcpArgs(raw, &in); err != nil {
					return fail(err)
				}
				if in.Path == "" {
					in.Path = "."
				}
				var flags []string
				switch in.Level {
				case "", "normal":
				case "brief", "full":
					flags = append(flags, "--"+in.Level)
				default:
					return fail(fmt.Errorf("unknown level %q (use brief, normal or full)", in.Level))
				}
				flag(&flags, in.Recursive, "-r")
				number(&flags, "--depth", in.Depth)
				value(&flags, "--format", in.Format)
				return run("", "show", flags, []string{in.Path}, []string{in.Path})
			},
		},
		{
			Name: "search", Title: "Search documentation", ReadOnly: true, Idempotent: true,
			Description: "Search the names, summaries, documents and tree notes of all documentation below path. Case is ignored unless the query has an upper-case letter. Each result is a reference to pass to show.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
"query":{"type":"string","description":"Words to find, or a regular expression (RE2) with regex."},
"path":{"type":"string","description":"Search below this directory. Default: the whole project."},
"regex":{"type":"boolean"},
"in":{"type":"array","items":{"enum":["name","summary","document","note"]},"description":"Fields to search. Default: all."},
"limit":{"type":"integer","minimum":0,"description":"Maximum results (0: no limit). Default 50."},
"format":{"type":"string","enum":["text","json"]}},"required":["query"],"additionalProperties":false}`),
			Call: func(raw json.RawMessage) (string, bool) {
				var in struct {
					Query  string   `json:"query"`
					Path   string   `json:"path"`
					Regex  bool     `json:"regex"`
					In     []string `json:"in"`
					Limit  *int     `json:"limit"`
					Format string   `json:"format"`
				}
				if err := mcpArgs(raw, &in); err != nil {
					return fail(err)
				}
				var flags []string
				flag(&flags, in.Regex, "--regex")
				value(&flags, "--in", strings.Join(in.In, ","))
				number(&flags, "--limit", in.Limit)
				value(&flags, "--format", in.Format)
				return run("", "search", flags, append([]string{in.Query}, optional(in.Path)...), optional(in.Path))
			},
		},
		{
			Name: "status", Title: "Documentation status", ReadOnly: true, Idempotent: true,
			Description: "List the directories and files below path with their state: dsd or inherited (directory described), pfd (file described), stale (file changed since), orphan (description of a deleted file), note (only a tree note), none (undocumented).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
"path":{"type":"string","description":"Default: the whole project."},
"only":{"type":"array","items":{"enum":["dsd","inherited","pfd","stale","orphan","note","none"]},"description":"List only these states."},
"depth":{"type":"integer","minimum":0,"description":"How many levels of directories to list."},
"format":{"type":"string","enum":["text","json"]}},"additionalProperties":false}`),
			Call: func(raw json.RawMessage) (string, bool) {
				var in struct {
					Path   string   `json:"path"`
					Only   []string `json:"only"`
					Depth  *int     `json:"depth"`
					Format string   `json:"format"`
				}
				if err := mcpArgs(raw, &in); err != nil {
					return fail(err)
				}
				var flags []string
				value(&flags, "--only", strings.Join(in.Only, ","))
				number(&flags, "--depth", in.Depth)
				value(&flags, "--format", in.Format)
				return run("", "status", flags, optional(in.Path), optional(in.Path))
			},
		},
		{
			Name: "check", Title: "Check documentation", ReadOnly: true, Idempotent: true,
			Description: "Validate the documentation below path: broken references, duplicate ids, trees that no longer match the files, and more. stale also reports files changed since their description; undocumented also reports the symbols of source files (Go, Rust, TypeScript, JavaScript, Java, Kotlin, Python, shell) that have no entry.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
"path":{"type":"string","description":"Default: the whole project."},
"strict":{"type":"boolean","description":"Fail on warnings too."},
"stale":{"type":"boolean"},
"undocumented":{"type":"boolean"},
"format":{"type":"string","enum":["text","json"]}},"additionalProperties":false}`),
			Call: func(raw json.RawMessage) (string, bool) {
				var in struct {
					Path         string `json:"path"`
					Strict       bool   `json:"strict"`
					Stale        bool   `json:"stale"`
					Undocumented bool   `json:"undocumented"`
					Format       string `json:"format"`
				}
				if err := mcpArgs(raw, &in); err != nil {
					return fail(err)
				}
				var flags []string
				flag(&flags, in.Strict, "--strict")
				flag(&flags, in.Stale, "--stale")
				flag(&flags, in.Undocumented, "--undocumented")
				value(&flags, "--format", in.Format)
				return run("", "check", flags, optional(in.Path), optional(in.Path))
			},
		},
		{
			Name: "export", Title: "Export a source", ReadOnly: true, Idempotent: true,
			Description: "Print the editable TOML source of the documentation of a file or directory, or a template when it has none. Edit it and store it with compile.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
"target":{"type":"string","description":"The documented file or directory."},
"format":{"type":"string","enum":["toml","json"],"description":"Default toml."}},"required":["target"],"additionalProperties":false}`),
			Call: func(raw json.RawMessage) (string, bool) {
				var in struct {
					Target string `json:"target"`
					Format string `json:"format"`
				}
				if err := mcpArgs(raw, &in); err != nil {
					return fail(err)
				}
				var flags []string
				value(&flags, "--format", in.Format)
				return run("", "export", flags, []string{in.Target}, []string{in.Target})
			},
		},
		{
			Name: "compile", Title: "Store documentation",
			Description: `Validate a TOML (or JSON) source and store it as the documentation of target: a file's PFD (summary, [[types]], [[functions]], [[variables]] with name, summary, document) or a directory's DSD (summary, tree, [[dsd]]). With merge, the source holds only the changes: entries are matched by id (or the id derived from the name) and delete = true removes one. Nothing is written when there are errors.`,
			InputSchema: json.RawMessage(`{"type":"object","properties":{
"target":{"type":"string","description":"The documented file or directory."},
"source":{"type":"string","description":"The TOML (or JSON) source."},
"merge":{"type":"boolean","description":"Apply source as a partial update of the stored document."},
"dry_run":{"type":"boolean","description":"Validate only."},
"strict":{"type":"boolean","description":"Treat warnings as errors."}},"required":["target","source"],"additionalProperties":false}`),
			Call: func(raw json.RawMessage) (string, bool) {
				var in struct {
					Target string `json:"target"`
					Source string `json:"source"`
					Merge  bool   `json:"merge"`
					DryRun bool   `json:"dry_run"`
					Strict bool   `json:"strict"`
				}
				if err := mcpArgs(raw, &in); err != nil {
					return fail(err)
				}
				var flags []string
				flag(&flags, in.Merge, "--merge")
				flag(&flags, in.DryRun, "--dry-run")
				flag(&flags, in.Strict, "--strict")
				return run(in.Source, "compile", flags, []string{in.Target, "-"}, []string{in.Target})
			},
		},
		{
			Name: "touch", Title: "Mark as up to date", Idempotent: true,
			Description: "Record that the documentation of changed files is still accurate, so that they are no longer shown as stale. The descriptions do not change.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
"files":{"type":"array","items":{"type":"string"},"minItems":1}},"required":["files"],"additionalProperties":false}`),
			Call: func(raw json.RawMessage) (string, bool) {
				var in struct {
					Files []string `json:"files"`
				}
				if err := mcpArgs(raw, &in); err != nil {
					return fail(err)
				}
				if len(in.Files) == 0 {
					return fail(fmt.Errorf("files is empty"))
				}
				return run("", "touch", nil, in.Files, in.Files)
			},
		},
		{
			Name: "rm", Title: "Remove documentation", Destructive: true,
			Description: `Remove documentation: "path" removes a file's PFD or a directory's DSD, "path#id" one entry. Works after the file was deleted.`,
			InputSchema: json.RawMessage(`{"type":"object","properties":{
"refs":{"type":"array","items":{"type":"string"},"minItems":1,"description":"Paths or path#id references."}},"required":["refs"],"additionalProperties":false}`),
			Call: func(raw json.RawMessage) (string, bool) {
				var in struct {
					Refs []string `json:"refs"`
				}
				if err := mcpArgs(raw, &in); err != nil {
					return fail(err)
				}
				if len(in.Refs) == 0 {
					return fail(fmt.Errorf("refs is empty"))
				}
				return run("", "rm", nil, in.Refs, in.Refs)
			},
		},
		{
			Name: "mv", Title: "Move with documentation", Destructive: true,
			Description: "Move or rename a file or directory together with its documentation: references to it are rewritten everywhere and trees follow. When the file was already moved, only the documentation follows.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
"source":{"type":"string"},
"destination":{"type":"string","description":"The new path, or an existing directory to move into."},
"dry_run":{"type":"boolean","description":"Only report what would change."}},"required":["source","destination"],"additionalProperties":false}`),
			Call: func(raw json.RawMessage) (string, bool) {
				var in struct {
					Source      string `json:"source"`
					Destination string `json:"destination"`
					DryRun      bool   `json:"dry_run"`
				}
				if err := mcpArgs(raw, &in); err != nil {
					return fail(err)
				}
				var flags []string
				flag(&flags, in.DryRun, "--dry-run")
				return run("", "mv", flags, []string{in.Source, in.Destination}, []string{in.Source, in.Destination})
			},
		},
	}
	return &mcp.Server{Name: "dirdoc", Version: version(), Instructions: mcpInstructions, Tools: tools}, nil
}

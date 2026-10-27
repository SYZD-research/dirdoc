package cli

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/SYZD-research/dirdoc/internal/container"
	"github.com/SYZD-research/dirdoc/internal/model"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const (
	fixtureDir = "../../testdata/myapp"
	schemaDir  = "../../schema"
)

// fixtureFiles is the file tree of the myapp project used throughout the design document.
var fixtureFiles = map[string]string{
	"dirdoc.toml":         "",
	".gitignore":          "target/\n",
	"Cargo.toml":          "",
	"docs/notes.md":       "",
	"src/main.rs":         "",
	"src/cli/args.rs":     "",
	"src/cli/commands.rs": "",
	"src/agent/pool.rs":   "",
	"src/agent/runner.rs": "",
	"target/debug/myapp":  "",
	"tools/release.go":    releaseGo,
}

// releaseGo is the one Go file of the fixture, for code analysis.
const releaseGo = `// Command release prints the archive names of a myapp release.
package main

import "fmt"

// Version is the version being released.
const Version = "1.2.0"

// Target is a platform to build for.
type Target struct {
	OS, Arch string
}

var targets = []Target{{"linux", "amd64"}, {"darwin", "arm64"}}

func main() {
	for _, t := range targets {
		fmt.Println(build(t))
	}
}

func build(t Target) string {
	return fmt.Sprintf("myapp-%s-%s.tar.gz", Version, t)
}

func (t Target) String() string { return t.OS + "-" + t.Arch }
`

type result struct {
	code           int
	stdout, stderr string
}

func run(t *testing.T, cwd, stdin string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	app := &App{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errb, Cwd: cwd, Getenv: func(string) string { return "" }}
	code := app.Run(args)
	return result{code, out.String(), errb.String()}
}

type target struct{ path, source string }

func manifest(t *testing.T) []target {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir, "compile.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var out []target
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && !strings.HasPrefix(f[0], "#") {
			src, _ := filepath.Abs(filepath.Join(fixtureDir, "sources", f[1]))
			out = append(out, target{f[0], src})
		}
	}
	return out
}

// newFixture builds the myapp project in a temporary directory and compiles its sources.
func newFixture(t *testing.T, config string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range fixtureFiles {
		if rel == "dirdoc.toml" {
			content = config
		}
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tg := range manifest(t) {
		if r := run(t, root, "", "compile", tg.path, tg.source); r.code != 0 {
			t.Fatalf("compile %s: exit %d\n%s%s", tg.path, r.code, r.stdout, r.stderr)
		}
	}
	return root
}

func schema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	path, _ := filepath.Abs(filepath.Join(schemaDir, name))
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(path, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func validate(t *testing.T, s *jsonschema.Schema, data []byte) {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
	if err := s.Validate(v); err != nil {
		t.Errorf("does not match the schema: %v", err)
	}
}

const badSource = `summray = "typo"

[[functions]]
name = "a()"
summary = "first"
document = "see [[#nope]] and [[../../outside.rs]] and [[missing.rs#x]]"

[[functions]]
id = "a"
name = "a2()"
summary = "duplicate id"

[[functions]]
name = "async def f()"
summary = "underivable"
`

var goldenCases = []struct {
	name  string
	dir   string // working directory below the project root
	stdin string
	json  bool
	args  []string
}{
	{name: "show-root"},
	{name: "show-src", args: []string{"src"}},
	{name: "show-src-brief", args: []string{"src", "--brief"}},
	{name: "show-src-full", args: []string{"src", "--full"}},
	{name: "show-src-json", json: true, args: []string{"src", "-f", "json"}},
	{name: "show-file", args: []string{"src/main.rs"}},
	{name: "show-file-json", json: true, args: []string{"src/main.rs", "-f", "json"}},
	{name: "show-entry", args: []string{"src/main.rs#main"}},
	{name: "show-entry-json", json: true, args: []string{"src/main.rs#check_stdout", "-f", "json"}},
	{name: "show-dsd-entry", args: []string{"src/#cli"}},
	{name: "show-inherited", args: []string{"src/cli"}},
	{name: "show-inherited-json", json: true, args: []string{"src/cli", "-f", "json"}},
	{name: "show-from-subdir", dir: "src/agent", args: []string{"pool.rs#spawn"}},
	{name: "show-recursive", args: []string{"-r", "--brief"}},
	{name: "show-recursive-json", json: true, args: []string{"-r", "--brief", "-f", "json"}},
	{name: "show-undocumented", args: []string{"docs"}},
	{name: "show-missing-entry", args: []string{"src/main.rs#nope"}},
	{name: "show-go-file", args: []string{"tools/release.go"}},
	{name: "show-go-file-json", json: true, args: []string{"tools/release.go", "-f", "json"}},
	{name: "show-go-entry", args: []string{"tools/release.go#build"}},
	{name: "show-go-entry-json", json: true, args: []string{"tools/release.go#Version", "-f", "json"}},
	{name: "check", args: []string{"check"}},
	{name: "check-json", json: true, args: []string{"check", "-f", "json"}},
	{name: "check-undocumented", args: []string{"check", "--undocumented"}},
	{name: "status", args: []string{"status"}},
	{name: "status-json", json: true, args: []string{"status", "src", "-f", "json"}},
	{name: "cat-pfd", args: []string{"cat", "src/.dirdoc/main.rs.dirdoc_pfd"}},
	{name: "search", args: []string{"search", "ワーカー"}},
	{name: "search-json", json: true, args: []string{"search", "tty", "-f", "json"}},
	{name: "export-src", args: []string{"export", "src/"}},
	{name: "export-main-json", args: []string{"export", "src/main.rs", "--format", "json"}},
	{name: "export-template-dir", args: []string{"export", "src/cli"}},
	{name: "export-template-file", args: []string{"export", "src/cli/args.rs"}},
	{name: "compile-errors", stdin: badSource, args: []string{"compile", "src/main.rs", "-"}},
	{name: "usage-error", args: []string{"compile", "src/"}},
	{name: "guide-ja", args: []string{"guide", "--lang", "ja"}},
}

func TestGolden(t *testing.T) {
	root := newFixture(t, "")
	view := schema(t, "view.v1.schema.json")
	for _, c := range goldenCases {
		t.Run(c.name, func(t *testing.T) {
			r := run(t, filepath.Join(root, c.dir), c.stdin, c.args...)
			got := fmt.Sprintf("$ dirdoc %s\nexit: %d\n--- stdout\n%s--- stderr\n%s", strings.Join(c.args, " "), r.code, r.stdout, r.stderr)
			path := filepath.Join(fixtureDir, "golden", c.name+".txt")
			if *update {
				os.MkdirAll(filepath.Dir(path), 0o755)
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			} else if want, err := os.ReadFile(path); err != nil || string(want) != got {
				t.Errorf("output differs from %s (run `go test ./internal/cli -update` to accept):\n%s", path, got)
			}
			if c.json {
				validate(t, view, []byte(r.stdout))
			}
		})
	}
}

func TestExportCompileRoundTrip(t *testing.T) {
	root := newFixture(t, "")
	for _, tg := range manifest(t) {
		exported := run(t, root, "", "export", tg.path)
		if exported.code != 0 {
			t.Fatalf("export %s: %v", tg.path, exported)
		}
		r := run(t, root, exported.stdout, "compile", tg.path, "-")
		if r.code != 0 || !strings.HasPrefix(r.stdout, "unchanged ") {
			t.Errorf("%s: export then compile changed the document: %+v", tg.path, r)
		}
	}
}

func TestStoredFilesMatchSchemas(t *testing.T) {
	root := newFixture(t, "")
	schemas := map[string]*jsonschema.Schema{
		model.FormatDSD: schema(t, "dsd.v1.schema.json"),
		model.FormatPFD: schema(t, "pfd.v1.schema.json"),
	}
	count := 0
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(filepath.Dir(p)) != model.DirName {
			return err
		}
		js, compressed, err := container.ReadFile(p)
		if err != nil || !compressed {
			t.Errorf("%s: compressed=%v err=%v", p, compressed, err)
			return nil
		}
		format := model.FormatDSD
		if strings.HasSuffix(p, model.PFDSuffix) {
			format = model.FormatPFD
		}
		validate(t, schemas[format], js)
		count++
		return nil
	})
	if count != 6 {
		t.Errorf("found %d stored documents, want 6", count)
	}
}

func TestSchemasRejectInvalidDocuments(t *testing.T) {
	bad := map[string]string{
		"view.v1.schema.json": `{"dirdoc_view":1,"kind":"directory"}`,
		"dsd.v1.schema.json":  `{"format":"dirdoc/dsd","version":1,"tree":{"name":"src","type":"dir"}}`,
		"pfd.v1.schema.json": `{"format":"dirdoc/pfd","version":1,"target":"a.rs","overview":{"types":[],"functions":[],"variables":[]},` +
			`"variables":[{"id":"V","name":"V","summary":"s","document":"not allowed"}]}`,
	}
	for name, doc := range bad {
		v, err := jsonschema.UnmarshalJSON(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		if err := schema(t, name).Validate(v); err == nil {
			t.Errorf("%s accepted an invalid document: %s", name, doc)
		}
	}
}

func TestUncompressedStorage(t *testing.T) {
	root := newFixture(t, "[storage]\ncompression = \"none\"\n")
	data, err := os.ReadFile(filepath.Join(root, "src", model.DirName, "main.rs"+model.PFDSuffix))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("{\n  \"format\": \"dirdoc/pfd\"")) {
		t.Errorf("not indented JSON:\n%s", data)
	}
	if r := run(t, root, "", "src/main.rs", "--brief"); r.code != 0 || !strings.Contains(r.stdout, "check_stdout") {
		t.Errorf("show: %+v", r)
	}
}

func TestMergeUpdatesAndKeepsReferencesValid(t *testing.T) {
	root := newFixture(t, "")
	r := run(t, root, "[[functions]]\nid = \"check_stdout\"\nsummary = \"stdout も stderr も見る\"\n\n[[functions]]\nname = \"load_config()\"\nsummary = \"設定を読む\"\n",
		"compile", "src/main.rs", "--merge", "-")
	if r.code != 0 || !strings.Contains(r.stdout, "functions: 3") {
		t.Fatalf("merge: %+v", r)
	}
	show := run(t, root, "", "src/main.rs#check_stdout")
	if !strings.Contains(show.stdout, "stdout も stderr も見る") || !strings.Contains(show.stdout, "Windows では") {
		t.Errorf("merged entry lost its document or did not change:\n%s", show.stdout)
	}
	// Deleting MAX_AGENTS would break [[#MAX_AGENTS]] in check_stdout's document.
	r = run(t, root, "[[variables]]\nid = \"MAX_AGENTS\"\ndelete = true\n", "compile", "src/main.rs", "--merge", "-")
	if r.code != 1 || !strings.Contains(r.stderr, "error[ref-local]") {
		t.Errorf("delete of a referenced entry was not refused: %+v", r)
	}
}

func TestDriftIsReported(t *testing.T) {
	root := newFixture(t, "")
	os.MkdirAll(filepath.Join(root, "src", "util"), 0o755)
	os.Remove(filepath.Join(root, "src", "agent", "runner.rs"))
	os.Remove(filepath.Join(root, "src", "agent", "pool.rs"))
	r := run(t, root, "", "check")
	for _, want := range []string{
		"warning[tree-unlisted] src/util/:",
		"warning[tree-missing] src/agent/runner.rs:",
		"warning[tree-missing] src/agent/pool.rs:",
		"warning[orphan-pfd] src/agent/pool.rs:",
		"4 warnings",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("check output lacks %q:\n%s", want, r.stdout)
		}
	}
	if r.code != 0 {
		t.Errorf("check exit = %d, want 0", r.code)
	}
	if r := run(t, root, "", "check", "--strict"); r.code != 1 {
		t.Errorf("check --strict exit = %d, want 1", r.code)
	}
	if r := run(t, root, "", "src/agent/pool.rs", "--brief"); r.code != 0 || !strings.Contains(r.stdout, "orphan-pfd") {
		t.Errorf("an orphaned PFD should still be shown with a warning: %+v", r)
	}
}

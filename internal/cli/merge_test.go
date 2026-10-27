package cli

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// versions stores three versions of src/main.rs's PFD: the fixture's, and two changes of it.
func versions(t *testing.T, root, ours, theirs string) (base, o, th string) {
	t.Helper()
	stored := filepath.Join(root, "src", ".dirdoc", "main.rs.dirdoc_pfd")
	orig, err := os.ReadFile(stored)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	save := func(name string) string {
		data, _ := os.ReadFile(stored)
		p := filepath.Join(dir, name)
		os.WriteFile(p, data, 0o644)
		os.WriteFile(stored, orig, 0o644)
		return p
	}
	base = save("base")
	for _, change := range []string{ours, theirs} {
		if r := run(t, root, change, "compile", "src/main.rs", "--merge", "-"); r.code != 0 {
			t.Fatalf("compile: %+v", r)
		}
		if change == ours {
			o = save("ours")
		} else {
			th = save("theirs")
		}
	}
	return base, o, th
}

func TestMergeDriver(t *testing.T) {
	root := newFixture(t, "")
	base, ours, theirs := versions(t, root,
		"[[functions]]\nid = \"check_stdout\"\nsummary = \"ours\"\n\n[[functions]]\nname = \"load()\"\nsummary = \"added by ours\"\n",
		"[[types]]\nid = \"Config\"\nsummary = \"theirs\"\n\n[[variables]]\nid = \"MAX_AGENTS\"\nsummary = \"theirs too\"\n")
	r := run(t, root, "", "merge-driver", base, ours, theirs, "src/.dirdoc/main.rs.dirdoc_pfd")
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("clean merge: %+v", r)
	}
	got := run(t, root, "", "cat", ours).stdout
	mustContain(t, "merged document", got, `summary = "ours"`, `summary = "added by ours"`, `summary = "theirs"`, `summary = "theirs too"`)
	if i, j := strings.Index(got, `id = "check_stdout"`), strings.Index(got, `id = "load"`); i < 0 || j < i {
		t.Errorf("the added entry is not after the entries it followed:\n%s", got)
	}

	// Both sides change the summary of check_stdout; ours removes main, which theirs changes.
	base, ours, theirs = versions(t, root,
		"[[functions]]\nid = \"check_stdout\"\nsummary = \"ours\"\n",
		"[[functions]]\nid = \"check_stdout\"\nsummary = \"theirs\"\n")
	r = run(t, root, "", "merge-driver", base, ours, theirs, "src/.dirdoc/main.rs.dirdoc_pfd")
	if r.code != 1 {
		t.Fatalf("conflict: %+v", r)
	}
	mustContain(t, "conflict report", r.stderr, "src/main.rs#check_stdout summary changed on both sides",
		"dirdoc compile src/main.rs src/.dirdoc/main.rs.dirdoc_pfd")
	data, _ := os.ReadFile(ours)
	text := string(data)
	mustContain(t, "conflict file", text, "# dirdoc pfd: src/main.rs\n", "<<<<<<< ours\nsummary = \"ours\"\n=======\nsummary = \"theirs\"\n>>>>>>> theirs\n")
	if r := run(t, root, "", "cat", ours); r.stdout != text {
		t.Errorf("cat does not show the conflict file as it is: %+v", r)
	}
	if r := run(t, root, "", "compile", "src/main.rs", ours); r.code != 1 || !strings.Contains(r.stderr, "unresolved merge conflict") {
		t.Errorf("compile of an unresolved file: %+v", r)
	}
	resolved := regexp.MustCompile(`(?s)<<<<<<< ours\n.*?=======\n(.*?)>>>>>>> theirs\n`).ReplaceAllString(text, "$1")
	os.WriteFile(ours, []byte(resolved), 0o644)
	if r := run(t, root, "", "compile", "src/main.rs", ours); r.code != 0 {
		t.Errorf("compile of the resolved file: %+v", r)
	}
	mustContain(t, "resolved", run(t, root, "", "src/main.rs#check_stdout").stdout, "> theirs")
}

func TestMergeDriverChecksTheResult(t *testing.T) {
	root := newFixture(t, "")
	// Ours removes MAX_AGENTS (and the reference to it); theirs refers to it from main.
	base, ours, theirs := versions(t, root,
		"[[functions]]\nid = \"check_stdout\"\ndocument = \"plain\"\n\n[[variables]]\nid = \"MAX_AGENTS\"\ndelete = true\n",
		"[[functions]]\nid = \"main\"\ndocument = \"see [[#MAX_AGENTS]]\"\n")
	r := run(t, root, "", "merge-driver", base, ours, theirs, "src/.dirdoc/main.rs.dirdoc_pfd")
	if r.code != 1 || !strings.Contains(r.stderr, "error[ref-local] src/main.rs#main: [[#MAX_AGENTS]] does not exist") {
		t.Errorf("a merge that breaks a reference: %+v", r)
	}
	data, _ := os.ReadFile(ours)
	mustContain(t, "source with errors", string(data), "# The merged document has errors to fix:\n#   error[ref-local]")
}

func TestInitGitInstallsTheMergeDriver(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	write(t, dir, ".gitattributes", "*.png binary\n.dirdoc_dsd   -text -merge diff=dirdoc\n*.dirdoc_pfd  -text -merge diff=dirdoc\n")
	if r := run(t, dir, "", "init", "--git"); r.code != 0 {
		t.Fatalf("init --git: %+v", r)
	}
	data, _ := os.ReadFile(filepath.Join(dir, ".gitattributes"))
	if want := "*.png binary\n.dirdoc_dsd   -text merge=dirdoc diff=dirdoc\n*.dirdoc_pfd  -text merge=dirdoc diff=dirdoc\n"; string(data) != want {
		t.Errorf(".gitattributes:\n%s\nwant:\n%s", data, want)
	}
	cmd := exec.Command("git", "config", "--get", "merge.dirdoc.driver")
	cmd.Dir = dir
	if out, err := cmd.Output(); err != nil || strings.TrimSpace(string(out)) != "dirdoc merge-driver %O %A %B %P" {
		t.Errorf("merge.dirdoc.driver = %q (%v)", out, err)
	}
	if r := run(t, dir, "", "init", "--git"); !strings.Contains(r.stdout, ".gitattributes already has the dirdoc entries") {
		t.Errorf("second init --git: %+v", r)
	}
}

func TestTemplateHeadings(t *testing.T) {
	root := newFixture(t, "[template]\nfunction = [\"Errors\"]\ndsd = [\"## Usage\"]\n")
	mustContain(t, "PFD template", run(t, root, "", "export", "src/cli/args.rs").stdout,
		"# Markdown body (optional). Link with [[#id]], [[other.rs#id]] or [[dir/]].\n#\n# ## Errors\n# '''")
	mustContain(t, "DSD template", run(t, root, "", "export", "docs").stdout, "#\n# ## Usage\n# '''")

	r := run(t, root, "summary = \"引数\"\n\n[[functions]]\nname = \"parse()\"\nsummary = \"解析する\"\ndocument = '''\n本文\n\n### errors\nなし\n'''\n\n"+
		"[[functions]]\nname = \"run()\"\nsummary = \"実行する\"\ndocument = \"本文だけ\"\n\n[[functions]]\nname = \"stop()\"\nsummary = \"本文なし\"\n",
		"compile", "src/cli/args.rs", "-")
	if r.code != 0 || r.stderr != "warning[template-heading] <stdin>:16 (src/cli/args.rs#run): the document has no \"Errors\" heading (required by [template] in dirdoc.toml)\n" {
		t.Errorf("compile: %+v", r)
	}
	// The fixture's documents predate the template.
	mustContain(t, "check", run(t, root, "", "check").stdout, "warning[template-heading] src/main.rs#main:", "warning[template-heading] src/#src:")
}

func TestMCP(t *testing.T) {
	root := newFixture(t, "")
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"show","arguments":{"path":"src/main.rs#main","level":"brief"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"compile","arguments":{"target":"src/main.rs","merge":true,"source":"[[functions]]\nid = \"main\"\nsummary = \"via MCP\"\n"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"search","arguments":{"query":"via MCP"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"show","arguments":{"path":"../.."}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"show","arguments":{"path":"docs"}}}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"check","arguments":{"strict":"yes"}}}`,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"status","arguments":{"path":"src","only":["inherited"]}}}`,
	}
	r := run(t, root, strings.Join(requests, "\n")+"\n", "mcp")
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("mcp: %+v", r)
	}
	type reply struct {
		ID     int `json:"id"`
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			Tools           []struct {
				Name string `json:"name"`
			} `json:"tools"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	replies := map[int]reply{}
	sc := bufio.NewScanner(strings.NewReader(r.stdout))
	for sc.Scan() {
		var rep reply
		if err := json.Unmarshal(sc.Bytes(), &rep); err != nil {
			t.Fatalf("not JSON: %s", sc.Text())
		}
		replies[rep.ID] = rep
	}
	if len(replies) != len(requests)-1 {
		t.Errorf("%d replies to %d requests (one is a notification):\n%s", len(replies), len(requests), r.stdout)
	}
	if v := replies[1].Result.ProtocolVersion; v != "2025-03-26" {
		t.Errorf("negotiated version %q", v)
	}
	if n := len(replies[2].Result.Tools); n != 9 {
		t.Errorf("%d tools", n)
	}
	text := func(id int) string {
		if c := replies[id].Result.Content; len(c) == 1 {
			return c[0].Text
		}
		return ""
	}
	for id, want := range map[int]string{
		3: "# fn main() `src/main.rs#main`",
		4: "wrote src/.dirdoc/main.rs.dirdoc_pfd",
		5: "src/main.rs#main  fn main() — via MCP",
		6: "is outside the project",
		7: "docs/ is not documented",
		8: "invalid arguments",
		9: "inherited  src/cli/",
	} {
		if !strings.Contains(text(id), want) {
			t.Errorf("reply %d lacks %q: %q", id, want, text(id))
		}
		if wantErr := id == 6 || id == 7 || id == 8; replies[id].Result.IsError != wantErr {
			t.Errorf("reply %d: isError = %v", id, replies[id].Result.IsError)
		}
	}
}

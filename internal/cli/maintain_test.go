package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SYZD-research/dirdoc/internal/model"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustContain(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s lacks %q:\n%s", what, w, got)
		}
	}
}

func TestStaleAndTouch(t *testing.T) {
	root := newFixture(t, "")
	if r := run(t, root, "", "src/main.rs"); strings.Contains(r.stdout, "stale") {
		t.Fatalf("a freshly compiled file is stale:\n%s", r.stdout)
	}
	write(t, root, "src/main.rs", "fn main() {}\r\n")

	r := run(t, root, "", "src/main.rs")
	mustContain(t, "file view", r.stdout, "# src/main.rs\n\n> stale: src/main.rs has changed since this document was last compiled\n")
	mustContain(t, "entry view", run(t, root, "", "src/main.rs#main").stdout, "> stale: src/main.rs")
	mustContain(t, "directory view", run(t, root, "", "src").stdout, "### main.rs `src/main.rs` (stale)")
	mustContain(t, "brief directory view", run(t, root, "", "src", "--brief").stdout, "`src/main.rs` (stale) —")
	var v struct{ Stale *bool }
	if err := json.Unmarshal([]byte(run(t, root, "", "src/main.rs", "-f", "json").stdout), &v); err != nil || v.Stale == nil || !*v.Stale {
		t.Errorf("JSON stale = %v (%v)", v.Stale, err)
	}

	if r := run(t, root, "", "check"); strings.Contains(r.stdout, "stale") {
		t.Errorf("check reports stale files without --stale:\n%s", r.stdout)
	}
	r = run(t, root, "", "check", "--stale", "--strict")
	mustContain(t, "check --stale", r.stdout, "warning[stale] src/main.rs:")
	if r.code != 1 {
		t.Errorf("check --stale --strict exit = %d", r.code)
	}
	mustContain(t, "status", run(t, root, "", "status", "--only", "stale").stdout, "stale      src/main.rs\n")

	if r := run(t, root, "", "touch", "src/main.rs"); r.code != 0 || r.stdout != "touched src/main.rs\n" {
		t.Errorf("touch: %+v", r)
	}
	if r := run(t, root, "", "touch", "src/main.rs"); r.stdout != "unchanged src/main.rs (already up to date)\n" {
		t.Errorf("second touch: %+v", r)
	}
	if r := run(t, root, "", "src/main.rs"); strings.Contains(r.stdout, "stale") {
		t.Errorf("still stale after touch:\n%s", r.stdout)
	}
	// Line endings do not matter: LF and CRLF content hash the same.
	write(t, root, "src/main.rs", "fn main() {}\n")
	if r := run(t, root, "", "check", "--stale"); strings.Contains(r.stdout, "stale") {
		t.Errorf("a change of line endings made the file stale:\n%s", r.stdout)
	}

	r = run(t, root, "", "touch", "src/", "src/cli/args.rs", "nope.rs", "src/main.rs")
	mustContain(t, "touch errors", r.stderr, "src/ is a directory", "src/cli/args.rs has no PFD", "nope.rs: no such file")
	if r.code != 1 || r.stdout != "unchanged src/main.rs (already up to date)\n" {
		t.Errorf("touch with errors: %+v", r)
	}
}

func TestCompileRecordsAnalysis(t *testing.T) {
	root := newFixture(t, "")
	write(t, root, "tools/release.go", "package main\n\n\nfunc main() {}\n\nfunc build() string { return \"\" }\n\nfunc helper() {}\n")
	r := run(t, root, "", "tools/release.go")
	if strings.Contains(r.stdout, "fn `main()` `#main` L4") {
		t.Fatal("the overview changed before the document was compiled again")
	}
	mustContain(t, "stale view", r.stdout, "> stale: tools/release.go")
	if r := run(t, root, "", "touch", "tools/release.go"); r.code != 0 {
		t.Fatalf("touch: %+v", r)
	}
	r = run(t, root, "", "tools/release.go")
	mustContain(t, "touched view", r.stdout, "fn `main()` `#main` L4 —", "fn `build(t)` `#build` L6 —", "- fn `helper` L8 — `func helper()`")
	if strings.Contains(r.stdout, "targets") || strings.Contains(r.stdout, "stale") {
		t.Errorf("the analysis was not refreshed:\n%s", r.stdout)
	}
	// Documenting a symbol takes it off the undocumented list.
	r = run(t, root, "[[functions]]\nname = \"helper()\"\nsummary = \"手伝う\"\n", "compile", "tools/release.go", "--merge", "-")
	if r.code != 0 {
		t.Fatalf("compile: %+v", r)
	}
	r = run(t, root, "", "tools/release.go")
	mustContain(t, "after compile", r.stdout, "fn `helper()` `#helper` L8 — 手伝う")
	if strings.Contains(r.stdout, "## Undocumented") {
		t.Errorf("helper is still undocumented:\n%s", r.stdout)
	}
}

func TestStatus(t *testing.T) {
	root := newFixture(t, "")
	os.Remove(filepath.Join(root, "src", "agent", "pool.rs"))
	r := run(t, root, "", "status", "src", "--only", "orphan,inherited")
	if r.code != 0 || r.stdout != "orphan     src/agent/pool.rs\ninherited  src/cli/\n"+
		"directories: 2 dsd, 1 inherited, 0 none; files: 1 pfd, 0 stale, 1 orphan, 1 note, 2 none\n" {
		t.Errorf("status: %+v", r)
	}
	if r := run(t, root, "", "status", "src/main.rs"); r.stdout != "pfd        src/main.rs\n"+
		"directories: 0 dsd, 0 inherited, 0 none; files: 1 pfd, 0 stale, 0 orphan, 0 note, 0 none\n" {
		t.Errorf("status of a file: %+v", r)
	}
	if r := run(t, root, "", "status", "--depth", "0"); strings.Contains(r.stdout, "src/main.rs") {
		t.Errorf("--depth 0 lists subdirectories:\n%s", r.stdout)
	}
	if r := run(t, root, "", "status", "--only", "fresh"); r.code != 2 {
		t.Errorf("unknown state: %+v", r)
	}
}

func TestRm(t *testing.T) {
	root := newFixture(t, "")
	// main's document refers to check_stdout: removing it alone would break the document.
	r := run(t, root, "", "rm", "src/main.rs#check_stdout")
	if r.code != 1 || !strings.Contains(r.stderr, "error[ref-local] src/main.rs#main:") {
		t.Errorf("rm of a referenced entry: %+v", r)
	}
	r = run(t, root, "", "rm", "src/main.rs#MAX_AGENTS", "src/main.rs#check_stdout", "src/main.rs#main", "src/main.rs#Config")
	if r.code != 0 {
		t.Fatalf("rm of entries that refer to each other: %+v", r)
	}
	mustContain(t, "rm warnings", r.stderr, "warning[ref-broken] src/agent/pool.rs#spawn: refers to src/main.rs#MAX_AGENTS, which was removed")
	if show := run(t, root, "", "src/main.rs"); strings.Contains(show.stdout, "## Overview") {
		t.Errorf("entries left:\n%s", show.stdout)
	}

	r = run(t, root, "", "rm", "src/#cli")
	if r.code != 0 || r.stdout != "removed src/#cli from src/.dirdoc/.dirdoc_dsd\n" {
		t.Errorf("rm of a dsd entry: %+v", r)
	}
	if exp := run(t, root, "", "export", "src/").stdout; strings.Contains(exp, "#cli") {
		t.Errorf("the tree still links to the removed entry:\n%s", exp)
	}

	// A PFD whose file is gone can still be removed, and an empty .dirdoc/ goes with it.
	os.Remove(filepath.Join(root, "src", "agent", "pool.rs"))
	run(t, root, "", "rm", "src/agent/")
	r = run(t, root, "", "rm", "src/agent/pool.rs")
	if r.code != 0 || r.stdout != "removed src/agent/.dirdoc/pool.rs.dirdoc_pfd\n" {
		t.Errorf("rm of an orphaned PFD: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "agent", model.DirName)); !os.IsNotExist(err) {
		t.Errorf("the empty .dirdoc/ was left: %v", err)
	}
	for _, args := range [][]string{{"rm", "docs/"}, {"rm", "src/cli/args.rs"}, {"rm", "src/main.rs#nope"}, {"rm", "missing.rs"}} {
		if r := run(t, root, "", args...); r.code != 1 {
			t.Errorf("%v: %+v", args, r)
		}
	}
}

func TestEdit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test editors are shell scripts")
	}
	root := newFixture(t, "")
	t.Setenv("TMPDIR", t.TempDir())
	scripts := t.TempDir()
	editor := func(name, body string) string {
		p := filepath.Join(scripts, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// The first save has an error; the second fixes it.
	twice := editor("twice", `if [ -f "$1.seen" ]; then printf 'summary = "edited"\n' > "$1"; else : > "$1.seen"; printf 'summray = "x"\n' > "$1"; fi`+"\n")
	edit := func(editorPath, answers string, args ...string) result {
		var out, errb strings.Builder
		app := &App{Stdin: strings.NewReader(answers), Stdout: &out, Stderr: &errb, Cwd: root,
			Getenv: func(k string) string {
				if k == "EDITOR" {
					return editorPath
				}
				return ""
			}}
		code := app.Run(append([]string{"edit"}, args...))
		return result{code, out.String(), errb.String()}
	}

	r := edit(twice, "\n", "src/cli/args.rs")
	if r.code != 0 || !strings.Contains(r.stderr, `unknown key "summray"`) || !strings.Contains(r.stderr, "Edit again? [Y/n]") ||
		!strings.Contains(r.stdout, "wrote src/cli/.dirdoc/args.rs.dirdoc_pfd") {
		t.Errorf("edit with a retry: %+v", r)
	}
	mustContain(t, "edited file", run(t, root, "", "src/cli/args.rs").stdout, "> edited")

	r = edit(twice, "n\n", "src/cli/commands.rs")
	if r.code != 1 || !strings.Contains(r.stderr, "nothing written; your edits are in ") {
		t.Errorf("edit given up: %+v", r)
	}
	if r := run(t, root, "", "src/cli/commands.rs"); r.code != 3 {
		t.Errorf("a given-up edit was stored: %+v", r)
	}

	if r := edit("true", "", "src/"); r.code != 0 || r.stdout != "no changes; nothing written\n" {
		t.Errorf("edit without changes: %+v", r)
	}
	if r := edit("false", "", "src/"); r.code != 1 || !strings.Contains(r.stderr, `editor "false" failed`) {
		t.Errorf("failing editor: %+v", r)
	}
}

func TestMv(t *testing.T) {
	root := newFixture(t, "")
	r := run(t, root, "", "mv", "src/main.rs", "src/app.rs", "--dry-run")
	if r.code != 0 || !strings.Contains(r.stdout, "would update src/agent/.dirdoc/pool.rs.dirdoc_pfd (1 reference)") {
		t.Errorf("dry run: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "main.rs")); err != nil {
		t.Fatal("the dry run moved the file")
	}

	r = run(t, root, "", "mv", "src/main.rs", "src/app.rs")
	want := "moved src/main.rs to src/app.rs\n" +
		"updated src/.dirdoc/.dirdoc_dsd (tree: main.rs -> app.rs)\n" +
		"updated src/.dirdoc/app.rs.dirdoc_pfd (moved from src/.dirdoc/main.rs.dirdoc_pfd)\n" +
		"updated src/agent/.dirdoc/pool.rs.dirdoc_pfd (1 reference)\n"
	if r.code != 0 || r.stdout != want {
		t.Errorf("mv file:\n%+v\nwant stdout:\n%s", r, want)
	}
	mustContain(t, "moved reference", run(t, root, "", "export", "src/agent/pool.rs").stdout, "[[../app.rs#MAX_AGENTS]]")

	// A directory moved one level deeper: its relative references gain a "../", the tree follows.
	os.MkdirAll(filepath.Join(root, "src", "lib"), 0o755)
	if r := run(t, root, "", "mv", "src/agent", "src/lib"); r.code != 0 {
		t.Fatalf("mv dir: %+v", r)
	}
	mustContain(t, "deeper reference", run(t, root, "", "export", "src/lib/agent/pool.rs").stdout, "[[../../app.rs#MAX_AGENTS]]")
	mustContain(t, "tree", run(t, root, "", "export", "src/").stdout, "├── lib/\n│   └── agent/            # エージェントの起動と管理\n")
	mustContain(t, "rewritten reference", run(t, root, "", "src/app.rs#main").stdout, "[[src/lib/agent/pool.rs#spawn]]")

	// Moved by someone else: only the documentation follows.
	os.Rename(filepath.Join(root, "src", "lib", "agent"), filepath.Join(root, "src", "lib", "workers"))
	r = run(t, root, "", "mv", "src/lib/agent", "src/lib/workers")
	if r.code != 0 || !strings.HasPrefix(r.stdout, "moved the documentation of src/lib/agent/ to src/lib/workers/\n") {
		t.Errorf("mv after the move: %+v", r)
	}
	if r := run(t, root, "", "check"); r.code != 0 || !strings.HasSuffix(r.stdout, "0 errors, 0 warnings\n") {
		t.Errorf("check after the moves: %+v", r)
	}

	for _, args := range [][]string{
		{"mv", "src/app.rs", "src/cli"},        // moves into src/cli: fine
		{"mv", "src/cli/app.rs", "Cargo.toml"}, // exists
		{"mv", "src", "src/lib/x"},             // into itself
		{"mv", "nope.rs", "other.rs"},          // nothing to move
		{"mv", "src/lib", "../outside"},        // leaves the project
	} {
		r := run(t, root, "", args...)
		if wantOK := args[2] == "src/cli"; (r.code == 0) != wantOK {
			t.Errorf("%v: %+v", args, r)
		}
	}
	mustContain(t, "moved into a directory", run(t, root, "", "export", "src/cli/app.rs").stdout, "[[../lib/workers/pool.rs#spawn]]")
}

package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// runTerminal runs dirdoc as if its standard output were a terminal, with the variables of env.
func runTerminal(t *testing.T, cwd string, env map[string]string, args ...string) result {
	t.Helper()
	return runTerminalStdin(t, cwd, "", env, args...)
}

func runTerminalStdin(t *testing.T, cwd, stdin string, env map[string]string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	app := &App{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errb, Cwd: cwd,
		Getenv:    func(k string) string { return env[k] },
		LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok },
		Terminal:  func(w io.Writer) bool { return w == &out },
	}
	code := app.Run(args)
	return result{code, out.String(), errb.String()}
}

var escapes = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestColor(t *testing.T) {
	root := newFixture(t, "")
	tty := map[string]string{"TERM": "xterm-256color", "DIRDOC_PAGER": "cat"}
	colored := map[string]bool{"show-src": true, "show-file": true, "show-entry": true, "check-undocumented": true,
		"status": true, "search": true, "guide-ja": true}
	for _, c := range goldenCases {
		plain := run(t, filepath.Join(root, c.dir), c.stdin, c.args...)
		got := runTerminalStdin(t, filepath.Join(root, c.dir), c.stdin, tty, c.args...)
		if stripped := escapes.ReplaceAllString(got.stdout, ""); stripped != plain.stdout || got.code != plain.code || got.stderr != plain.stderr {
			t.Errorf("%s: without the escapes, the output differs:\n%s", c.name, stripped)
		}
		has := strings.Contains(got.stdout, "\x1b[")
		if colored[c.name] && !has {
			t.Errorf("%s is not colored:\n%s", c.name, got.stdout)
		}
		if has && (c.json || len(c.args) > 0 && (c.args[0] == "export" || c.args[0] == "cat")) {
			t.Errorf("%s is colored:\n%s", c.name, got.stdout)
		}
	}
	mustContain(t, "check", runTerminal(t, root, tty, "check", "--undocumented").stdout, "\x1b[33mwarning\x1b[0m[undocumented]",
		"0 errors, \x1b[33m4 warnings\x1b[0m\n")
	mustContain(t, "status", runTerminal(t, root, tty, "status").stdout, "\x1b[32mpfd\x1b[0m        src/main.rs\n")
	mustContain(t, "search", runTerminal(t, root, tty, "search", "ワーカー").stdout, "\x1b[4;34msrc/agent/pool.rs\x1b[0m  file — \x1b[1;31mワーカー\x1b[0mのプール。\n")

	for _, c := range []struct {
		env  map[string]string
		args []string
		want bool
	}{
		{tty, []string{"src", "--color=never"}, false},
		{map[string]string{"TERM": "xterm", "NO_COLOR": "1", "DIRDOC_PAGER": "cat"}, []string{"src"}, false},
		{map[string]string{"TERM": "dumb"}, []string{"src"}, false},
		{map[string]string{"DIRDOC_PAGER": "cat"}, []string{"src"}, runtime.GOOS == "windows"}, // no TERM
		{map[string]string{"TERM": "xterm", "NO_COLOR": "1", "DIRDOC_PAGER": "cat"}, []string{"src", "--color=always"}, true},
	} {
		if got := runTerminal(t, root, c.env, c.args...); strings.Contains(got.stdout, "\x1b[") != c.want {
			t.Errorf("%v %v: colored = %v", c.env, c.args, !c.want)
		}
	}
	if got := run(t, root, "", "src", "--color=always"); !strings.Contains(got.stdout, "\x1b[") {
		t.Error("--color=always does not color a pipe")
	}
	if got := run(t, root, "", "src", "--color=sometimes"); got.code != 2 || !strings.Contains(got.stderr, `unknown --color "sometimes"`) {
		t.Errorf("--color=sometimes: %+v", got)
	}
}

func TestPager(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test pager is a shell script")
	}
	root := newFixture(t, "")
	dir := t.TempDir()
	paged := filepath.Join(dir, "paged")
	pager := filepath.Join(dir, "pager")
	script := "#!/bin/sh\n{ echo \"LESS=$LESS LV=$LV LESSCHARSET=$LESSCHARSET args=$*\"; cat; } > '" + paged + "'\necho paged\n"
	if err := os.WriteFile(pager, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	pagedOutput := func() (string, bool) {
		t.Helper()
		data, err := os.ReadFile(paged)
		os.Remove(paged)
		return string(data), err == nil
	}
	tty := map[string]string{"TERM": "xterm", "DIRDOC_PAGER": pager + " -x"}

	r := runTerminal(t, root, tty, "src/main.rs")
	out, ok := pagedOutput()
	if r.code != 0 || r.stdout != "paged\n" || !ok {
		t.Fatalf("paged view: %+v (pager ran: %v)", r, ok)
	}
	head, view, _ := strings.Cut(out, "\n")
	if want := "LESS=FRX LV=-c LESSCHARSET=utf-8 args=-x"; head != want {
		t.Errorf("pager environment = %q, want %q", head, want)
	}
	if plain := run(t, root, "", "src/main.rs").stdout; escapes.ReplaceAllString(view, "") != plain || !strings.Contains(view, "\x1b[") {
		t.Errorf("paged output:\n%s", view)
	}

	// A LESS, LV or LESSCHARSET of the user's is left alone.
	user := map[string]string{"TERM": "xterm", "PAGER": pager, "LESS": "-i", "LV": "", "LESSCHARDEF": "x"}
	runTerminal(t, root, user, "src/main.rs")
	out, _ = pagedOutput()
	if head, _, _ := strings.Cut(out, "\n"); head != "LESS="+os.Getenv("LESS")+" LV="+os.Getenv("LV")+" LESSCHARSET="+os.Getenv("LESSCHARSET")+" args=" {
		t.Errorf("the user's settings were replaced: %q", head)
	}

	for _, c := range []struct {
		name string
		env  map[string]string
		args []string
	}{
		{"error", tty, []string{"nope.rs"}},
		{"--no-pager", tty, []string{"src/main.rs", "--no-pager"}},
		{"export to a file", tty, []string{"export", "src/main.rs", "-o", filepath.Join(dir, "x.toml")}},
		{"a writing command", tty, []string{"touch", "src/main.rs"}},
		{"DIRDOC_PAGER=cat", map[string]string{"TERM": "xterm", "DIRDOC_PAGER": "cat", "PAGER": pager}, []string{"src"}},
		{"DIRDOC_PAGER empty", map[string]string{"TERM": "xterm", "DIRDOC_PAGER": "", "PAGER": pager}, []string{"src"}},
		{"TERM=dumb", map[string]string{"TERM": "dumb", "PAGER": pager}, []string{"src"}},
	} {
		r := runTerminal(t, root, c.env, c.args...)
		if _, ran := pagedOutput(); ran || strings.Contains(r.stdout, "paged") {
			t.Errorf("%s: the pager ran: %+v", c.name, r)
		}
		if c.name == "error" && (r.code != 1 || r.stdout != "" || !strings.Contains(r.stderr, "nope.rs: no such file")) {
			t.Errorf("error: %+v", r)
		}
		if c.name == "--no-pager" && !strings.HasPrefix(r.stdout, "\x1b[") {
			t.Errorf("--no-pager: %+v", r)
		}
	}

	r = runTerminal(t, root, map[string]string{"TERM": "xterm", "DIRDOC_PAGER": filepath.Join(dir, "missing")}, "src/main.rs", "--color=never")
	if r.code != 0 || r.stdout != run(t, root, "", "src/main.rs").stdout || !strings.Contains(r.stderr, "dirdoc: warning: cannot run the pager") {
		t.Errorf("a pager that cannot run: %+v", r)
	}
	r = runTerminal(t, root, map[string]string{"TERM": "xterm", "DIRDOC_PAGER": "missing-pager \"$HOME\""}, "src/main.rs", "--color=never")
	if r.code != 0 || !strings.Contains(r.stdout, "# src/main.rs") {
		t.Errorf("a shell pager that is not found: %+v", r)
	}
}

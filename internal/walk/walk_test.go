package walk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitignorePatterns(t *testing.T) {
	pats := parsePatterns(strings.Split(`
# comment
*.log
!keep.log
build/
/root-only
docs/**/draft.md
a?c
[ab]x
`, "\n"))
	tests := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"x.log", false, true},
		{"sub/x.log", false, true},
		{"keep.log", false, false},
		{"build", true, true},
		{"build", false, false},
		{"sub/build", true, true},
		{"root-only", false, true},
		{"sub/root-only", false, false},
		{"docs/draft.md", false, true},
		{"docs/a/b/draft.md", false, true},
		{"abc", false, true},
		{"abbc", false, false},
		{"ax", false, true},
		{"cx", false, false},
	}
	for _, tt := range tests {
		_, got := match(pats, tt.path, tt.isDir)
		if got != tt.want {
			t.Errorf("match(%q, dir=%v) = %v, want %v", tt.path, tt.isDir, got, tt.want)
		}
	}
}

func TestIgnorerAndList(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if strings.HasSuffix(rel, "/") {
			os.MkdirAll(p, 0o755)
			return
		}
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, nil, 0o644)
	}
	for _, rel := range []string{"src/main.rs", "src/gen/out.rs", "target/", "node_modules/x/", ".git/", ".dirdoc/", "README.md", "src/.gitignore"} {
		mk(rel)
	}
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte("target/\n"), 0o644)
	os.WriteFile(filepath.Join(root, "src", ".gitignore"), []byte("gen/\n"), 0o644)

	ig := NewIgnorer(root, true, []string{"node_modules/"})
	entries, err := ig.List(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if got := strings.Join(names, ","); got != "src,.gitignore,README.md" {
		t.Errorf("List(root) = %s", got)
	}
	if !ig.Ignored(filepath.Join(root, "src", "gen", "out.rs"), false) {
		t.Error("file under a nested ignored directory is not ignored")
	}

	var dirs []string
	ig.Dirs(root, -1, func(dir string, depth int) error {
		rel, _ := filepath.Rel(root, dir)
		dirs = append(dirs, filepath.ToSlash(rel))
		return nil
	})
	if got := strings.Join(dirs, ","); got != ".,src" {
		t.Errorf("Dirs = %s", got)
	}

	noGit := NewIgnorer(root, false, nil)
	if noGit.Ignored(filepath.Join(root, "target"), true) {
		t.Error("gitignore = false still ignores target/")
	}
}

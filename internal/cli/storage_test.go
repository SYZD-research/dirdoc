package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/store"
)

// storedFormats returns the format of every stored file below root, by path relative to root.
func storedFormats(t *testing.T, root string) map[string]store.Format {
	t.Helper()
	got := map[string]store.Format{}
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(filepath.Dir(p)) != model.DirName {
			return err
		}
		f, err := store.ReadFile(p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		got[filepath.ToSlash(rel)] = f.Format
		return nil
	})
	return got
}

func allIn(t *testing.T, root string, want store.Format) {
	t.Helper()
	formats := storedFormats(t, root)
	if len(formats) != 6 {
		t.Errorf("found %d stored documents, want 6", len(formats))
	}
	for p, f := range formats {
		if f != want {
			t.Errorf("%s is stored as %s, want %s", p, f, want)
		}
	}
}

// TestTOMLStorage stores the fixture as TOML: every view is the same as with zstd.
func TestTOMLStorage(t *testing.T) {
	zstd := newFixture(t, "")
	root := newFixture(t, "[storage]\nformat = \"toml\"\n")
	allIn(t, root, store.TOML)
	data, err := os.ReadFile(filepath.Join(root, "src", model.DirName, "main.rs"+model.PFDSuffix))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, "stored TOML", string(data), "# dirdoc pfd: main.rs\nformat = \"dirdoc/pfd\"\nversion = 1\ntarget = \"main.rs\"\n",
		"\n[generated]\ntarget_hash = \"sha256:")
	for _, c := range goldenCases {
		if c.name == "compile-errors" {
			continue // the compile fails before anything is stored
		}
		a := run(t, filepath.Join(zstd, c.dir), c.stdin, c.args...)
		b := run(t, filepath.Join(root, c.dir), c.stdin, c.args...)
		if a != b {
			t.Errorf("%s differs with TOML storage:\nzstd: %+v\ntoml: %+v", c.name, a, b)
		}
	}
	for _, tg := range manifest(t) {
		exported := run(t, root, "", "export", tg.path)
		if r := run(t, root, exported.stdout, "compile", tg.path, "-"); r.code != 0 || !strings.HasPrefix(r.stdout, "unchanged ") {
			t.Errorf("%s: export then compile changed the document: %+v", tg.path, r)
		}
	}
	if r := run(t, root, "", "check", "--strict"); r.code != 0 {
		t.Errorf("check: %+v", r)
	}

	// git merges stored TOML line by line; a conflict leaves markers in the stored file itself.
	stored := filepath.Join(root, "src", model.DirName, "main.rs"+model.PFDSuffix)
	conflicted := strings.Replace(string(data), "summary = \"標準出力がTTYかを判定する\"\n",
		"<<<<<<< ours\nsummary = \"ours\"\n=======\nsummary = \"theirs\"\n>>>>>>> theirs\n", 1)
	if conflicted == string(data) {
		t.Fatal("the summary to put in conflict was not found")
	}
	os.WriteFile(stored, []byte(conflicted), 0o644)
	r := run(t, root, "", "src/main.rs")
	if r.code != 1 || !strings.Contains(r.stderr, "unresolved merge conflict (line ") {
		t.Errorf("view of a conflicted file: %+v", r)
	}
	if r := run(t, root, "", "cat", stored); r.stdout != conflicted {
		t.Errorf("cat does not show the conflicted file as it is: %+v", r)
	}
	resolved := regexp.MustCompile(`(?s)<<<<<<< ours\n.*?=======\n(.*?)>>>>>>> theirs\n`).ReplaceAllString(conflicted, "$1")
	os.WriteFile(stored, []byte(resolved), 0o644)
	mustContain(t, "resolved by hand", run(t, root, "", "src/main.rs#check_stdout").stdout, "> theirs")
	if r := run(t, root, "", "check", "--strict"); r.code != 0 {
		t.Errorf("check after resolving: %+v", r)
	}
}

func TestMergeDriverKeepsTheFormat(t *testing.T) {
	root := newFixture(t, "[storage]\nformat = \"toml\"\n")
	base, ours, theirs := versions(t, root,
		"[[functions]]\nid = \"check_stdout\"\nsummary = \"ours\"\n",
		"[[types]]\nid = \"Config\"\nsummary = \"theirs\"\n")
	if r := run(t, root, "", "merge-driver", base, ours, theirs, "src/.dirdoc/main.rs.dirdoc_pfd"); r.code != 0 {
		t.Fatalf("merge: %+v", r)
	}
	f, err := store.ReadFile(ours)
	if err != nil || f.Format != store.TOML {
		t.Fatalf("merged file: %v %v", f, err)
	}
	if f.PFD.Functions[1].Summary != "ours" || f.PFD.Types[0].Summary != "theirs" {
		t.Errorf("merged = %+v", f.PFD)
	}
}

func TestConvert(t *testing.T) {
	root := newFixture(t, "")
	write(t, root, "src/main.rs", "fn main() { changed(); }\n") // stale: convert must keep it so
	before := run(t, root, "", "-r", "--full").stdout

	r := run(t, root, "", "convert", "--to", "toml", "--dry-run")
	if r.code != 0 || !strings.Contains(r.stdout, "would convert src/.dirdoc/main.rs.dirdoc_pfd (zstd -> toml)\n") ||
		!strings.HasSuffix(r.stdout, "would convert 6 documents to toml\n") {
		t.Errorf("dry run: %+v", r)
	}
	allIn(t, root, store.Zstd)

	r = run(t, root, "", "convert", "--to", "toml")
	if r.code != 0 || !strings.HasSuffix(r.stdout, "converted 6 documents to toml\n") ||
		r.stderr != "warning: dirdoc.toml stores documents as zstd, so files dirdoc writes later become zstd again "+
			"(set [storage] format = \"toml\" in dirdoc.toml to keep toml)\n" {
		t.Errorf("convert: %+v", r)
	}
	allIn(t, root, store.TOML)
	if after := run(t, root, "", "-r", "--full").stdout; after != before {
		t.Errorf("the views changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	mustContain(t, "check", run(t, root, "", "check").stdout,
		"warning[storage-format] src/.dirdoc/main.rs.dirdoc_pfd: stored as toml, not as zstd ([storage] format in dirdoc.toml)")

	// Without --to, convert follows dirdoc.toml.
	r = run(t, root, "", "convert", "src/agent")
	if r.code != 0 || r.stdout != "converted src/agent/.dirdoc/.dirdoc_dsd (toml -> zstd)\n"+
		"converted src/agent/.dirdoc/pool.rs.dirdoc_pfd (toml -> zstd)\nconverted 2 documents to zstd\n" || r.stderr != "" {
		t.Errorf("convert a directory: %+v", r)
	}
	r = run(t, root, "", "convert", "src/main.rs", "--to", "json")
	if r.code != 0 || r.stdout != "converted src/.dirdoc/main.rs.dirdoc_pfd (toml -> json)\nconverted 1 document to json\n" {
		t.Errorf("convert a file: %+v", r)
	}
	r = run(t, root, "", "convert")
	if r.code != 0 || !strings.HasSuffix(r.stdout, "converted 4 documents to zstd (2 already zstd)\n") {
		t.Errorf("convert back: %+v", r)
	}
	allIn(t, root, store.Zstd)
	if after := run(t, root, "", "-r", "--full").stdout; after != before {
		t.Error("the views changed after converting back")
	}
	if r := run(t, root, "", "check"); strings.Contains(r.stdout, "storage-format") {
		t.Errorf("check after converting back:\n%s", r.stdout)
	}

	if r := run(t, root, "", "convert", "--to", "yaml"); r.code != 2 {
		t.Errorf("unknown format: %+v", r)
	}
	os.Remove(filepath.Join(root, "dirdoc.toml"))
	r = run(t, root, "", "convert", "--to", "json", "src/main.rs")
	mustContain(t, "warning without dirdoc.toml", r.stderr, "warning: documents are stored as zstd by default, so files dirdoc writes later become zstd again")
	if r := run(t, root, "", "convert", "src/cli/args.rs"); r.code != 0 || !strings.Contains(r.stdout, "nothing to convert") {
		t.Errorf("a file without a PFD: %+v", r)
	}
}

func TestConvertKeepsWhatItCannotConvert(t *testing.T) {
	root := newFixture(t, "[storage]\nformat = \"json\"\n")
	// A null that a JSON source put in an x- field has no TOML form.
	main := filepath.Join(root, "src", model.DirName, "main.rs"+model.PFDSuffix)
	data, _ := os.ReadFile(main)
	data = bytes.Replace(data, []byte(`"target": "main.rs",`), []byte(`"target": "main.rs", "x-gone": null,`), 1)
	os.WriteFile(main, data, 0o644)
	broken := filepath.Join(root, "src", "agent", model.DirName, "pool.rs"+model.PFDSuffix)
	os.WriteFile(broken, []byte("# dirdoc pfd: src/agent/pool.rs\nsummary = \"left by a merge\"\n"), 0o644)

	r := run(t, root, "", "convert", "--to", "toml")
	if r.code != 1 || !strings.HasSuffix(r.stdout, "converted 4 documents to toml (2 failed)\n") {
		t.Errorf("convert: %+v", r)
	}
	mustContain(t, "errors", r.stderr, `dirdoc: src/.dirdoc/main.rs.dirdoc_pfd: cannot store as TOML: field "x-gone" holds null`,
		"dirdoc: src/agent/.dirdoc/pool.rs.dirdoc_pfd: the file holds a TOML source")
	if f, err := store.ReadFile(main); err != nil || f.Format != store.JSON {
		t.Errorf("the file that could not be converted: %v %v", f, err)
	}
}

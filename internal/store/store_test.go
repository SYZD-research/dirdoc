package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SYZD-research/dirdoc/internal/config"
	"github.com/SYZD-research/dirdoc/internal/container"
	"github.com/SYZD-research/dirdoc/internal/model"
)

func testPFD() *model.PFD {
	p := &model.PFD{
		Target:     "main.go",
		TargetHash: "sha256:00",
		Summary:    "entry point",
		Functions:  []model.Entry{{ID: "main", Name: "main()", Summary: "starts", Document: "```\n<<<<<<< ours\n```"}},
	}
	p.Overview = model.BuildOverview(p)
	model.AnnotateOverview(&p.Overview, []model.Symbol{{ID: "main", Kind: "function", Line: 3, Signature: "func main()"}})
	return p
}

func TestDecodeTellsTheFormatsApart(t *testing.T) {
	d := &model.DSD{Summary: "s", Entries: []model.DSDEntry{{ID: "root", Document: "d"}}}
	for _, f := range Formats {
		for _, doc := range []struct {
			d *model.DSD
			p *model.PFD
		}{{d, nil}, {nil, testPFD()}} {
			data, err := Encode(doc.d, doc.p, Settings{Format: f, Level: container.LevelFastest})
			if err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			got, err := Decode(data)
			if err != nil {
				t.Fatalf("%s: %v\n%s", f, err, data)
			}
			want, _ := marshal(doc.d, doc.p)
			have, _ := marshal(got.DSD, got.PFD)
			if got.Format != f || !sameJSON(want, have) {
				t.Errorf("%s: decoded as %s:\nwant %s\ngot  %s", f, got.Format, want, have)
			}
		}
	}
	if _, err := Encode(d, nil, Settings{Format: "yaml"}); err == nil {
		t.Error("an unknown format was encoded")
	}
}

func TestDecodeReportsWhatMergesLeave(t *testing.T) {
	stored, err := Encode(nil, testPFD(), Settings{Format: TOML})
	if err != nil {
		t.Fatal(err)
	}
	conflicted := strings.Replace(string(stored), "summary = \"entry point\"\n",
		"<<<<<<< ours\nsummary = \"a\"\n=======\nsummary = \"b\"\n>>>>>>> theirs\n", 1)
	for _, c := range []struct {
		data   string
		line   int
		stored bool
	}{
		{"# dirdoc pfd: src/main.go\nsummary = \"s\"\n", 0, false},
		{"# dirdoc pfd: src/main.go\n<<<<<<< ours\nsummary = \"a\"\n=======\nsummary = \"b\"\n>>>>>>> theirs\n", 2, false},
		{conflicted, 5, true},
	} {
		_, err := Decode([]byte(c.data))
		var se *SourceError
		if !errors.As(err, &se) || se.Line != c.line || se.Stored != c.stored {
			t.Errorf("%q: %#v", c.data, err)
			continue
		}
		if want := "dirdoc compile"; c.stored == strings.Contains(se.Error(), want) {
			t.Errorf("%q: message %q", c.data, se.Error())
		}
	}
	for _, garbage := range []string{"", "hello", "a = 1\n", "\x00\x01"} {
		if _, err := Decode([]byte(garbage)); err != ErrUnknownFormat {
			t.Errorf("%q: %v", garbage, err)
		}
	}
	if _, err := Decode([]byte("# dirdoc pfd: x\nformat = \"dirdoc/pfd\"\nversion = 1\ntarget = [\n")); err == nil ||
		!strings.Contains(err.Error(), "invalid TOML (line 4)") {
		t.Errorf("broken TOML: %v", err)
	}
}

func TestWriteFileSkipsSameContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".dirdoc", "main.go.dirdoc_pfd")
	p := testPFD()
	zstd := Settings{Format: Zstd, Level: container.LevelBest}
	if written, err := WriteFile(path, nil, p, zstd); err != nil || !written {
		t.Fatalf("first write: %v %v", written, err)
	}
	old := time.Now().Add(-time.Hour)
	unchanged := func(what string, s Settings) {
		t.Helper()
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		if written, err := WriteFile(path, nil, p, s); err != nil || written {
			t.Errorf("%s: written=%v err=%v", what, written, err)
		}
		if st, _ := os.Stat(path); !st.ModTime().Equal(old) {
			t.Errorf("%s: the file was touched", what)
		}
	}
	unchanged("same document", zstd)
	unchanged("another level", Settings{Format: Zstd, Level: container.LevelFastest})

	for _, f := range []Format{JSON, TOML} {
		if written, err := WriteFile(path, nil, p, Settings{Format: f}); err != nil || !written {
			t.Errorf("switching to %s: written=%v err=%v", f, written, err)
		}
		if got, err := ReadFile(path); err != nil || got.Format != f {
			t.Errorf("after switching to %s: %v %v", f, got, err)
		}
	}
	// A hand edit that changes only the layout is the same document.
	data, _ := os.ReadFile(path)
	edited := strings.Replace(string(data), "summary = \"entry point\"", "summary   =   'entry point' # edited", 1)
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	unchanged("re-laid-out TOML", Settings{Format: TOML})

	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("temporary files left behind: %v", entries)
	}
}

func TestConfigured(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.Format, cfg.Storage.Level = "toml", "fastest"
	if s := Configured(cfg); s.Format != TOML || s.Level != container.LevelFastest || !s.Format.Valid() || Format("none").Valid() {
		t.Errorf("Configured = %+v", s)
	}
}

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil || !cfg.Compress() || cfg.Lint.SummaryMax.Entry != 120 {
		t.Fatalf("defaults: %+v %v", cfg, err)
	}
	cfg, err = Load(write(t, "[storage]\ncompression = \"none\"\n[lint]\nsummary_max = { dsd = 50 }\n"))
	if err != nil || cfg.Compress() || cfg.Lint.SummaryMax.DSD != 50 || cfg.Lint.SummaryMax.PFD != 400 {
		t.Fatalf("partial settings: %+v %v", cfg, err)
	}
	if cfg, err := Load(write(t, Template)); err != nil || cfg.Storage.Level != "best" {
		t.Fatalf("template: %+v %v", cfg, err)
	}
	cfg, err = Load(write(t, "[template]\nfunction = [\"## Errors \", \"Example\"]\n"))
	if err != nil || strings.Join(cfg.Template.Headings("function"), "|") != "Errors|Example" || cfg.Template.Headings("dsd-entry") != nil {
		t.Fatalf("[template]: %+v %v", cfg.Template, err)
	}
	for _, bad := range []string{
		"[storage]\ncompresion = \"none\"\n",
		"[storage]\ncompression = \"gzip\"\n",
		"[show]\nformat = \"html\"\n",
		"[lint]\nsummary_max = { dsd = -1 }\n",
		"[template]\nfunction = [\"\"]\n",
		"[template]\ntype = [\"##\"]\n",
		"[template]\nfile = [\"Usage\"]\n",
	} {
		if _, err := Load(write(t, bad)); err == nil || !strings.Contains(err.Error(), FileName) {
			t.Errorf("%q: error %v", bad, err)
		}
	}
}

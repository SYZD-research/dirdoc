// Package config loads dirdoc.toml, the optional project settings file that also marks the project root.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/pelletier/go-toml/v2"
)

// FileName is the name of the settings file.
const FileName = "dirdoc.toml"

// Config is the content of dirdoc.toml.
type Config struct {
	Storage Storage `toml:"storage"`
	Walk    Walk    `toml:"walk"`
	Lint    Lint    `toml:"lint"`
	Show    Show    `toml:"show"`
}

type Storage struct {
	Compression string `toml:"compression"`
	Level       string `toml:"level"`
}

type Walk struct {
	Gitignore bool     `toml:"gitignore"`
	Ignore    []string `toml:"ignore"`
}

type Lint struct {
	SummaryMax SummaryMax `toml:"summary_max"`
}

// SummaryMax holds the summary lengths (in code points) above which a warning is reported. 0 disables the check.
type SummaryMax struct {
	DSD   int `toml:"dsd"`
	PFD   int `toml:"pfd"`
	Entry int `toml:"entry"`
}

type Show struct {
	Format string `toml:"format"`
}

// Default returns the settings used when dirdoc.toml is absent.
func Default() *Config {
	return &Config{
		Storage: Storage{Compression: "zstd", Level: "best"},
		Walk:    Walk{Gitignore: true},
		Lint:    Lint{SummaryMax: SummaryMax{DSD: 200, PFD: 400, Entry: 120}},
		Show:    Show{Format: "md"},
	}
}

// Compress reports whether documents are stored zstd-compressed.
func (c *Config) Compress() bool { return c.Storage.Compression != "none" }

// Load reads path. A missing file yields the defaults; unknown keys and invalid values are errors.
func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		var de *toml.DecodeError
		if errors.As(err, &de) {
			row, col := de.Position()
			return nil, fmt.Errorf("%s:%d:%d: %s", path, row, col, de.Error())
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) validate() error {
	switch c.Storage.Compression {
	case "zstd", "none":
	default:
		return fmt.Errorf("storage.compression must be \"zstd\" or \"none\", not %q", c.Storage.Compression)
	}
	switch c.Storage.Level {
	case "fastest", "default", "better", "best":
	default:
		return fmt.Errorf("storage.level must be one of fastest, default, better, best, not %q", c.Storage.Level)
	}
	switch c.Show.Format {
	case "md", "json":
	default:
		return fmt.Errorf("show.format must be \"md\" or \"json\", not %q", c.Show.Format)
	}
	m := c.Lint.SummaryMax
	if m.DSD < 0 || m.PFD < 0 || m.Entry < 0 {
		return errors.New("lint.summary_max values must not be negative")
	}
	return nil
}

// Template is the dirdoc.toml written by `dirdoc init`.
const Template = `# dirdoc project settings. This file also marks the project root.

[storage]
compression = "zstd"   # "zstd" | "none"
level = "best"         # "fastest" | "default" | "better" | "best"

[walk]
gitignore = true       # follow .gitignore files
ignore = []            # extra patterns to exclude (gitignore syntax), e.g. ["target/", "node_modules/"]

[lint]
summary_max = { dsd = 200, pfd = 400, entry = 120 }   # warn when a summary is longer (Unicode code points)

[show]
format = "md"          # default output format: "md" | "json"
`

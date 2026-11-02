// Package store reads and writes .dirdoc_* files in the three storage formats of design §4: JSON
// compressed with zstd, indented JSON, and TOML. Reading tells the formats apart by their content,
// so a project may mix them.
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/SYZD-research/dirdoc/internal/config"
	"github.com/SYZD-research/dirdoc/internal/container"
	"github.com/SYZD-research/dirdoc/internal/model"
	"github.com/SYZD-research/dirdoc/internal/source"
)

// Format is how a .dirdoc_* file is stored.
type Format string

const (
	Zstd Format = "zstd" // JSON compressed with zstd, the default
	JSON Format = "json" // indented JSON
	TOML Format = "toml" // the TOML of design §5.4
)

// Formats lists the formats in the order the documentation gives them.
var Formats = []Format{Zstd, JSON, TOML}

// Valid reports whether f is one of the formats.
func (f Format) Valid() bool { return f == Zstd || f == JSON || f == TOML }

// Settings say how documents are written: [storage] in dirdoc.toml.
type Settings struct {
	Format Format
	Level  container.Level // zstd only
}

// Configured returns the settings of dirdoc.toml.
func Configured(c *config.Config) Settings {
	return Settings{Format: Format(c.Storage.Format), Level: container.Level(c.Storage.Level)}
}

// File is a decoded .dirdoc_* file: a DSD or a PFD, and the format it was stored in.
type File struct {
	Kind   model.Kind
	DSD    *model.DSD
	PFD    *model.PFD
	Format Format
}

// ErrUnknownFormat is returned for data that is none of the formats.
var ErrUnknownFormat = errors.New("not a dirdoc file (neither zstd, JSON nor TOML)")

// SourceError reports a .dirdoc_* file that a merge left as a TOML source, or with git conflict
// markers, instead of a stored document.
type SourceError struct {
	Line   int  // the first conflict marker, 0 when there is none
	Stored bool // a stored TOML document that only the markers keep from being read
}

func (e *SourceError) Error() string {
	switch {
	case e.Stored:
		return fmt.Sprintf("unresolved merge conflict (line %d): keep one side of each block between the markers "+
			"and delete the markers; the file is then read as it is (`dirdoc check` validates it)", e.Line)
	case e.Line > 0:
		return fmt.Sprintf("the file holds a TOML source with an unresolved merge conflict (line %d): resolve it, "+
			"then store it with `dirdoc compile TARGET FILE`", e.Line)
	}
	return "the file holds a TOML source, not a stored document: resolve the merge conflict in it, " +
		"then store it with `dirdoc compile TARGET FILE`"
}

var storedLine = regexp.MustCompile(`(?m)^format\s*=\s*["']dirdoc/`)

// Decode reads the content of a .dirdoc_* file in any of the formats.
func Decode(data []byte) (*File, error) {
	js, compressed, err := container.Decode(data)
	if err == nil {
		kind, d, p, err := model.Decode(js)
		if err != nil {
			return nil, err
		}
		f := &File{Kind: kind, DSD: d, PFD: p, Format: JSON}
		if compressed {
			f.Format = Zstd
		}
		return f, nil
	}
	if !errors.Is(err, container.ErrUnknownFormat) {
		return nil, err
	}
	if len(data) > container.MaxDecodedSize {
		return nil, fmt.Errorf("file exceeds %d MiB", container.MaxDecodedSize>>20)
	}
	// Both stored TOML and the sources the merge driver leaves start with a "# dirdoc" comment.
	dirdocTOML := bytes.HasPrefix(bytes.TrimLeft(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), " \t\r\n"), []byte("# dirdoc")) ||
		storedLine.Match(data)
	kind, d, p, err := source.ParseStored(data)
	var se *source.SyntaxError
	switch {
	case err == nil:
		return &File{Kind: kind, DSD: d, PFD: p, Format: TOML}, nil
	case !dirdocTOML && (errors.Is(err, source.ErrNotStored) || errors.As(err, &se)):
		return nil, ErrUnknownFormat
	case errors.Is(err, source.ErrNotStored):
		return nil, &SourceError{}
	case errors.As(err, &se):
		if line := source.ConflictMarker(data); line > 0 {
			return nil, &SourceError{Line: line, Stored: storedLine.Match(data)}
		}
	}
	return nil, err
}

// ReadFile reads a .dirdoc_* file.
func ReadFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Decode(data)
}

func marshal(d *model.DSD, p *model.PFD) ([]byte, error) {
	if d != nil {
		return model.Marshal(d)
	}
	return model.Marshal(p)
}

// Encode returns the bytes of a document stored with s. Exactly one of d and p is set.
func Encode(d *model.DSD, p *model.PFD, s Settings) ([]byte, error) {
	switch s.Format {
	case TOML:
		text, err := source.EmitStored(d, p)
		return []byte(text), err
	case Zstd, JSON:
		js, err := marshal(d, p)
		if err != nil {
			return nil, err
		}
		return container.Encode(js, s.Format == Zstd, s.Level)
	}
	return nil, fmt.Errorf("unknown storage format %q", s.Format)
}

// WriteFile stores a document at path. It does nothing and returns false when the file already holds
// the same document in the same format, so that writing again never produces a spurious git diff,
// even after a compression library update or a hand edit that only changed the layout. The write goes
// through a temporary file and a rename, so a crash cannot leave a broken file.
func WriteFile(path string, d *model.DSD, p *model.PFD, s Settings) (bool, error) {
	js, err := marshal(d, p)
	if err != nil {
		return false, err
	}
	if old, err := ReadFile(path); err == nil && old.Format == s.Format {
		if oldJS, err := marshal(old.DSD, old.PFD); err == nil && sameJSON(oldJS, js) {
			return false, nil
		}
	}
	data, err := Encode(d, p, s)
	if err != nil {
		return false, err
	}
	return true, writeAtomic(path, data)
}

// writeAtomic writes data to path through a temporary file in the same directory and a rename.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// sameJSON compares two JSON texts byte for byte once the layout is removed. Unknown fields keep the
// layout of the file they were read from, so it may differ.
func sameJSON(a, b []byte) bool {
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return false
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}

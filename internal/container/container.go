// Package container reads and writes the bytes of .dirdoc_* files: JSON, zstd-compressed by default.
package container

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// MaxDecodedSize bounds the size of a decoded document, so a hostile file cannot exhaust memory.
const MaxDecodedSize = 16 << 20

var zstdMagic = []byte{0x28, 0xB5, 0x2F, 0xFD}

// ErrUnknownFormat is returned for data that is neither zstd nor JSON.
var ErrUnknownFormat = errors.New("not a dirdoc file (neither zstd nor JSON)")

// Level is a compression level name as written in dirdoc.toml.
type Level string

const (
	LevelFastest Level = "fastest"
	LevelDefault Level = "default"
	LevelBetter  Level = "better"
	LevelBest    Level = "best"
)

func (l Level) encoderLevel() (zstd.EncoderLevel, error) {
	switch l {
	case LevelFastest:
		return zstd.SpeedFastest, nil
	case LevelDefault:
		return zstd.SpeedDefault, nil
	case LevelBetter:
		return zstd.SpeedBetterCompression, nil
	case LevelBest, "":
		return zstd.SpeedBestCompression, nil
	}
	return 0, fmt.Errorf("unknown compression level %q", string(l))
}

var (
	decoderOnce sync.Once
	decoder     *zstd.Decoder
	decoderErr  error
)

func getDecoder() (*zstd.Decoder, error) {
	decoderOnce.Do(func() {
		decoder, decoderErr = zstd.NewReader(nil,
			zstd.WithDecoderMaxMemory(MaxDecodedSize),
			zstd.WithDecoderConcurrency(1))
	})
	return decoder, decoderErr
}

// Decode returns the JSON held in data and whether it was compressed.
// The decision is made from the first bytes, so compressed and plain files can be mixed freely.
func Decode(data []byte) (js []byte, compressed bool, err error) {
	if bytes.HasPrefix(data, zstdMagic) {
		dec, err := getDecoder()
		if err != nil {
			return nil, true, err
		}
		out, err := dec.DecodeAll(data, nil)
		if errors.Is(err, zstd.ErrDecoderSizeExceeded) {
			return nil, true, fmt.Errorf("decompressed size exceeds %d MiB", MaxDecodedSize>>20)
		}
		if err != nil {
			return nil, true, fmt.Errorf("zstd: %w", err)
		}
		return out, true, nil
	}
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '{' {
		if len(data) > MaxDecodedSize {
			return nil, false, fmt.Errorf("file exceeds %d MiB", MaxDecodedSize>>20)
		}
		return data, false, nil
	}
	return nil, false, ErrUnknownFormat
}

// Encode wraps js for storage: zstd-compressed, or as indented JSON when compress is false.
func Encode(js []byte, compress bool, level Level) ([]byte, error) {
	if !compress {
		var buf bytes.Buffer
		if err := json.Indent(&buf, js, "", "  "); err != nil {
			return nil, err
		}
		buf.WriteByte('\n')
		return buf.Bytes(), nil
	}
	lvl, err := level.encoderLevel()
	if err != nil {
		return nil, err
	}
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(lvl), zstd.WithEncoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	defer enc.Close()
	return enc.EncodeAll(js, nil), nil
}

// ReadFile reads a .dirdoc_* file and returns its JSON.
func ReadFile(path string) (js []byte, compressed bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	return Decode(data)
}

// WriteFile stores js at path. It does nothing and returns false when the file already holds the
// same JSON in the same container, so that recompiling never produces a spurious git diff.
// The write goes through a temporary file and a rename, so a crash cannot leave a broken file.
func WriteFile(path string, js []byte, compress bool, level Level) (bool, error) {
	if old, err := os.ReadFile(path); err == nil {
		if oldJS, oldCompressed, err := Decode(old); err == nil && oldCompressed == compress && sameJSON(oldJS, js) {
			return false, nil
		}
	}
	data, err := Encode(js, compress, level)
	if err != nil {
		return false, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return false, err
	}
	return true, nil
}

func sameJSON(a, b []byte) bool {
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return false
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}

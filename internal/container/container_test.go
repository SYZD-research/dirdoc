package container

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEncodeDecode(t *testing.T) {
	js := []byte(`{"format":"dirdoc/dsd","version":1,"summary":"日本語"}`)
	for _, compress := range []bool{true, false} {
		data, err := Encode(js, compress, LevelBest)
		if err != nil {
			t.Fatal(err)
		}
		if got := bytes.HasPrefix(data, zstdMagic); got != compress {
			t.Errorf("compress=%v: zstd magic present = %v", compress, got)
		}
		out, compressed, err := Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if compressed != compress || !sameJSON(out, js) {
			t.Errorf("compress=%v: Decode = %s (compressed %v)", compress, out, compressed)
		}
	}
	if _, _, err := Decode([]byte("hello")); err != ErrUnknownFormat {
		t.Errorf("Decode(garbage) error = %v", err)
	}
}

func TestEncodeIsDeterministic(t *testing.T) {
	js := bytes.Repeat([]byte(`{"a":"some repeated text"}`), 1)
	a, _ := Encode(js, true, LevelBest)
	b, _ := Encode(js, true, LevelBest)
	if !bytes.Equal(a, b) {
		t.Error("two encodings of the same JSON differ")
	}
}

func TestDecodeSizeLimit(t *testing.T) {
	big := append([]byte(`{"a":"`), bytes.Repeat([]byte("x"), MaxDecodedSize+1)...)
	big = append(big, `"}`...)
	data, err := Encode(big, true, LevelFastest)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Decode(data); err == nil {
		t.Error("oversized document decoded without error")
	}
}

func TestWriteFileSkipsSameContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".dirdoc", ".dirdoc_dsd")
	js := []byte(`{"format":"dirdoc/dsd","version":1}`)
	written, err := WriteFile(path, js, true, LevelBest)
	if err != nil || !written {
		t.Fatalf("first write: %v %v", written, err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	indented := []byte("{\n  \"format\": \"dirdoc/dsd\",\n  \"version\": 1\n}")
	if written, err := WriteFile(path, indented, true, LevelBest); err != nil || written {
		t.Errorf("same JSON: written=%v err=%v", written, err)
	}
	if st, _ := os.Stat(path); !st.ModTime().Equal(old) {
		t.Error("file was touched although the content is the same")
	}
	if written, _ := WriteFile(path, js, false, LevelBest); !written {
		t.Error("switching to uncompressed storage did not rewrite the file")
	}
	data, _ := os.ReadFile(path)
	if data[0] != '{' {
		t.Errorf("uncompressed file starts with %q", data[0])
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("temporary files left behind: %v", entries)
	}
}

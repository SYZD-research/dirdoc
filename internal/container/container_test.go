package container

import (
	"bytes"
	"encoding/json"
	"testing"
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
		var plain bytes.Buffer
		if err := json.Compact(&plain, out); err != nil || compressed != compress || !bytes.Equal(plain.Bytes(), js) {
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

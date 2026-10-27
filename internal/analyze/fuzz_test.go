package analyze

import (
	"strings"
	"testing"
	"time"

	"github.com/SYZD-research/dirdoc/internal/model"
)

// FuzzFile feeds any text to every analyzer: none may panic or hang, and each symbol must have a
// valid, unique id, a known kind, a line of the file and a one-line signature.
func FuzzFile(f *testing.F) {
	for _, src := range []string{goSource, rustSource, tsSource, jsSource, tsxSource, javaSource, kotlinSource,
		pythonSource, shellSource} {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		lines := strings.Count(src, "\n") + 1
		for _, name := range []string{"a.go", "a.rs", "a.ts", "a.tsx", "a.js", "A.java", "a.kt", "a.py", "a.sh"} {
			done := make(chan []model.Symbol, 1)
			go func() { done <- File(name, []byte(src)) }()
			var syms []model.Symbol
			select {
			case syms = <-done:
			case <-time.After(5 * time.Second):
				t.Fatalf("%s: no result after 5s", name)
			}
			seen := map[string]bool{}
			for _, s := range syms {
				switch {
				case !model.ValidID(s.ID), seen[s.ID]:
					t.Errorf("%s: id %q", name, s.ID)
				case s.Kind != KindType && s.Kind != KindFunction && s.Kind != KindVariable:
					t.Errorf("%s: %s: kind %q", name, s.ID, s.Kind)
				case s.Line < 1 || s.Line > lines:
					t.Errorf("%s: %s: line %d of %d", name, s.ID, s.Line, lines)
				case strings.ContainsAny(s.Signature, "\n\r"):
					t.Errorf("%s: %s: signature %q", name, s.ID, s.Signature)
				}
				seen[s.ID] = true
			}
		}
	})
}

// Package diag defines diagnostics (errors and warnings with a code and a location) and the
// validators that produce them for stored documents (design §12).
package diag

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// Severity of a diagnostic.
type Severity int

const (
	Error Severity = iota
	Warning
)

func (s Severity) String() string {
	if s == Error {
		return "error"
	}
	return "warning"
}

func (s Severity) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

// Diagnostic codes. The first group are errors, the second warnings (design §12).
const (
	CodeParse          = "parse"
	CodeVersion        = "version"
	CodeIDInvalid      = "id-invalid"
	CodeIDDuplicate    = "id-duplicate"
	CodeTreeSyntax     = "tree-syntax"
	CodeTreeLink       = "tree-link"
	CodeRefLocal       = "ref-local"
	CodeRefOutside     = "ref-outside"
	CodeTargetMismatch = "target-mismatch"
	CodeMerge          = "merge"

	CodeRefBroken     = "ref-broken"
	CodeOrphanPFD     = "orphan-pfd"
	CodeTreeMissing   = "tree-missing"
	CodeTreeType      = "tree-type"
	CodeTreeUnlisted  = "tree-unlisted"
	CodeDSDUnlinked   = "dsd-unlinked"
	CodeSummaryEmpty  = "summary-empty"
	CodeSummaryLong   = "summary-long"
	CodeUnknownFile   = "unknown-file"
	CodeSourceIgnored = "source-ignored"

	// Reported only when asked for (check --stale, check --undocumented) or configured ([template]).
	CodeStale           = "stale"
	CodeUndocumented    = "undocumented"
	CodeTemplateHeading = "template-heading"
)

// Diagnostic is one problem. Path is a dirdoc location (a path or a ref such as src/main.rs#main);
// File and Line point into a source file when the problem was found while compiling one.
type Diagnostic struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Path     string   `json:"path,omitempty"`
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	Message  string   `json:"message"`
}

// Location returns where the problem is, preferring the source position.
func (d Diagnostic) Location() string {
	if d.File == "" {
		return d.Path
	}
	loc := d.File
	if d.Line > 0 {
		loc += ":" + strconv.Itoa(d.Line)
	}
	if d.Path != "" {
		loc += " (" + d.Path + ")"
	}
	return loc
}

func (d Diagnostic) String() string {
	if loc := d.Location(); loc != "" {
		return fmt.Sprintf("%s[%s] %s: %s", d.Severity, d.Code, loc, d.Message)
	}
	return fmt.Sprintf("%s[%s]: %s", d.Severity, d.Code, d.Message)
}

// List is a list of diagnostics.
type List []Diagnostic

// Add appends a diagnostic.
func (l *List) Add(sev Severity, code, path, format string, args ...any) *Diagnostic {
	*l = append(*l, Diagnostic{Severity: sev, Code: code, Path: path, Message: fmt.Sprintf(format, args...)})
	return &(*l)[len(*l)-1]
}

// Errorf appends an error.
func (l *List) Errorf(code, path, format string, args ...any) *Diagnostic {
	return l.Add(Error, code, path, format, args...)
}

// Warnf appends a warning.
func (l *List) Warnf(code, path, format string, args ...any) *Diagnostic {
	return l.Add(Warning, code, path, format, args...)
}

// Count returns the number of errors and warnings.
func (l List) Count() (errors, warnings int) {
	for _, d := range l {
		if d.Severity == Error {
			errors++
		} else {
			warnings++
		}
	}
	return
}

// HasErrors reports whether l contains an error.
func (l List) HasErrors() bool {
	e, _ := l.Count()
	return e > 0
}

// Failed reports whether l should fail a command: any error, or any warning in strict mode.
func (l List) Failed(strict bool) bool {
	e, w := l.Count()
	return e > 0 || strict && w > 0
}

// SortByLine orders diagnostics by source line. One without a line stays right after the
// diagnostic it followed.
func (l List) SortByLine() {
	type keyed struct {
		key int
		d   Diagnostic
	}
	ks := make([]keyed, len(l))
	key := 0
	for i, d := range l {
		if d.Line > 0 {
			key = d.Line
		}
		ks[i] = keyed{key, d}
	}
	sort.SliceStable(ks, func(i, j int) bool { return ks[i].key < ks[j].key })
	for i := range ks {
		l[i] = ks[i].d
	}
}

// Dedup removes repeated diagnostics while keeping the first occurrence.
func (l List) Dedup() List {
	seen := map[Diagnostic]bool{}
	var out List
	for _, d := range l {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

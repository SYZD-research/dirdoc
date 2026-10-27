// Package analyze finds the symbols a source file declares, so that compile can give the overview
// line numbers and signatures and list the symbols no entry documents (design §5.3, §10.6). Go is
// parsed with go/parser; Rust, TypeScript, JavaScript, Java, Kotlin, Python and POSIX shell are read
// by small scanners of their own, which understand comments, strings and nesting but no more of the
// grammar than declarations need.
package analyze

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/SYZD-research/dirdoc/internal/model"
)

// Symbol kinds, named like the PFD sections they belong in.
const (
	KindType     = "type"
	KindFunction = "function"
	KindVariable = "variable"
)

// extensions maps file extensions to the languages dirdoc analyzes. The names double as Markdown
// code fence languages.
var extensions = map[string]string{
	".go":   "go",
	".rs":   "rust",
	".ts":   "typescript",
	".tsx":  "typescript",
	".mts":  "typescript",
	".cts":  "typescript",
	".js":   "javascript",
	".jsx":  "javascript",
	".mjs":  "javascript",
	".cjs":  "javascript",
	".java": "java",
	".kt":   "kotlin",
	".kts":  "kotlin",
	".py":   "python",
	".pyi":  "python",
	".sh":   "sh",
	".bash": "sh",
}

// Language returns the language dirdoc analyzes the file named name as ("go", "rust", ...), judged
// by its extension, or "" for none.
func Language(name string) string {
	return extensions[strings.ToLower(filepath.Ext(name))]
}

// interpreters maps the program of a "#!" line to a language, for scripts without an extension.
var interpreters = map[string]string{
	"sh": "sh", "bash": "sh", "dash": "sh", "ash": "sh", "ksh": "sh",
	"python": "python", "python2": "python", "python3": "python",
	"node": "javascript", "nodejs": "javascript",
}

// shebang returns the language named by the "#!" line of src, or "".
func shebang(src []byte) string {
	if !bytes.HasPrefix(src, []byte("#!")) {
		return ""
	}
	line, _, _ := bytes.Cut(src[2:], []byte("\n"))
	fields := strings.Fields(string(line))
	if len(fields) == 0 {
		return ""
	}
	prog := filepath.Base(fields[0])
	if prog == "env" {
		prog = ""
		for _, f := range fields[1:] {
			if !strings.HasPrefix(f, "-") && !strings.Contains(f, "=") {
				prog = filepath.Base(f)
				break
			}
		}
	}
	if lang, ok := interpreters[prog]; ok {
		return lang
	}
	if strings.HasPrefix(prog, "python3.") {
		return "python"
	}
	return ""
}

// File returns the symbols the file named name declares, in source order: those at the top level
// and, for languages with classes, their members. It returns nil when dirdoc does not analyze the
// language, and for Go when the file does not parse. A file whose extension names no language
// (a script often has none, or a dot in its name) is recognized by its "#!" line.
func File(name string, src []byte) []model.Symbol {
	lang := Language(name)
	if lang == "" {
		lang = shebang(src)
	}
	switch lang {
	case "go":
		return goFile(name, src)
	case "rust":
		return rustFile(src)
	case "typescript": // JSX only in .tsx, where <T>x is no type assertion
		return jsFile(src, true, strings.EqualFold(filepath.Ext(name), ".tsx"))
	case "javascript":
		return jsFile(src, false, true)
	case "java":
		return javaFile(src)
	case "kotlin":
		return kotlinFile(src)
	case "python":
		return pythonFile(src)
	case "sh":
		return shellFile(src)
	}
	return nil
}

// collector gathers the symbols of a file: valid ids only, each id once (the first declaration,
// such as the first of overloaded methods), signatures on one line and clipped.
type collector struct {
	syms []model.Symbol
	seen map[string]bool
}

func (c *collector) add(id, kind string, line int, sig string) {
	if !model.ValidID(id) || c.seen[id] {
		return
	}
	if c.seen == nil {
		c.seen = map[string]bool{}
	}
	c.seen[id] = true
	c.syms = append(c.syms, model.Symbol{ID: id, Kind: kind, Line: line, Signature: clip(tidy(sig))})
}

// list returns the symbols: an empty list, not nil, when there are none (the file was analyzed).
func (c *collector) list() []model.Symbol {
	if c.syms == nil {
		return []model.Symbol{}
	}
	return c.syms
}

// maxValue is the length above which a value in a signature is abbreviated.
const maxValue = 60

// maxSignature is the length at which a signature is cut.
const maxSignature = 240

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxSignature {
		return s
	}
	return string([]rune(s)[:maxSignature]) + " ..."
}

var spaces = regexp.MustCompile(`\s+`)

// tidy puts a declaration on one line: whitespace collapsed, no space inside brackets, no trailing
// comma before a closing bracket or at the end.
func tidy(s string) string {
	s = strings.TrimSpace(spaces.ReplaceAllString(s, " "))
	for _, r := range [][2]string{{"( ", "("}, {"[ ", "["}, {" )", ")"}, {" ]", "]"}, {",)", ")"}, {",]", "]"}, {", }", " }"}} {
		s = strings.ReplaceAll(s, r[0], r[1])
	}
	return strings.TrimSuffix(s, ",")
}

// Package walk lists and walks directories while honouring .gitignore files and extra ignore patterns.
package walk

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// pattern is one compiled gitignore line.
type pattern struct {
	re      *regexp.Regexp
	negate  bool
	dirOnly bool
}

// parsePatterns compiles gitignore lines. Paths are matched relative to the directory the lines belong to.
func parsePatterns(lines []string) []pattern {
	var out []pattern
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasSuffix(line, `\ `) {
			line = strings.TrimRight(line[:len(line)-2], " ") + `\ `
		} else {
			line = strings.TrimRight(line, " \t")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var p pattern
		switch {
		case strings.HasPrefix(line, "!"):
			p.negate = true
			line = line[1:]
		case strings.HasPrefix(line, `\!`), strings.HasPrefix(line, `\#`):
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			p.dirOnly = true
			line = strings.TrimRight(line, "/")
		}
		if line == "" {
			continue
		}
		if strings.Contains(line, "/") {
			line = strings.TrimPrefix(line, "/")
		} else {
			line = "**/" + line
		}
		re, err := regexp.Compile(globToRegexp(line))
		if err != nil {
			continue
		}
		p.re = re
		out = append(out, p)
	}
	return out
}

// globToRegexp converts a gitignore glob to an anchored regular expression.
func globToRegexp(p string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(p); {
		switch {
		case strings.HasPrefix(p[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 3
		case strings.HasPrefix(p[i:], "/**") && i+3 == len(p):
			b.WriteString("/.*")
			i += 3
		case strings.HasPrefix(p[i:], "**"):
			b.WriteString(".*")
			i += 2
		case p[i] == '*':
			b.WriteString("[^/]*")
			i++
		case p[i] == '?':
			b.WriteString("[^/]")
			i++
		case p[i] == '[':
			j := i + 1
			if j < len(p) && (p[j] == '!' || p[j] == '^') {
				j++
			}
			if j < len(p) && p[j] == ']' {
				j++
			}
			for j < len(p) && p[j] != ']' {
				j++
			}
			if j >= len(p) {
				b.WriteString(`\[`)
				i++
				continue
			}
			class := p[i+1 : j]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i = j + 1
		case p[i] == '\\' && i+1 < len(p):
			r, size := utf8.DecodeRuneInString(p[i+1:])
			b.WriteString(regexp.QuoteMeta(string(r)))
			i += 1 + size
		default:
			r, size := utf8.DecodeRuneInString(p[i:])
			b.WriteString(regexp.QuoteMeta(string(r)))
			i += size
		}
	}
	b.WriteString("$")
	return b.String()
}

// match applies patterns to rel; the last matching pattern decides.
func match(patterns []pattern, rel string, isDir bool) (matched, ignored bool) {
	for i := len(patterns) - 1; i >= 0; i-- {
		p := patterns[i]
		if p.dirOnly && !isDir {
			continue
		}
		if p.re.MatchString(rel) {
			return true, !p.negate
		}
	}
	return false, false
}

// Ignorer decides which paths below a root are excluded.
type Ignorer struct {
	root      string
	gitignore bool
	extra     []pattern
	cache     map[string][]pattern
}

// NewIgnorer returns an Ignorer for root. extra holds gitignore-style patterns relative to root;
// they take precedence over .gitignore files.
func NewIgnorer(root string, gitignore bool, extra []string) *Ignorer {
	return &Ignorer{
		root:      filepath.Clean(root),
		gitignore: gitignore,
		extra:     parsePatterns(extra),
		cache:     map[string][]pattern{},
	}
}

func (ig *Ignorer) gitignoreAt(dir string) []pattern {
	if p, ok := ig.cache[dir]; ok {
		return p
	}
	var pats []pattern
	if data, err := os.ReadFile(filepath.Join(dir, ".gitignore")); err == nil {
		pats = parsePatterns(strings.Split(string(data), "\n"))
	}
	ig.cache[dir] = pats
	return pats
}

func alwaysSkipped(name string) bool { return name == ".git" || name == ".dirdoc" }

// Ignored reports whether abs is excluded: it is .git or .dirdoc, lies inside one, or is matched by
// the extra patterns or a .gitignore file (including through an ignored parent directory).
func (ig *Ignorer) Ignored(abs string, isDir bool) bool {
	rel, err := filepath.Rel(ig.root, abs)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return alwaysSkipped(filepath.Base(abs)) && isDir
	}
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		last := i == len(parts)-1
		if alwaysSkipped(part) && (!last || isDir) {
			return true
		}
		if ig.matchParts(parts[:i+1], !last || isDir) {
			return true
		}
	}
	return false
}

func (ig *Ignorer) matchParts(parts []string, isDir bool) bool {
	if m, ignored := match(ig.extra, strings.Join(parts, "/"), isDir); m {
		return ignored
	}
	if !ig.gitignore {
		return false
	}
	for depth := len(parts) - 1; depth >= 0; depth-- {
		dir := filepath.Join(append([]string{ig.root}, parts[:depth]...)...)
		if m, ignored := match(ig.gitignoreAt(dir), strings.Join(parts[depth:], "/"), isDir); m {
			return ignored
		}
	}
	return false
}

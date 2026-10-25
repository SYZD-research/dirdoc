package source

import (
	"regexp"
	"strings"
)

// The TOML decoder does not report where values are, so a light line scan recovers the lines of
// top-level keys and of each [[table]] and its keys. It only serves diagnostics; when it guesses
// wrong, a message merely lacks its line number.

type keyPos struct {
	line    int // line of "key ="
	content int // first line of the value (the next line for a multi-line string opened at line end)
}

type tableLayout struct {
	line int
	keys map[string]keyPos
}

type layout struct {
	top    map[string]keyPos
	tables map[string][]*tableLayout
}

var (
	arrayTableRe = regexp.MustCompile(`^\[\[\s*([A-Za-z0-9_-]+)\s*\]\]`)
	tableRe      = regexp.MustCompile(`^\[[^\[]`)
	keyRe        = regexp.MustCompile(`^("[^"]*"|'[^']*'|[A-Za-z0-9_-]+)\s*=\s*(.*)$`)
)

func scanLayout(src []byte) *layout {
	lay := &layout{top: map[string]keyPos{}, tables: map[string][]*tableLayout{}}
	cur := lay.top
	inMulti := ""
	for i, line := range strings.Split(string(src), "\n") {
		ln := i + 1
		if inMulti != "" {
			if strings.Contains(line, inMulti) {
				inMulti = ""
			}
			continue
		}
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if m := arrayTableRe.FindStringSubmatch(t); m != nil {
			tl := &tableLayout{line: ln, keys: map[string]keyPos{}}
			lay.tables[m[1]] = append(lay.tables[m[1]], tl)
			cur = tl.keys
			continue
		}
		if tableRe.MatchString(t) {
			cur = map[string]keyPos{}
			continue
		}
		m := keyRe.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		key := strings.Trim(m[1], `"'`)
		kp := keyPos{line: ln, content: ln}
		val := m[2]
		for _, d := range []string{"'''", `"""`} {
			if strings.HasPrefix(val, d) {
				if !strings.Contains(val[3:], d) {
					inMulti = d
					if strings.TrimSpace(val[3:]) == "" {
						kp.content = ln + 1
					}
				}
				break
			}
		}
		if _, dup := cur[key]; !dup {
			cur[key] = kp
		}
	}
	return lay
}

func (l *layout) entry(section string, i int) *tableLayout {
	if ts := l.tables[section]; i < len(ts) {
		return ts[i]
	}
	return &tableLayout{keys: map[string]keyPos{}}
}

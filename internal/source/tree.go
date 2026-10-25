// Package source reads and writes the TOML (or JSON) text that authors edit, including the tree notation.
package source

import (
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/SYZD-research/dirdoc/internal/model"
)

// TreeIssue is a problem found while parsing the tree notation. Line is 1-based within the tree text.
type TreeIssue struct {
	Line    int
	Message string
}

// ParseTree parses the tree notation (design §7) into a tree whose root is the owner directory.
// It also returns the line on which each node was written.
func ParseTree(text string) (*model.TreeNode, map[*model.TreeNode]int, []TreeIssue) {
	type frame struct {
		col      int
		node     *model.TreeNode
		childCol int
	}
	root := &model.TreeNode{Name: ".", Type: model.NodeDir}
	lines := map[*model.TreeNode]int{root: 0}
	implicit := map[*model.TreeNode]bool{}
	stack := []*frame{{col: -1, node: root, childCol: -1}}
	var issues []TreeIssue
	fail := func(line int, format string, args ...any) {
		issues = append(issues, TreeIssue{Line: line, Message: fmt.Sprintf(format, args...)})
	}

	first := true
	for i, raw := range strings.Split(text, "\n") {
		lineNo := i + 1
		raw = strings.TrimRight(raw, "\r")
		if strings.TrimSpace(strings.ReplaceAll(raw, " ", " ")) == "" {
			continue
		}
		if strings.ContainsRune(raw, '\t') {
			fail(lineNo, "tabs are not allowed; indent with spaces")
			continue
		}
		if strings.IndexFunc(raw, unicode.IsControl) >= 0 {
			fail(lineNo, "control characters are not allowed")
			continue
		}
		col, rest := splitIndent(raw)
		name, quoted, rest, err := parseName(rest)
		if err != nil {
			fail(lineNo, "%v", err)
			continue
		}
		link, note, err := parseAnnotations(rest)
		if err != nil {
			fail(lineNo, "%v", err)
			continue
		}

		if first {
			first = false
			if !quoted && (name == "." || name == "./") {
				// The root has no siblings, so lines at its own column are its children too.
				stack[0].col = col - 1
				root.DSD, root.Note = link, note
				continue
			}
		}

		for len(stack) > 1 && stack[len(stack)-1].col >= col {
			stack = stack[:len(stack)-1]
		}
		parent := stack[len(stack)-1]
		if col <= parent.col {
			fail(lineNo, "line is not indented under the root line")
			continue
		}
		if parent.childCol == -1 {
			parent.childCol = col
		} else if parent.childCol != col {
			fail(lineNo, "inconsistent indentation: siblings must start at the same column")
			continue
		}
		if !parent.node.IsDir() {
			fail(lineNo, "%q is a file and cannot have children", parent.node.Name)
			continue
		}

		if !quoted && (name == "..." || name == "…") {
			if link != "" {
				fail(lineNo, "an elision line cannot link to an entry")
				continue
			}
			parent.node.More = true
			continue
		}

		isDir := strings.HasSuffix(name, "/")
		segments := strings.Split(strings.TrimRight(name, "/"), "/")
		if bad := badSegment(segments); bad != "" {
			fail(lineNo, "invalid name %q: %s", name, bad)
			continue
		}
		cur := parent.node
		ok := true
		for k, seg := range segments {
			last := k == len(segments)-1
			typ := model.NodeDir
			if last && !isDir {
				typ = model.NodeFile
			}
			if existing := cur.Child(seg); existing != nil {
				if existing.Type != typ {
					fail(lineNo, "%q is listed both as a file and as a directory", seg)
					ok = false
					break
				}
				if last {
					if !implicit[existing] {
						fail(lineNo, "%q is listed twice", seg)
						ok = false
						break
					}
					delete(implicit, existing)
					lines[existing] = lineNo
				}
				cur = existing
				continue
			}
			n := &model.TreeNode{Name: seg, Type: typ}
			cur.Children = append(cur.Children, n)
			lines[n] = lineNo
			if !last {
				implicit[n] = true
			}
			cur = n
		}
		if !ok {
			continue
		}
		cur.DSD, cur.Note = link, note
		stack = append(stack, &frame{col: col, node: cur, childCol: -1})
	}
	return root, lines, issues
}

// splitIndent strips the leading tree drawing (box characters, ASCII |, ` and the dashes that follow
// them, spaces) and returns the column where the name starts.
func splitIndent(line string) (int, string) {
	col, pos := 0, 0
	var prev rune
	for pos < len(line) {
		r, size := utf8.DecodeRuneInString(line[pos:])
		indent := false
		switch r {
		case ' ', ' ', '│', '├', '└', '─', '┃', '┣', '┗', '━', '|', '`':
			indent = true
		case '-':
			switch prev {
			case '|', '`', '-', '+', '├', '└', '─', '┣', '┗', '━':
				indent = true
			}
		case '+':
			indent = strings.HasPrefix(line[pos+size:], "-")
		}
		if !indent {
			break
		}
		prev = r
		col++
		pos += size
	}
	return col, line[pos:]
}

// parseName reads a quoted or bare name. A bare name ends at whitespace or at "[[".
func parseName(s string) (name string, quoted bool, rest string, err error) {
	if strings.HasPrefix(s, `"`) {
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			switch c := s[i]; {
			case c == '\\' && i+1 < len(s):
				i++
				b.WriteByte(s[i])
			case c == '"':
				name, rest = b.String(), s[i+1:]
				if strings.HasPrefix(rest, "/") {
					name += "/"
					rest = rest[1:]
				}
				return name, true, rest, nil
			default:
				b.WriteByte(c)
			}
		}
		return "", true, "", fmt.Errorf("unterminated quoted name")
	}
	end := len(s)
	for i, r := range s {
		if r == ' ' || r == ' ' || strings.HasPrefix(s[i:], "[[") {
			end = i
			break
		}
	}
	if end == 0 {
		return "", false, "", fmt.Errorf("missing name")
	}
	return s[:end], false, s[end:], nil
}

// parseAnnotations reads the optional [[#id]] link and the optional "# note" after a name.
func parseAnnotations(s string) (link, note string, err error) {
	s = trimSpaces(s)
	if strings.HasPrefix(s, "[[") {
		end := strings.Index(s, "]]")
		if end < 0 {
			return "", "", fmt.Errorf("unterminated link (missing \"]]\")")
		}
		target := strings.TrimSpace(s[2:end])
		if !strings.HasPrefix(target, "#") {
			return "", "", fmt.Errorf("tree links must point to an entry of this DSD, written [[#id]] (got [[%s]])", target)
		}
		link = strings.TrimSpace(target[1:])
		if !model.ValidID(link) {
			return "", "", fmt.Errorf("invalid id %q in link", link)
		}
		s = trimSpaces(s[end+2:])
	}
	if s == "" {
		return link, "", nil
	}
	if !strings.HasPrefix(s, "#") {
		return "", "", fmt.Errorf("unexpected text %q (notes start with \" # \"; quote names that contain spaces)", s)
	}
	return link, strings.TrimSpace(s[1:]), nil
}

func trimSpaces(s string) string { return strings.Trim(s, "  ") }

func badSegment(segments []string) string {
	for _, seg := range segments {
		switch seg {
		case "":
			return "empty path segment"
		case ".", "..":
			return "\".\" and \"..\" are not allowed"
		}
	}
	return ""
}

// Annotator returns the text printed between a node's name and its note.
type Annotator func(n *model.TreeNode, rel string) string

// LinkAnnotator prints the [[#id]] links as written in source files.
func LinkAnnotator(n *model.TreeNode, rel string) string {
	if n.DSD == "" {
		return ""
	}
	return "[[#" + n.DSD + "]]"
}

// FormatTree prints a tree in the tree-command style that ParseTree reads back.
func FormatTree(root *model.TreeNode, annot Annotator) string {
	type line struct{ left, mid, note string }
	lines := []line{{left: "./", mid: annot(root, ""), note: root.Note}}
	var walk func(n *model.TreeNode, prefix, rel string)
	walk = func(n *model.TreeNode, prefix, rel string) {
		count := len(n.Children)
		if n.More {
			count++
		}
		for i, c := range n.Children {
			conn, next := "├── ", "│   "
			if i == count-1 {
				conn, next = "└── ", "    "
			}
			crel := path.Join(rel, c.Name)
			lines = append(lines, line{left: prefix + conn + displayName(c), mid: annot(c, crel), note: c.Note})
			if c.IsDir() {
				walk(c, prefix+next, crel)
			}
		}
		if n.More {
			lines = append(lines, line{left: prefix + "└── ..."})
		}
	}
	walk(root, "", "")

	const maxLeft = 40
	leftW, midW := 0, 0
	for _, l := range lines {
		if l.mid == "" && l.note == "" {
			continue
		}
		if w := utf8.RuneCountInString(l.left); w > leftW && w <= maxLeft {
			leftW = w
		}
		if w := utf8.RuneCountInString(l.mid); w > midW {
			midW = w
		}
	}
	var b strings.Builder
	for _, l := range lines {
		s := l.left
		if l.mid != "" || l.note != "" {
			s = pad(s, leftW) + "  "
		}
		if l.mid != "" {
			s += l.mid
		}
		if l.note != "" {
			if midW > 0 {
				s = pad(s, leftW+2+midW) + "  "
			}
			s += "# " + l.note
		}
		b.WriteString(strings.TrimRight(s, " "))
		b.WriteByte('\n')
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func pad(s string, width int) string {
	if n := utf8.RuneCountInString(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

func displayName(n *model.TreeNode) string {
	name := n.Name
	if n.IsDir() {
		name += "/"
	}
	if !needsQuote(n.Name) {
		return name
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
}

func needsQuote(name string) bool {
	if name == "..." || name == "…" || name == "." {
		return true
	}
	if strings.ContainsAny(name, " \" ") || strings.Contains(name, "[[") || strings.HasPrefix(name, "+-") {
		return true
	}
	r, _ := utf8.DecodeRuneInString(name)
	switch r {
	case '│', '├', '└', '─', '┃', '┣', '┗', '━', '|', '`':
		return true
	}
	return false
}

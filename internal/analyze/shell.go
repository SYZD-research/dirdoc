package analyze

import (
	"strings"
	"unicode/utf8"

	"github.com/SYZD-research/dirdoc/internal/model"
)

// shToken is a word or an operator of a shell script. Quotes and expansions stay inside words.
type shToken struct {
	op   bool // an operator, or a line break ("\n")
	text string
	line int
}

// shOps are the operators of several characters.
var shOps = []string{";;&", ";;", ";&", "&&", "||", "<<<", "<<-", "<<", ">>", "<&", ">&", "<>", ">|", "|&"}

// heredoc is a here-document waiting for the end of its line: its delimiter, and whether leading
// tabs are stripped (<<-).
type heredoc struct {
	delim string
	tabs  bool
}

// lexShell splits a script into words and operators, skipping comments and the bodies of
// here-documents.
func lexShell(src []byte) []shToken {
	s := newScanner(src)
	var toks []shToken
	var pending []heredoc
	for !s.eof() {
		start, line := s.pos, s.line
		switch c := s.src[s.pos]; {
		case c == ' ' || c == '\t' || c == '\r':
			s.pos++
		case c == '\\' && s.peek(1) == '\n': // a line continuation
			s.advance(2)
		case c == '\n':
			toks = append(toks, shToken{op: true, text: "\n", line: line})
			s.advance(1)
			for _, h := range pending {
				s.heredocBody(h)
			}
			pending = nil
		case c == '#': // a comment: only where a word would start
			s.lineComment()
		case strings.IndexByte(";&|()<>", c) >= 0:
			s.punct(shOps)
			op := s.src[start:s.pos]
			toks = append(toks, shToken{op: true, text: op, line: line})
			if op == "<<" || op == "<<-" {
				for s.peek(0) == ' ' || s.peek(0) == '\t' {
					s.pos++
				}
				w := s.pos
				s.shellWord()
				delim := strings.NewReplacer(`'`, "", `"`, "", `\`, "").Replace(s.src[w:s.pos])
				toks = append(toks, shToken{text: s.src[w:s.pos], line: line})
				if delim != "" {
					pending = append(pending, heredoc{delim, op == "<<-"})
				}
			}
		default:
			s.shellWord()
			if s.pos == start { // cannot happen, but never loop forever
				s.pos++
			}
			toks = append(toks, shToken{text: s.src[start:s.pos], line: line})
		}
	}
	return toks
}

// heredocBody moves past the lines of a here-document, up to and including its delimiter line.
func (s *scanner) heredocBody(h heredoc) {
	for !s.eof() {
		end := strings.IndexByte(s.src[s.pos:], '\n')
		line := s.src[s.pos:]
		if end >= 0 {
			line = line[:end]
		}
		if h.tabs {
			line = strings.TrimLeft(line, "\t")
		}
		if end < 0 {
			s.advance(len(s.src) - s.pos)
			return
		}
		s.advance(end + 1)
		if strings.TrimSuffix(line, "\r") == h.delim {
			return
		}
	}
}

// shellWord moves past a word: up to a blank, a line break or an operator outside quotes,
// command substitutions and parameter expansions.
func (s *scanner) shellWord() {
	for !s.eof() {
		switch c := s.src[s.pos]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || strings.IndexByte(";&|()<>", c) >= 0:
			return
		case c == '\\':
			s.advance(2)
		case c == '\'':
			s.advance(1)
			s.until("'")
		case c == '"':
			s.shellDouble()
		case c == '`':
			s.backquote()
		case c == '$' && s.peek(1) == '\'':
			s.advance(1)
			s.quoted('\'', false)
		case c == '$' && s.peek(1) == '(':
			s.advance(1)
			s.shellParen()
		case c == '$' && s.peek(1) == '{':
			s.advance(1)
			s.shellBrace()
		default:
			s.advance(1)
		}
	}
}

// shellDouble moves past a "..." string with its substitutions.
func (s *scanner) shellDouble() {
	s.advance(1)
	for !s.eof() {
		switch c := s.src[s.pos]; {
		case c == '\\':
			s.advance(2)
		case c == '"':
			s.advance(1)
			return
		case c == '`':
			s.backquote()
		case c == '$' && s.peek(1) == '(':
			s.advance(1)
			s.shellParen()
		case c == '$' && s.peek(1) == '{':
			s.advance(1)
			s.shellBrace()
		default:
			s.advance(1)
		}
	}
}

// backquote moves past a `...` command substitution.
func (s *scanner) backquote() {
	s.advance(1)
	for !s.eof() {
		switch s.src[s.pos] {
		case '\\':
			s.advance(2)
		case '`':
			s.advance(1)
			return
		default:
			s.advance(1)
		}
	}
}

// shellParen moves past $(...) or $((...)) from its '(' with the quotes and substitutions inside.
func (s *scanner) shellParen() {
	depth := 0
	for !s.eof() {
		switch c := s.src[s.pos]; {
		case c == '(':
			depth++
			s.advance(1)
		case c == ')':
			depth--
			s.advance(1)
			if depth == 0 {
				return
			}
		case c == '\\':
			s.advance(2)
		case c == '\'':
			s.advance(1)
			s.until("'")
		case c == '"':
			s.shellDouble()
		case c == '`':
			s.backquote()
		default:
			s.advance(1)
		}
	}
}

// shellBrace moves past ${...} from its '{'.
func (s *scanner) shellBrace() {
	depth := 0
	for !s.eof() {
		switch c := s.src[s.pos]; {
		case c == '{':
			depth++
			s.advance(1)
		case c == '}':
			depth--
			s.advance(1)
			if depth == 0 {
				return
			}
		case c == '\\':
			s.advance(2)
		case c == '\'':
			s.advance(1)
			s.until("'")
		case c == '"':
			s.shellDouble()
		case c == '$' && s.peek(1) == '(':
			s.advance(1)
			s.shellParen()
		default:
			s.advance(1)
		}
	}
}

// shReserved are the reserved words after which another command starts.
var shReserved = map[string]bool{"if": true, "then": true, "else": true, "elif": true, "fi": true, "do": true,
	"done": true, "while": true, "until": true, "for": true, "case": true, "esac": true, "in": true,
	"select": true, "time": true, "!": true, "{": true, "}": true}

// shOpeners maps the words that open a compound command to the words that close it.
var shOpeners = map[string]string{"{": "}", "if": "fi", "case": "esac", "for": "done", "while": "done",
	"until": "done", "select": "done"}

// shellName reports whether w can name a shell function: letters, digits, '_' and, as bash allows,
// '-', '.' and ':'.
func shellName(w string) bool {
	if w == "" || isDigit(w[0]) || w[0] == '-' {
		return false
	}
	for _, r := range w {
		if !(r == '_' || r == '-' || r == '.' || r == ':' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9') {
			return false
		}
	}
	return true
}

// assignedName returns NAME for a word NAME=value (or NAME+=value), or "".
func assignedName(w string) string {
	i := strings.IndexByte(w, '=')
	if i <= 0 {
		return ""
	}
	name := strings.TrimSuffix(w[:i], "+")
	if name == "" || isDigit(name[0]) {
		return ""
	}
	for _, r := range name {
		if !(r == '_' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9') {
			return ""
		}
	}
	return name
}

// shellValue prints an assignment word, abbreviating a long value.
func shellValue(w string) string {
	i := strings.IndexByte(w, '=')
	if i < 0 || utf8.RuneCountInString(w[i+1:]) <= maxValue {
		return w
	}
	return w[:i+1] + "..."
}

// shellFile lists the functions a script defines (name() { ... }, and bash's function name) outside
// other functions, and the variables it assigns at its top level, outside functions, conditionals
// and loops: commands made of assignments only, and the assignments of export and readonly (and of
// bash's declare and typeset).
func shellFile(src []byte) []model.Symbol {
	toks := lexShell(src)
	var c collector
	start := true // at the start of a command
	depth := 0    // of the compound commands (if, case, loops, { }) around
	isOp := func(j int, text string) bool { return j < len(toks) && toks[j].op && toks[j].text == text }
	for i := 0; i < len(toks); {
		t := toks[i]
		if t.op {
			start = true
			i++
			continue
		}
		if !start {
			i++
			continue
		}
		switch {
		case t.text == "function" && i+1 < len(toks) && !toks[i+1].op:
			name, j := toks[i+1].text, i+2
			sig := "function " + name
			if isOp(j, "(") && isOp(j+1, ")") {
				j += 2
				sig += "()"
			}
			c.add(name, KindFunction, t.line, sig)
			i = skipShellBody(toks, j)
		case shellName(t.text) && isOp(i+1, "(") && isOp(i+2, ")"):
			c.add(t.text, KindFunction, t.line, t.text+"()")
			i = skipShellBody(toks, i+3)
		case shReserved[t.text]:
			switch t.text {
			case "{", "if", "case", "for", "while", "until", "select":
				depth++
			case "}", "fi", "esac", "done":
				depth--
			}
			i++
		case depth > 0 && assignedName(t.text) != "":
			i, start = i+1, false
		case assignedName(t.text) != "":
			j := i
			for j < len(toks) && !toks[j].op && assignedName(toks[j].text) != "" {
				j++
			}
			if isOp(j, "(") && strings.HasSuffix(toks[j-1].text, "=") && j == i+1 { // an array: a=(x y)
				w, end := shellArray(toks, i)
				c.add(assignedName(t.text), KindVariable, t.line, shellValue(w))
				i, start = end, false
				continue
			}
			if j == len(toks) || toks[j].op && toks[j].text != "(" && toks[j].text != "<" && toks[j].text != ">" {
				for k := i; k < j; k++ {
					c.add(assignedName(toks[k].text), KindVariable, toks[k].line, shellValue(toks[k].text))
				}
			}
			i, start = j, false
		case depth == 0 && (t.text == "export" || t.text == "readonly" || t.text == "declare" || t.text == "typeset"):
			j := i + 1
			for j < len(toks) && !toks[j].op {
				w, next := toks[j].text, j+1
				if isOp(next, "(") && strings.HasSuffix(w, "=") {
					w, next = shellArray(toks, j)
				}
				if name := assignedName(w); name != "" {
					c.add(name, KindVariable, toks[j].line, t.text+" "+shellValue(w))
				}
				j = next
			}
			i, start = j, false
		default:
			i, start = i+1, false
		}
	}
	return c.list()
}

// shellArray returns the array assignment a=(x y) whose "a=" is toks[i], on one line, and the index
// after its ')'.
func shellArray(toks []shToken, i int) (string, int) {
	var words []string
	j := i + 2
	for ; j < len(toks) && !(toks[j].op && toks[j].text == ")"); j++ {
		if !toks[j].op {
			words = append(words, toks[j].text)
		}
	}
	return toks[i].text + "(" + strings.Join(words, " ") + ")", j + 1
}

// skipShellBody returns the index just after the body of a function definition that starts at
// toks[j] (after line breaks): a { } group, a ( ) subshell, or another compound command.
func skipShellBody(toks []shToken, j int) int {
	for j < len(toks) && toks[j].op && toks[j].text == "\n" {
		j++
	}
	if j >= len(toks) {
		return j
	}
	if toks[j].op {
		if toks[j].text != "(" {
			return j
		}
		depth := 0
		for ; j < len(toks); j++ {
			if !toks[j].op {
				continue
			}
			switch toks[j].text {
			case "(":
				depth++
			case ")":
				if depth--; depth == 0 {
					return j + 1
				}
			}
		}
		return j
	}
	if shOpeners[toks[j].text] == "" {
		return j
	}
	depth, start := 0, true
	for ; j < len(toks); j++ {
		t := toks[j]
		switch {
		case t.op:
			start = true
		case !start:
		case shOpeners[t.text] != "":
			depth++
		case t.text == "}" || t.text == "fi" || t.text == "esac" || t.text == "done":
			if depth--; depth == 0 {
				return j + 1
			}
			start = false
		case shReserved[t.text]:
		default:
			start = false
		}
	}
	return j
}

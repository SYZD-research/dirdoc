package model

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxIDLen is the maximum length of an ID in code points.
const MaxIDLen = 128

// ValidID reports whether id uses only letters, digits and _ . : $ - and is 1 to 128 characters long.
func ValidID(id string) bool {
	if id == "" || !utf8.ValidString(id) || utf8.RuneCountInString(id) > MaxIDLen {
		return false
	}
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '_', '.', ':', '$', '-':
			continue
		}
		return false
	}
	return true
}

// DeriveID returns the ID implied by an entry name: everything before the first "(", "<" or "["
// with surrounding spaces removed. It reports false when that is not a valid ID; the author must
// then set the id explicitly instead of dirdoc guessing one.
func DeriveID(name string) (string, bool) {
	s := name
	if i := strings.IndexAny(s, "(<["); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if !ValidID(s) {
		return "", false
	}
	return s, true
}

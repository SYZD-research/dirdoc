package analyze

import (
	"strconv"
	"strings"
	"testing"
)

const goSource = `package demo

import "fmt"

// Kind of a thing.
type Kind int

const (
	KindA Kind = iota + 1
	KindB
	_
)

const Limit = 8

var (
	names        = []string{"a", "b"}
	table, other = load()
	long         = map[string]int{"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6}
)

type Pool[T any] struct {
	items []T
}

type Runner interface{}

type Alias = Pool[int]

func init() {}

func New[T any](n int,
	opts ...string,
) *Pool[T] {
	return nil
}

func (p *Pool[T]) Spawn(n int) error { return fmt.Errorf("x") }

func (Kind) String() string { return "" }

func load() (map[string]int, int) { return nil, 0 }
`

func TestGoSymbols(t *testing.T) {
	syms := File("demo.go", []byte(goSource))
	var got []string
	for _, s := range syms {
		got = append(got, s.Kind+" "+s.ID+" "+strconv.Itoa(s.Line)+": "+s.Signature)
	}
	want := []string{
		"type Kind 6: type Kind int",
		"variable KindA 9: const KindA Kind = iota + 1",
		"variable KindB 10: const KindB Kind",
		"variable Limit 14: const Limit = 8",
		`variable names 17: var names = []string{"a", "b"}`,
		"variable table 18: var table = load()",
		"variable other 18: var other = load()",
		"variable long 19: var long = map[string]int{...}",
		"type Pool 22: type Pool[T any] struct{ ... }",
		"type Runner 26: type Runner interface{}",
		"type Alias 28: type Alias = Pool[int]",
		"function New 32: func New[T any](n int, opts ...string) *Pool[T]",
		"function Pool.Spawn 38: func (p *Pool[T]) Spawn(n int) error",
		"function Kind.String 40: func (Kind) String() string",
		"function load 42: func load() (map[string]int, int)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("symbols:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestUnsupportedAndBroken(t *testing.T) {
	if syms := File("main.c", []byte("int main() {}")); syms != nil {
		t.Errorf("C: %v", syms)
	}
	if syms := File("broken.go", []byte("package x\nfunc (")); syms != nil {
		t.Errorf("syntax error: %v", syms)
	}
	if syms := File("empty.go", []byte("package x\n")); syms == nil || len(syms) != 0 {
		t.Errorf("no declarations: %#v (want an empty, non-nil list)", syms)
	}
	for name, want := range map[string]string{"a.GO": "go", "a.rs": "rust", "a.tsx": "typescript", "a.mjs": "javascript",
		"A.java": "java", "a.kts": "kotlin", "a.pyi": "python", "a.sh": "sh", "a.c": "", "Makefile": ""} {
		if got := Language(name); got != want {
			t.Errorf("Language(%q) = %q, want %q", name, got, want)
		}
	}
	for src, want := range map[string]string{"#!/bin/sh\nf() { :; }\n": "sh", "#!/usr/bin/env -S python3 -u\ndef f(): pass\n": "python",
		"#!/usr/bin/env node\nfunction f() {}\n": "javascript", "#!/usr/bin/perl\nsub f {}\n": ""} {
		for _, name := range []string{"script", "invoke-rc.d"} {
			syms := File(name, []byte(src))
			if (syms != nil && len(syms) == 1) != (want != "") {
				t.Errorf("%s %q: %v (want language %q)", name, src, syms, want)
			}
		}
	}
	if syms := File("notes.md", []byte("# Notes\n")); syms != nil {
		t.Errorf("Markdown: %v", syms)
	}
}

// symbols analyzes src as the file name and prints each symbol as "kind id line: signature".
func symbols(name, src string) []string {
	var out []string
	for _, s := range File(name, []byte(src)) {
		out = append(out, s.Kind+" "+s.ID+" "+strconv.Itoa(s.Line)+": "+s.Signature)
	}
	return out
}

func checkSymbols(t *testing.T, name, src string, want []string) {
	t.Helper()
	got := symbols(name, src)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s symbols:\n%s\nwant:\n%s", name, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

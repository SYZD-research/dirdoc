package analyze

import "testing"

const shellSource = `#!/bin/sh
# Deploy script.
set -eu

PREFIX=/usr/local
VERSION="1.2.3" BUILD=42
readonly MAX_AGENTS=8
export PATH="$PREFIX/bin:$PATH" EDITOR
LONG_VALUE="this is a very long value that goes on and on and on for more than sixty characters"
CC=gcc make all
ARR=(one two three)

usage() {
    cat <<EOF
usage: deploy [options]
fake() { :; }
EOF
}

function bash_style {
    local x=1
    inner() { :; }
}

function with_parens() (
    cd /tmp
)

case "$1" in
    start) STATE=running ;;
    *) usage ;;
esac

if [ -n "${VAR:-}" ]; then
    CONFIGURED=yes
fi

log_msg ()
{
    echo "$(date +%s) $*" # a comment with name() {
}

for f in *.txt; do COUNT=$((COUNT + 1)); done
echo done > /dev/null
cat <<< "here() { string; }"
declare -A MAP=([k]=v [l]=w) PLAIN
if command -v git >/dev/null; then
    in_git() { git rev-parse 2>/dev/null; }
    export IN_GIT=yes
fi
`

func TestShellSymbols(t *testing.T) {
	checkSymbols(t, "deploy.sh", shellSource, []string{
		"variable PREFIX 5: PREFIX=/usr/local",
		`variable VERSION 6: VERSION="1.2.3"`,
		"variable BUILD 6: BUILD=42",
		"variable MAX_AGENTS 7: readonly MAX_AGENTS=8",
		`variable PATH 8: export PATH="$PREFIX/bin:$PATH"`,
		"variable LONG_VALUE 9: LONG_VALUE=...",
		"variable ARR 11: ARR=(one two three)",
		"function usage 13: usage()",
		"function bash_style 20: function bash_style",
		"function with_parens 25: function with_parens()",
		"function log_msg 38: log_msg()",
		"variable MAP 46: declare MAP=([k]=v [l]=w)",
		"function in_git 48: in_git()",
	})
}

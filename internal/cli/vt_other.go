//go:build !windows

package cli

import "os"

// enableVT reports whether the terminal f writes to shows escape sequences, which terminals outside
// Windows always do.
func enableVT(*os.File) bool { return true }

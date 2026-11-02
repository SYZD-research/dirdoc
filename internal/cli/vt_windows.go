//go:build windows

package cli

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableVT turns on the escape sequences of the console f writes to, which Windows consoles need
// before they show colors. It reports whether they are on.
func enableVT(f *os.File) bool {
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}

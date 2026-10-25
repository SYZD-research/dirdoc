// Command dirdoc reads and writes directory and file documentation for AI agents.
package main

import (
	"fmt"
	"os"

	"dirdoc/internal/cli"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dirdoc:", err)
		os.Exit(1)
	}
	app := &cli.App{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Cwd: cwd, Getenv: os.Getenv}
	os.Exit(app.Run(os.Args[1:]))
}

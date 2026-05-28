// Package main wires process-level CLI execution.
package main

import (
	"fmt"
	"os"

	"github.com/contribution-dev/contribution/internal/cli"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	info := cli.BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	}
	if err := cli.Execute(info); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

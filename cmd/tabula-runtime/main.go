package main

import (
	"io"
	"os"

	"github.com/bamanoz/tabula/internal/runtime/hostcmd"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() { os.Exit(run(os.Args[1:], os.Stderr)) }

func run(args []string, stderr io.Writer) int {
	return hostcmd.Run(args, stderr, hostcmd.BuildInfo{
		BinaryName: "tabula-runtime",
		Version:    version,
		Commit:     commit,
		Date:       date,
	})
}

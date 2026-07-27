package main

import (
	"os"

	tabulaapp "github.com/bamanoz/tabula/internal/cli/tabula"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	os.Exit(tabulaapp.Run(os.Args[1:], tabulaapp.BuildInfo{Version: version, Commit: commit, Date: date}))
}

package hostcmd

import (
	"fmt"
	"io"
	"strings"
)

type BuildInfo struct {
	BinaryName string
	Version    string
	Commit     string
	Date       string
}

func normalizeBuildInfo(build BuildInfo) BuildInfo {
	if strings.TrimSpace(build.BinaryName) == "" {
		build.BinaryName = "tabula-runtime"
	}
	if build.Version == "" {
		build.Version = "dev"
	}
	if build.Commit == "" {
		build.Commit = "unknown"
	}
	if build.Date == "" {
		build.Date = "unknown"
	}
	return build
}

func Run(args []string, stderr io.Writer, build BuildInfo) int {
	build = normalizeBuildInfo(build)
	if len(args) == 1 {
		switch args[0] {
		case "--version", "version":
			fmt.Fprintf(stderr, "%s %s (%s) built %s\n", build.BinaryName, build.Version, build.Commit, build.Date)
			return 0
		}
	}
	subcommand := "start"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand = args[0]
		args = args[1:]
	}
	switch subcommand {
	case "start":
		return startCmd(args, stderr)
	case "stdio":
		return stdioCmd(args, stderr)
	case "version":
		fmt.Fprintf(stderr, "%s %s (%s) built %s\n", build.BinaryName, build.Version, build.Commit, build.Date)
		return 0
	case "help", "--help", "-h":
		printUsage(stderr, build.BinaryName)
		return 0
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", subcommand)
		printUsage(stderr, build.BinaryName)
		return 1
	}
}

func printUsage(w io.Writer, binaryName string) {
	fmt.Fprintf(w, "Usage: %s [start] [--config PATH] [--runtime-id ID]\n", binaryName)
	fmt.Fprintf(w, "       %s stdio\n", binaryName)
	fmt.Fprintf(w, "       %s --version\n", binaryName)
}

package tabula

import (
	"fmt"
	"os"
	"strings"

	"github.com/bamanoz/tabula/internal/kernel"
)

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

func normalizeBuildInfo(build BuildInfo) BuildInfo {
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

func Run(args []string, build BuildInfo) int {
	build = normalizeBuildInfo(build)
	// Parse subcommand early.
	subcommand := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand = args[0]
	}

	switch subcommand {
	case "status":
		return statusCmd(args[1:])
	case "tenant":
		return tenantCmd(args[1:])
	case "runtime":
		return runtimeCmd(args[1:], build)
	case "config":
		return configCmd(args[1:])
	case "health":
		return healthCmd(args[1:])
	case "serve":
		serveOpts, code := parseServeFlags(args[1:])
		if code != 0 {
			return code
		}
		return serveCmd(build, serveOpts)
	default:
		// --version is the only flag-only invocation we support.
		if len(args) == 1 && args[0] == "--version" {
			fmt.Printf("tabula %s (%s) built %s\n", build.Version, build.Commit, build.Date)
			return 0
		}
		// --protocol prints the kernel's plugin protocol range as JSON. Used
		// by install scripts to write $TABULA_HOME/PROTOCOL so the distro
		// installer can enforce `requires.protocol_version` offline.
		if len(args) == 1 && args[0] == "--protocol" {
			fmt.Printf("{\"plugin_protocol_min\": %d, \"plugin_protocol_max\": %d}\n",
				kernel.MinPluginProtocolVersion, kernel.MaxPluginProtocolVersion)
			return 0
		}
		// No subcommand — print usage.
		fmt.Fprintf(os.Stderr, "Usage: tabula <command>\n\nCommands:\n  serve    Start the kernel WebSocket server (default)\n  status   Show kernel/runtime/tenant status\n  config   Inspect runtime configuration\n  health   Check installed plugin health\n  tenant   Manage tenants\n  runtime  Manage runtimes\n\nServe flags:\n  --runtime-mode external|managed|disabled\n\nFlags:\n  --version    Show version\n  --protocol   Show kernel plugin protocol range (JSON)\n")
		return 1
	}

}

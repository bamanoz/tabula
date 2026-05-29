package tabula

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
	"github.com/bamanoz/tabula/internal/runtime/trust"
)

// distroCmd dispatches `tabula distro <sub>` for trust management.
//
// Subcommands:
//
//   - trust [<distro-id>] [--yes] [--list]
//   - untrust <distro-id>
//
// Without a distro-id, `trust` resolves the active distro from
// runtime.toml. The `--list` form prints the trust DB and ignores other
// arguments.
func distroCmd(args []string) int {
	if len(args) == 0 {
		printDistroUsage(os.Stderr)
		return 1
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "trust":
		return distroTrustCmd(rest, os.Stdout, os.Stderr, os.Stdin)
	case "untrust":
		return distroUntrustCmd(rest, os.Stdout, os.Stderr)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown distro subcommand %q\n", sub)
		printDistroUsage(os.Stderr)
		return 1
	}
}

func printDistroUsage(w io.Writer) {
	fmt.Fprint(w, "Usage: tabula distro <command>\n\nCommands:\n"+
		"  trust [<distro-id>] [--yes]    Approve a distro's boot tree\n"+
		"  trust --list                   Show all trusted distros\n"+
		"  untrust <distro-id>            Remove a distro's trust record\n",
	)
}

func distroTrustCmd(args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	fs := flag.NewFlagSet("distro trust", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	yes := fs.Bool("yes", false, "skip the [y/N] confirmation prompt")
	listAll := fs.Bool("list", false, "print all trust records instead of trusting")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	tabulaHome := strings.TrimSpace(os.Getenv("TABULA_HOME"))
	if tabulaHome == "" {
		fmt.Fprintln(stderr, "error: TABULA_HOME is not set")
		return 1
	}
	dbPath := filepath.Join(tabulaHome, "state", "trust.json")

	if *listAll {
		return distroTrustList(dbPath, stdout, stderr)
	}

	cfgPath := filepath.Join(tabulaHome, "config", "runtime.toml")
	cfg, cfgErr := runtimeconfig.Load(cfgPath)
	if cfgErr != nil {
		fmt.Fprintf(stderr, "error: load runtime config: %v\n", cfgErr)
		return 1
	}

	distroID := strings.TrimSpace(cfg.Distro.Active)
	distroDir := strings.TrimSpace(cfg.Distro.Dir)
	if narg := fs.NArg(); narg == 1 {
		// Explicit override; users with multiple distros can still trust
		// a non-active one this way if they pre-populated runtime.toml
		// pointing at the right tree.
		distroID = strings.TrimSpace(fs.Arg(0))
		if distroID != cfg.Distro.Active {
			fmt.Fprintf(stderr,
				"error: runtime.toml records active distro %q; refusing to trust %q\n"+
					"  run `tabula-install %s` first to switch the active distro\n",
				cfg.Distro.Active, distroID, distroID,
			)
			return 1
		}
	} else if narg > 1 {
		fmt.Fprintln(stderr, "error: trust takes at most one distro id")
		return 1
	}
	if distroID == "" || distroDir == "" {
		fmt.Fprintln(stderr, "error: no active distro recorded in runtime.toml; run `tabula-install <distro>` first")
		return 1
	}

	digest, err := trust.HashDir(distroDir)
	if err != nil {
		fmt.Fprintf(stderr, "error: hash distro: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Distro:   %s\n", distroID)
	fmt.Fprintf(stdout, "Source:   %s\n", distroDir)
	fmt.Fprintf(stdout, "SHA256:   %s\n", digest)
	if !*yes {
		fmt.Fprint(stdout, "Trust this distro? [y/N]: ")
		reader := bufio.NewReader(stdin)
		line, _ := reader.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(stdout, "Aborted; trust DB not modified.")
			return 1
		}
	}
	if _, err := trust.Approve(dbPath, distroID, distroDir, "user"); err != nil {
		fmt.Fprintf(stderr, "error: approve: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Trusted %s.\n", distroID)
	return 0
}

func distroTrustList(dbPath string, stdout, stderr io.Writer) int {
	db, err := trust.Load(dbPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: load trust db: %v\n", err)
		return 1
	}
	if len(db.Distros) == 0 {
		fmt.Fprintln(stdout, "(no trusted distros)")
		return 0
	}
	ids := make([]string, 0, len(db.Distros))
	for id := range db.Distros {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	out := make(map[string]trust.Record, len(ids))
	for _, id := range ids {
		out[id] = db.Distros[id]
	}
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(stderr, "error: encode: %v\n", err)
		return 1
	}
	return 0
}

func distroUntrustCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: tabula distro untrust <distro-id>")
		return 1
	}
	distroID := strings.TrimSpace(args[0])
	if distroID == "" {
		fmt.Fprintln(stderr, "error: distro id is required")
		return 1
	}
	tabulaHome := strings.TrimSpace(os.Getenv("TABULA_HOME"))
	if tabulaHome == "" {
		fmt.Fprintln(stderr, "error: TABULA_HOME is not set")
		return 1
	}
	dbPath := filepath.Join(tabulaHome, "state", "trust.json")
	db, err := trust.Load(dbPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: load trust db: %v\n", err)
		return 1
	}
	if _, ok := db.Distros[distroID]; !ok {
		fmt.Fprintf(stdout, "(distro %q is not trusted; nothing to do)\n", distroID)
		return 0
	}
	if err := trust.Revoke(dbPath, distroID); err != nil {
		fmt.Fprintf(stderr, "error: revoke: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Revoked trust for %s.\n", distroID)
	return 0
}

// isCheckError extracts the structured trust check error if present.
// Reserved for future CLI surfaces (e.g. `tabula health`).
func isCheckError(err error) (*trust.CheckError, bool) {
	var ce *trust.CheckError
	if errors.As(err, &ce) {
		return ce, true
	}
	return nil, false
}

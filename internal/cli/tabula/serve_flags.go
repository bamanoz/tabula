package tabula

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

type localRuntimeMode string

const (
	localRuntimeModeExternal localRuntimeMode = "external"
	localRuntimeModeManaged  localRuntimeMode = "managed"
	localRuntimeModeDisabled localRuntimeMode = "disabled"
)

type serveOptions struct {
	runtimeMode localRuntimeMode
}

func defaultServeOptions() serveOptions {
	return serveOptions{runtimeMode: localRuntimeModeExternal}
}

func parseServeFlags(args []string) (serveOptions, int) {
	opts := defaultServeOptions()
	if mode := strings.TrimSpace(os.Getenv("TABULA_LOCAL_RUNTIME_MODE")); mode != "" {
		parsed, err := parseLocalRuntimeMode(mode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return opts, 1
		}
		opts.runtimeMode = parsed
	}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	_ = fs.Bool("foreground", true, "run in foreground")
	runtimeModeFlag := fs.String("runtime-mode", "", "local runtime mode: external, managed, or disabled")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return opts, 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "error: unexpected serve argument %q\n", fs.Arg(0))
		return opts, 1
	}
	if strings.TrimSpace(*runtimeModeFlag) != "" {
		parsed, err := parseLocalRuntimeMode(*runtimeModeFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return opts, 1
		}
		opts.runtimeMode = parsed
	}
	return opts, 0
}

func parseLocalRuntimeMode(value string) (localRuntimeMode, error) {
	switch localRuntimeMode(strings.ToLower(strings.TrimSpace(value))) {
	case localRuntimeModeExternal:
		return localRuntimeModeExternal, nil
	case localRuntimeModeManaged:
		return localRuntimeModeManaged, nil
	case localRuntimeModeDisabled:
		return localRuntimeModeDisabled, nil
	default:
		return "", fmt.Errorf("invalid runtime mode %q (want external, managed, or disabled)", value)
	}
}

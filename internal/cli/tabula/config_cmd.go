package tabula

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/bamanoz/tabula/internal/cli/inspect"
)

func configCmd(args []string) int {
	if len(args) == 0 {
		printConfigUsage(os.Stderr)
		return 1
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "inspect":
		return configInspectCmd(rest, os.Stdout, os.Stderr)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown config subcommand %q\n", sub)
		printConfigUsage(os.Stderr)
		return 1
	}
}

func printConfigUsage(w io.Writer) {
	fmt.Fprint(w, "Usage: tabula config <command>\n\nCommands:\n  inspect [--plugin ID] [--tenant ID] [--format text|json]  Inspect runtime config\n")
}

func configInspectCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("config inspect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	pluginID := fs.String("plugin", "", "include effective merged config for plugin id")
	tenantID := fs.String("tenant", "", "tenant overlay id")
	format := fs.String("format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "error: unexpected argument %q\n", fs.Arg(0))
		return 1
	}
	report, err := inspect.Build(inspect.Options{PluginID: strings.TrimSpace(*pluginID), TenantID: strings.TrimSpace(*tenantID)})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "json":
		return printJSON(stdout, stderr, report)
	case "text", "":
		printInspectText(stdout, report)
		return 0
	default:
		fmt.Fprintf(stderr, "error: unsupported format %q\n", *format)
		return 1
	}
}

func healthCmd(args []string) int {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	tenantID := fs.String("tenant", "", "tenant overlay id")
	format := fs.String("format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "error: unexpected argument %q\n", fs.Arg(0))
		return 1
	}
	report, err := inspect.BuildHealth(context.Background(), inspect.Options{TenantID: strings.TrimSpace(*tenantID)})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "json":
		return printJSON(os.Stdout, os.Stderr, report)
	case "text", "":
		printHealthText(os.Stdout, report)
		if healthHasErrors(report) {
			return 1
		}
		return 0
	default:
		fmt.Fprintf(os.Stderr, "error: unsupported format %q\n", *format)
		return 1
	}
}

func printJSON(stdout, stderr io.Writer, value any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		fmt.Fprintf(stderr, "error: encode json: %v\n", err)
		return 1
	}
	return 0
}

func printInspectText(w io.Writer, report inspect.Report) {
	fmt.Fprintln(w, "--- runtime ----------------------------")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "TABULA_HOME\t%s\n", report.Paths.Home)
	fmt.Fprintf(tw, "config_dir\t%s\n", report.Paths.ConfigDir)
	fmt.Fprintf(tw, "state_dir\t%s\n", report.Paths.StateDir)
	fmt.Fprintf(tw, "run_dir\t%s\n", report.Paths.RunDir)
	fmt.Fprintf(tw, "runtime.toml\t%s\n", report.Paths.RuntimeConfig)
	_ = tw.Flush()

	fmt.Fprintln(w, "\n--- kernel -----------------------------")
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, kernel := range report.Runtime.Kernels {
		fmt.Fprintf(tw, "%s\t%s\ttenants=%s\n", kernel.ID, kernel.URL, strings.Join(kernel.Tenants, ","))
	}
	for _, item := range report.Tenants {
		selected := ""
		if item.Selected {
			selected = "active"
		}
		fmt.Fprintf(tw, "tenant\t%s\t%s\n", item.ID, selected)
	}
	_ = tw.Flush()

	fmt.Fprintln(w, "\n--- plugins (from runtime.toml) --------")
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, plugin := range report.Plugins {
		status := "disabled"
		if plugin.Enabled {
			status = "enabled"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", plugin.ID, plugin.PluginDir, status)
	}
	_ = tw.Flush()
	if report.Plugin != nil {
		fmt.Fprintf(w, "\n--- plugin %s config -------------------\n", report.Plugin.ID)
		data, _ := json.MarshalIndent(report.Plugin.Config, "", "  ")
		fmt.Fprintln(w, string(data))
	}
}

func printHealthText(w io.Writer, report inspect.HealthReport) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, plugin := range report.Plugins {
		message := ""
		if len(plugin.Messages) > 0 {
			message = plugin.Messages[0].Text
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", plugin.PluginID, plugin.Status, message)
	}
	_ = tw.Flush()
}

func healthHasErrors(report inspect.HealthReport) bool {
	for _, plugin := range report.Plugins {
		if plugin.Status == "error" {
			return true
		}
	}
	return false
}

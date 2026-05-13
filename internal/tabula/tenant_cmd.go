package tabula

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/bamanoz/tabula/internal/tenant"
)

type tenantCLIRecord struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type tenantShowRecord struct {
	ID                 string   `json:"id"`
	DisplayName        string   `json:"display_name,omitempty"`
	CreatedAt          string   `json:"created_at"`
	Plugins            []string `json:"plugins"`
	Skills             []string `json:"skills"`
	ActiveSessionCount int      `json:"active_session_count"`
}

func tenantCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: tabula tenant <list|create|show|delete>")
		return 1
	}
	sub := args[0]
	args = args[1:]
	switch sub {
	case "list":
		return tenantListCmd(args, os.Stdout)
	case "create":
		return tenantCreateCmd(args)
	case "show":
		return tenantShowCmd(args, os.Stdout)
	case "set":
		return tenantSetCmd(args)
	case "delete":
		return tenantDeleteCmd(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown tenant command %q\n", sub)
		return 1
	}
}

func tenantSetCmd(args []string) int {
	id, workspaceRoot, ok := parseTenantSetArgs(args)
	if !ok {
		fmt.Fprintln(os.Stderr, "usage: tabula tenant set <id> --workspace-root PATH")
		return 1
	}
	store, err := tenantStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if _, exists, err := store.Get(id); err != nil {
		fmt.Fprintf(os.Stderr, "tenant_set_failed: %v\n", err)
		return 1
	} else if !exists {
		fmt.Fprintf(os.Stderr, "tenant_not_found: %s\n", id)
		return 1
	}
	tabulaHome, err := resolveTabulaHome()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tenant_set_failed: %v\n", err)
		return 1
	}
	if err := setTenantWorkspaceRoot(tabulaHome, id, workspaceRoot); err != nil {
		fmt.Fprintf(os.Stderr, "tenant_set_failed: %v\n", err)
		return 1
	}
	return 0
}

func tenantListCmd(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("tenant list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: tabula tenant list [--json]")
		return 1
	}
	store, err := tenantStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	items, err := store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	records := tenantRecords(items)
	if *asJSON {
		data, _ := json.Marshal(records)
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, record := range records {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", record.ID, record.DisplayName, record.CreatedAt)
	}
	_ = tw.Flush()
	return 0
}

func tenantCreateCmd(args []string) int {
	id, opts, ok := parseTenantCreateArgs(args)
	if !ok {
		fmt.Fprintln(os.Stderr, "usage: tabula tenant create <id> [--display-name NAME] [--exists-ok]")
		return 1
	}
	store, err := tenantStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	err = store.Create(tenant.Tenant{ID: id, DisplayName: opts.displayName})
	if err != nil {
		if opts.existsOK && errors.Is(err, tenant.ErrExists) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "tenant_create_failed: %v\n", err)
		return 1
	}
	if tabulaHome, homeErr := resolveTabulaHome(); homeErr == nil {
		if err := refreshTenantRuntimeSurface(tabulaHome, id); err != nil {
			fmt.Fprintf(os.Stderr, "tenant_create_failed: %v\n", err)
			return 1
		}
		if err := seedTenantConfigTemplates(tabulaHome, id); err != nil {
			fmt.Fprintf(os.Stderr, "tenant_create_failed: %v\n", err)
			return 1
		}
		if id == tenant.DefaultID {
			if err := tenant.EnsureDefaultWorkspaceRoot(tabulaHome); err != nil {
				fmt.Fprintf(os.Stderr, "tenant_create_failed: %v\n", err)
				return 1
			}
		}
	}
	return 0
}

func tenantShowCmd(args []string, stdout io.Writer) int {
	id, asJSON, ok := parseTenantShowArgs(args)
	if !ok {
		fmt.Fprintln(os.Stderr, "usage: tabula tenant show <id> [--json]")
		return 1
	}
	store, err := tenantStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	item, ok, err := store.Get(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tenant_show_failed: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "tenant_not_found: %s\n", id)
		return 1
	}
	record := tenantShowRecord{ID: item.ID, DisplayName: item.DisplayName, CreatedAt: formatStatusTime(item.CreatedAt), Plugins: []string{}, Skills: []string{}, ActiveSessionCount: 0}
	if asJSON {
		data, _ := json.Marshal(record)
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintf(stdout, "id: %s\ndisplay_name: %s\ncreated_at: %s\nplugins: 0\nskills: 0\nactive_sessions: 0\n", record.ID, record.DisplayName, record.CreatedAt)
	return 0
}

func tenantDeleteCmd(args []string) int {
	id, force, ok := parseTenantDeleteArgs(args)
	if !ok {
		fmt.Fprintln(os.Stderr, "usage: tabula tenant delete <id> [--force]")
		return 1
	}
	id = strings.TrimSpace(id)
	if id == tenant.DefaultID && !force {
		fmt.Fprintln(os.Stderr, "tenant_delete_failed: default tenant requires --force")
		return 1
	}
	store, err := tenantStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if err := store.Delete(id); err != nil {
		fmt.Fprintf(os.Stderr, "tenant_delete_failed: %v\n", err)
		return 1
	}
	return 0
}

func tenantStore() (*tenant.FSStore, error) {
	tabulaHome, err := resolveTabulaHome()
	if err != nil {
		return nil, err
	}
	return tenant.NewFSStore(tabulaHome), nil
}

func tenantRecords(items []tenant.Tenant) []tenantCLIRecord {
	records := make([]tenantCLIRecord, 0, len(items))
	for _, item := range items {
		records = append(records, tenantCLIRecord{ID: item.ID, DisplayName: item.DisplayName, CreatedAt: formatStatusTime(item.CreatedAt)})
	}
	return records
}

func refreshTenantRuntimeSurface(tabulaHome, tenantID string) error {
	tenantRoot := filepath.Join(tabulaHome, "tenants", tenantID)
	for _, name := range []string{"clients", "templates", "plugins", "skills"} {
		if err := mirrorRuntimeSurface(filepath.Join(tabulaHome, name), filepath.Join(tenantRoot, name)); err != nil {
			return err
		}
	}
	return linkSharedTree(filepath.Join(tabulaHome, "_lib"), filepath.Join(tenantRoot, "_lib"))
}

func mirrorRuntimeSurface(srcDir, dstDir string) error {
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(dstDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dstDir, entry.Name())
		if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	entries, err = os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		target := filepath.Join(dstDir, entry.Name())
		rel, err := filepath.Rel(dstDir, filepath.Join(srcDir, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.Symlink(rel, target); err != nil {
			return err
		}
	}
	return nil
}

func linkSharedTree(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	rel, err := filepath.Rel(filepath.Dir(dst), src)
	if err != nil {
		return err
	}
	return os.Symlink(rel, dst)
}

func seedTenantConfigTemplates(tabulaHome, tenantID string) error {
	configRoot := filepath.Join(tabulaHome, "config")
	entries := []struct {
		src string
		dst string
	}{}
	err := filepath.Walk(configRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info == nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".example") {
			return nil
		}
		rel, err := filepath.Rel(configRoot, path)
		if err != nil {
			return err
		}
		entries = append(entries, struct {
			src string
			dst string
		}{src: path, dst: filepath.Join(tabulaHome, "tenants", tenantID, "config", rel)})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if _, err := os.Stat(entry.dst); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		data, err := os.ReadFile(entry.src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(entry.dst), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(entry.dst, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func setTenantWorkspaceRoot(tabulaHome, tenantID, workspaceRoot string) error {
	if strings.TrimSpace(workspaceRoot) == "" {
		return fmt.Errorf("workspace root is required")
	}
	abs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return err
	}
	path := filepath.Join(tabulaHome, "tenants", tenantID, "config", "tenant.toml")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(data)
	escaped := strings.ReplaceAll(abs, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	line := fmt.Sprintf("project_root = \"%s\"", escaped)
	if strings.Contains(text, "[workspace]") {
		lines := strings.Split(text, "\n")
		inWorkspace := false
		replaced := false
		for i, item := range lines {
			trimmed := strings.TrimSpace(item)
			if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
				inWorkspace = trimmed == "[workspace]"
				continue
			}
			if inWorkspace && strings.HasPrefix(trimmed, "project_root") {
				lines[i] = line
				replaced = true
			}
		}
		if !replaced {
			out := make([]string, 0, len(lines)+1)
			inserted := false
			inWorkspace = false
			for _, item := range lines {
				trimmed := strings.TrimSpace(item)
				if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
					if inWorkspace && !inserted {
						out = append(out, line)
						inserted = true
					}
					inWorkspace = trimmed == "[workspace]"
				}
				out = append(out, item)
			}
			if inWorkspace && !inserted {
				out = append(out, line)
			}
			lines = out
		}
		text = strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	} else {
		if strings.TrimSpace(text) != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += "\n[workspace]\n" + line + "\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o600)
}

type tenantCreateOptions struct {
	displayName string
	existsOK    bool
}

func parseTenantCreateArgs(args []string) (string, tenantCreateOptions, bool) {
	var id string
	var opts tenantCreateOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--exists-ok":
			opts.existsOK = true
		case arg == "--display-name":
			if i+1 >= len(args) {
				return "", opts, false
			}
			i++
			opts.displayName = args[i]
		case strings.HasPrefix(arg, "--display-name="):
			opts.displayName = strings.TrimPrefix(arg, "--display-name=")
		case strings.HasPrefix(arg, "-"):
			return "", opts, false
		default:
			if id != "" {
				return "", opts, false
			}
			id = arg
		}
	}
	return id, opts, id != ""
}

func parseTenantShowArgs(args []string) (string, bool, bool) {
	var id string
	asJSON := false
	for _, arg := range args {
		switch {
		case arg == "--json":
			asJSON = true
		case strings.HasPrefix(arg, "-"):
			return "", false, false
		default:
			if id != "" {
				return "", false, false
			}
			id = arg
		}
	}
	return id, asJSON, id != ""
}

func parseTenantSetArgs(args []string) (string, string, bool) {
	var id string
	var workspaceRoot string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--workspace-root":
			if i+1 >= len(args) {
				return "", "", false
			}
			i++
			workspaceRoot = args[i]
		case strings.HasPrefix(arg, "--workspace-root="):
			workspaceRoot = strings.TrimPrefix(arg, "--workspace-root=")
		case strings.HasPrefix(arg, "-"):
			return "", "", false
		default:
			if id != "" {
				return "", "", false
			}
			id = arg
		}
	}
	return id, workspaceRoot, id != "" && workspaceRoot != ""
}

func parseTenantDeleteArgs(args []string) (string, bool, bool) {
	var id string
	force := false
	for _, arg := range args {
		switch {
		case arg == "--force":
			force = true
		case strings.HasPrefix(arg, "-"):
			return "", false, false
		default:
			if id != "" {
				return "", false, false
			}
			id = arg
		}
	}
	return id, force, id != ""
}

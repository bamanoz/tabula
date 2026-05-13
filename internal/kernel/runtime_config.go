package kernel

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	"github.com/bamanoz/tabula/internal/tenant"
)

const defaultRuntimeID = "local"

type RuntimeDefinition struct {
	ID        string   `toml:"id"`
	Backend   string   `toml:"backend"`
	URL       string   `toml:"url"`
	Socket    string   `toml:"socket"`
	Host      string   `toml:"host"`
	Command   string   `toml:"command"`
	RemoteCmd []string `toml:"remote_cmd"`
	Token     string   `toml:"token"`
	SSHArgs   []string `toml:"ssh_args"`
	CAFile    string   `toml:"ca_file"`
	CertFile  string   `toml:"cert_file"`
	KeyFile   string   `toml:"key_file"`
}

type TenantRuntimeBinding struct {
	AllowedRuntimes []string
	DefaultRuntime  string
}

type globalRuntimeConfigFile struct {
	Runtimes []RuntimeDefinition `toml:"runtime"`
}

type tenantRuntimeConfigFile struct {
	Tenant struct {
		AllowedRuntimes []string `toml:"allowed_runtimes"`
		DefaultRuntime  string   `toml:"default_runtime"`
	} `toml:"tenant"`
}

func LoadRuntimeDefinitions(tabulaHome string) ([]RuntimeDefinition, error) {
	var file globalRuntimeConfigFile
	path := filepath.Join(tabulaHome, "config", "global.toml")
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &file); err != nil {
			return nil, fmt.Errorf("load runtime registry %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if len(file.Runtimes) == 0 {
		return []RuntimeDefinition{{ID: defaultRuntimeID, Backend: "local"}}, nil
	}
	seen := map[string]struct{}{}
	out := make([]RuntimeDefinition, 0, len(file.Runtimes))
	for i, runtime := range file.Runtimes {
		runtime.ID = strings.TrimSpace(runtime.ID)
		runtime.Backend = strings.TrimSpace(runtime.Backend)
		runtime.Host = strings.TrimSpace(runtime.Host)
		runtime.Command = strings.TrimSpace(runtime.Command)
		if err := wire.ValidateRuntimeID(runtime.ID); err != nil {
			return nil, fmt.Errorf("runtime[%d].id: %w", i, err)
		}
		if _, ok := seen[runtime.ID]; ok {
			return nil, fmt.Errorf("duplicate runtime id %q", runtime.ID)
		}
		seen[runtime.ID] = struct{}{}
		switch runtime.Backend {
		case "local", "attach", "ssh", "wss":
		case "":
			return nil, fmt.Errorf("runtime[%d].backend is required", i)
		default:
			return nil, fmt.Errorf("runtime[%d].backend %q is unsupported", i, runtime.Backend)
		}
		if runtime.Backend == "ssh" && runtime.Host == "" {
			return nil, fmt.Errorf("runtime[%d].host is required for ssh backend", i)
		}
		out = append(out, runtime)
	}
	return out, nil
}

func LoadTenantRuntimeBinding(tabulaHome, tenantID string, runtimeIDs map[string]struct{}) (TenantRuntimeBinding, error) {
	if err := tenant.ValidateID(tenantID); err != nil {
		return TenantRuntimeBinding{}, err
	}
	var file tenantRuntimeConfigFile
	path := filepath.Join(tabulaHome, "tenants", tenantID, "config", "tenant.toml")
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &file); err != nil {
			return TenantRuntimeBinding{}, fmt.Errorf("load tenant runtime binding %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return TenantRuntimeBinding{}, err
	}
	allowed := normalizeRuntimeAllowlist(file.Tenant.AllowedRuntimes)
	defaultRuntime := strings.TrimSpace(file.Tenant.DefaultRuntime)
	if defaultRuntime == "" {
		if len(runtimeIDs) == 1 {
			for runtimeID := range runtimeIDs {
				defaultRuntime = runtimeID
			}
		} else {
			return TenantRuntimeBinding{}, fmt.Errorf("tenant %q default_runtime is required when multiple runtimes are configured", tenantID)
		}
	}
	if _, ok := runtimeIDs[defaultRuntime]; !ok {
		return TenantRuntimeBinding{}, fmt.Errorf("tenant %q references unknown runtime %q", tenantID, defaultRuntime)
	}
	wildcard := false
	for i, runtimeID := range allowed {
		if runtimeID == "*" {
			wildcard = true
			continue
		}
		if _, ok := runtimeIDs[runtimeID]; !ok {
			return TenantRuntimeBinding{}, fmt.Errorf("tenant %q allowed_runtimes[%d] references unknown runtime %q", tenantID, i, runtimeID)
		}
	}
	if wildcard && len(allowed) > 1 {
		return TenantRuntimeBinding{}, fmt.Errorf("tenant %q allowed_runtimes cannot mix wildcard with explicit runtime ids", tenantID)
	}
	return TenantRuntimeBinding{AllowedRuntimes: allowed, DefaultRuntime: defaultRuntime}, nil
}

func normalizeRuntimeAllowlist(in []string) []string {
	if len(in) == 0 {
		return []string{"*"}
	}
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return []string{"*"}
	}
	sort.Strings(out)
	return out
}

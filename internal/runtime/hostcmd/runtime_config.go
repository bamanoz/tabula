package hostcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bamanoz/tabula/internal/layout"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy/bare"
	runtimeinstance "github.com/bamanoz/tabula/internal/runtime/instance"
)

func configuredRuntimeID(explicit string, now time.Time) (string, error) {
	if runtimeID := strings.TrimSpace(explicit); runtimeID != "" {
		return runtimeID, nil
	}
	meta, err := runtimeinstance.Ensure(layout.RuntimeInstanceFile(layout.Home()), now)
	if err != nil {
		return "", fmt.Errorf("resolve runtime instance metadata: %w", err)
	}
	return meta.RuntimeID, nil
}

func workerKernelURL(configured string) string {
	if envURL := strings.TrimSpace(os.Getenv("TABULA_URL")); envURL != "" {
		return envURL
	}
	return strings.TrimSpace(configured)
}

func runtimePythonPath(cfg runtimeconfig.Config, tabulaHome string) []string {
	paths := []string{
		filepath.Join(tabulaHome, "distrib", "active", "packages", "python", "src"),
	}
	if cfg.Distro.Dir != "" {
		paths = append(paths, filepath.Join(cfg.Distro.Dir, "packages", "python", "src"))
	}
	return paths
}

func coldWorkerTenantOverrides(in map[string]runtimeconfig.TenantPool) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for tenantID, limits := range in {
		if limits.ColdWorkersMax > 0 {
			out[tenantID] = limits.ColdWorkersMax
		}
	}
	return out
}

func pluginKindDependencies(in map[string]runtimeconfig.PluginKind) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for kind, policy := range in {
		if len(policy.DependsOn) > 0 {
			out[kind] = append([]string(nil), policy.DependsOn...)
		}
	}
	return out
}

func runtimePolicy(cfg runtimeconfig.Config) *bare.Policy {
	commands := make(map[string][]string, len(cfg.Runtimes))
	for name, runtime := range cfg.Runtimes {
		commands[name] = append([]string(nil), runtime.Command...)
	}
	return &bare.Policy{RuntimeCommands: commands}
}

func newManifestStore(cfg runtimeconfig.Config) (*manifest.Store, error) {
	if len(cfg.Tenants) == 0 {
		return manifest.NewSearchStore(cfg.PluginDirs, cfg.SkillDirs)
	}
	catalogs := make(map[string]manifest.SearchDirs, len(cfg.Tenants))
	for _, tenant := range cfg.Tenants {
		catalogs[tenant.ID] = manifest.SearchDirs{PluginDirs: tenant.PluginDirs, SkillDirs: tenant.SkillDirs}
	}
	return manifest.NewTenantStore(catalogs)
}

func runtimePluginDirs(cfg runtimeconfig.Config) []string {
	if len(cfg.Tenants) == 0 {
		return cfg.PluginDirs
	}
	var out []string
	for _, tenant := range cfg.Tenants {
		out = append(out, tenant.PluginDirs...)
	}
	return out
}

func runtimeSkillDirs(cfg runtimeconfig.Config) []string {
	if len(cfg.Tenants) == 0 {
		return cfg.SkillDirs
	}
	var out []string
	for _, tenant := range cfg.Tenants {
		out = append(out, tenant.SkillDirs...)
	}
	return out
}

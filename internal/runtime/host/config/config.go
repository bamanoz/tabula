// Package config loads tabula-runtime daemon configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/bamanoz/tabula/internal/runtime/paths"
)

// Config is the runtime-side configuration file. The list-of-tables shape is
// intentional: M2-02 only starts one kernel connection, but later N:M work must
// not change the schema.
type Config struct {
	Kernels    []Kernel `toml:"kernel"`
	PluginDirs []string `toml:"plugin_dirs"`
	SkillDirs  []string `toml:"skill_dirs"`
	Tenants    []Tenant `toml:"tenant"`
	Pool       Pool     `toml:"pool"`
	Distro     Distro   `toml:"distro"`
}

// Distro records which distro generation the installer activated. The
// installer writes these fields; the kernel treats them as status metadata.
type Distro struct {
	Active string `toml:"active"`
	Dir    string `toml:"dir"`
}

type Tenant struct {
	ID         string   `toml:"id"`
	PluginDirs []string `toml:"plugin_dirs"`
	SkillDirs  []string `toml:"skill_dirs"`
}

type Pool struct {
	ColdWorkersPerTenantMax int                   `toml:"cold_workers_per_tenant_max"`
	Tenants                 map[string]TenantPool `toml:"tenants"`
}

type TenantPool struct {
	ColdWorkersMax int `toml:"cold_workers_max"`
}

// Kernel describes one kernel endpoint that the runtime daemon dials.
type Kernel struct {
	ID                    string   `toml:"id"`
	URL                   string   `toml:"url"`
	TokenFile             string   `toml:"token_file"`
	Tenants               []string `toml:"tenants"`
	CAFile                string   `toml:"ca_file"`
	CertFile              string   `toml:"cert_file"`
	KeyFile               string   `toml:"key_file"`
	TLSInsecureSkipVerify bool     `toml:"tls_insecure_skip_verify"`
}

// DefaultPath returns $TABULA_HOME/config/runtime.toml. The caller is expected
// to use this only when no explicit --config path was provided.
func DefaultPath() (string, error) {
	return paths.RuntimeConfigFile(), nil
}

// Load reads and validates a runtime.toml file.
func Load(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{}, fmt.Errorf("runtime config path is required")
	}
	var cfg Config
	meta, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("load runtime config %s: %w", path, err)
	}
	if meta.IsDefined("pool", "cold_workers_per_tenant_max") && cfg.Pool.ColdWorkersPerTenantMax == 0 {
		return Config{}, fmt.Errorf("pool.cold_workers_per_tenant_max must be >= 1")
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		fields := make([]string, 0, len(undecoded))
		for _, key := range undecoded {
			fields = append(fields, key.String())
		}
		sort.Strings(fields)
		return Config{}, fmt.Errorf("runtime config contains unsupported field(s): %s", strings.Join(fields, ", "))
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save validates and writes a runtime.toml file, creating parent directories as
// needed.
func Save(path string, cfg Config) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("runtime config path is required")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create runtime config dir %s: %w", filepath.Dir(path), err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create runtime config %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("write runtime config %s: %w", path, err)
	}
	return nil
}

// Validate checks the M2-02 runtime-side config shape and expands environment
// variables in path-like fields. It deliberately accepts token_file only; the
// stale inline/path-like token key from early issue text is not a supported
// alias.
func (c *Config) Validate() error {
	if len(c.Kernels) == 0 {
		return fmt.Errorf("runtime config requires at least one [[kernel]] entry")
	}
	for i := range c.Kernels {
		k := &c.Kernels[i]
		k.ID = strings.TrimSpace(k.ID)
		k.URL = os.ExpandEnv(strings.TrimSpace(k.URL))
		k.TokenFile = os.ExpandEnv(strings.TrimSpace(k.TokenFile))
		k.CAFile = os.ExpandEnv(strings.TrimSpace(k.CAFile))
		k.CertFile = os.ExpandEnv(strings.TrimSpace(k.CertFile))
		k.KeyFile = os.ExpandEnv(strings.TrimSpace(k.KeyFile))
		if k.ID == "" {
			return fmt.Errorf("kernel[%d].id is required", i)
		}
		if k.URL == "" {
			return fmt.Errorf("kernel[%d].url is required", i)
		}
		if k.TokenFile == "" {
			return fmt.Errorf("kernel[%d].token_file is required", i)
		}
		if (k.CertFile == "") != (k.KeyFile == "") {
			return fmt.Errorf("kernel[%d].cert_file and key_file must be set together", i)
		}
		if len(k.Tenants) == 0 {
			k.Tenants = []string{"*"}
		}
		wildcard := false
		seenTenants := map[string]struct{}{}
		for j, tenantID := range k.Tenants {
			tenantID = strings.TrimSpace(tenantID)
			if tenantID == "" {
				return fmt.Errorf("kernel[%d].tenants[%d] is empty", i, j)
			}
			if tenantID == "*" {
				wildcard = true
			}
			if _, ok := seenTenants[tenantID]; ok {
				continue
			}
			seenTenants[tenantID] = struct{}{}
			k.Tenants[j] = tenantID
		}
		if wildcard && len(seenTenants) > 1 {
			return fmt.Errorf("kernel[%d].tenants cannot mix \"*\" with explicit tenant ids", i)
		}
	}
	if len(c.PluginDirs) == 0 {
		c.PluginDirs = []string{paths.PluginsDir()}
	}
	for i := range c.PluginDirs {
		c.PluginDirs[i] = os.ExpandEnv(strings.TrimSpace(c.PluginDirs[i]))
		if c.PluginDirs[i] == "" {
			return fmt.Errorf("plugin_dirs[%d] is empty", i)
		}
	}
	seenTenantCatalogs := map[string]struct{}{}
	for i := range c.Tenants {
		tenant := &c.Tenants[i]
		tenant.ID = strings.TrimSpace(tenant.ID)
		if tenant.ID == "" {
			return fmt.Errorf("tenant[%d].id is required", i)
		}
		if _, ok := seenTenantCatalogs[tenant.ID]; ok {
			return fmt.Errorf("duplicate tenant catalog id %q", tenant.ID)
		}
		seenTenantCatalogs[tenant.ID] = struct{}{}
		for j := range tenant.PluginDirs {
			tenant.PluginDirs[j] = os.ExpandEnv(strings.TrimSpace(tenant.PluginDirs[j]))
			if tenant.PluginDirs[j] == "" {
				return fmt.Errorf("tenant[%d].plugin_dirs[%d] is empty", i, j)
			}
		}
		for j := range tenant.SkillDirs {
			tenant.SkillDirs[j] = os.ExpandEnv(strings.TrimSpace(tenant.SkillDirs[j]))
			if tenant.SkillDirs[j] == "" {
				return fmt.Errorf("tenant[%d].skill_dirs[%d] is empty", i, j)
			}
		}
	}
	for i := range c.SkillDirs {
		c.SkillDirs[i] = os.ExpandEnv(strings.TrimSpace(c.SkillDirs[i]))
		if c.SkillDirs[i] == "" {
			return fmt.Errorf("skill_dirs[%d] is empty", i)
		}
	}
	if c.Pool.ColdWorkersPerTenantMax == 0 {
		c.Pool.ColdWorkersPerTenantMax = 16
	}
	if c.Pool.ColdWorkersPerTenantMax < 1 {
		return fmt.Errorf("pool.cold_workers_per_tenant_max must be >= 1")
	}
	for tenantID, limits := range c.Pool.Tenants {
		if strings.TrimSpace(tenantID) == "" {
			return fmt.Errorf("pool.tenants contains an empty tenant id")
		}
		if limits.ColdWorkersMax < 0 {
			return fmt.Errorf("pool.tenants.%s.cold_workers_max must be >= 0", tenantID)
		}
	}
	c.Distro.Active = strings.TrimSpace(c.Distro.Active)
	c.Distro.Dir = os.ExpandEnv(strings.TrimSpace(c.Distro.Dir))
	if (c.Distro.Active == "") != (c.Distro.Dir == "") {
		return fmt.Errorf("distro.active and distro.dir must be set together")
	}
	return nil
}

// SingleKernel returns the sole kernel entry supported by M2-02.
func (c Config) SingleKernel() (Kernel, error) {
	if len(c.Kernels) != 1 {
		return Kernel{}, fmt.Errorf("M2-02 runtime supports exactly one [[kernel]] entry, got %d", len(c.Kernels))
	}
	return c.Kernels[0], nil
}

// Package config loads tabula-runtime daemon configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the runtime-side configuration file. The list-of-tables shape is
// intentional: M2-02 only starts one kernel connection, but later N:M work must
// not change the schema.
type Config struct {
	Kernels    []Kernel `toml:"kernel"`
	PluginDirs []string `toml:"plugin_dirs"`
}

// Kernel describes one kernel endpoint that the runtime daemon dials.
type Kernel struct {
	ID        string `toml:"id"`
	URL       string `toml:"url"`
	TokenFile string `toml:"token_file"`
}

// DefaultPath returns $TABULA_HOME/config/runtime.toml. The caller is expected
// to use this only when no explicit --config path was provided.
func DefaultPath() (string, error) {
	home := strings.TrimSpace(os.Getenv("TABULA_HOME"))
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		home = filepath.Join(userHome, ".tabula")
	}
	return filepath.Join(home, "config", "runtime.toml"), nil
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
		if k.ID == "" {
			return fmt.Errorf("kernel[%d].id is required", i)
		}
		if k.URL == "" {
			return fmt.Errorf("kernel[%d].url is required", i)
		}
		if k.TokenFile == "" {
			return fmt.Errorf("kernel[%d].token_file is required", i)
		}
	}
	if len(c.PluginDirs) == 0 {
		home, err := tabulaHome()
		if err != nil {
			return err
		}
		c.PluginDirs = []string{filepath.Join(home, "plugins")}
	}
	for i := range c.PluginDirs {
		c.PluginDirs[i] = os.ExpandEnv(strings.TrimSpace(c.PluginDirs[i]))
		if c.PluginDirs[i] == "" {
			return fmt.Errorf("plugin_dirs[%d] is empty", i)
		}
	}
	return nil
}

func tabulaHome() (string, error) {
	home := strings.TrimSpace(os.Getenv("TABULA_HOME"))
	if home != "" {
		return home, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(userHome, ".tabula"), nil
}

// SingleKernel returns the sole kernel entry supported by M2-02.
func (c Config) SingleKernel() (Kernel, error) {
	if len(c.Kernels) != 1 {
		return Kernel{}, fmt.Errorf("M2-02 runtime supports exactly one [[kernel]] entry, got %d", len(c.Kernels))
	}
	return c.Kernels[0], nil
}

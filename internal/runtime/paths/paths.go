// Package paths centralises TABULA_HOME conventions for the Go side of the
// Tabula kernel and tooling.
//
// All accessors return paths that are “expanduser“-equivalent and made
// absolute, but symlinks are intentionally not resolved. This mirrors the
// Python helper in tabula_plugin_sdk.paths and avoids macOS surprises where
// /var/folders/... would otherwise canonicalise to /private/var/folders/...
//
// None of the accessors create directories. Call EnsureRuntimeDirs (or
// os.MkdirAll on a specific subdirectory) when callers need the directory to
// exist.
//
// TABULA_HOME is read from the environment on every call. Tests can pin a
// value without touching the environment via SetForTests; pass an empty
// string to clear the override.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const defaultHome = "~/.tabula"

var (
	overrideMu sync.RWMutex
	override   string
)

// expand returns “in“ with a leading “~“ expanded and the result made
// absolute. Symlinks are intentionally not resolved.
func expand(in string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		in = defaultHome
	}
	if strings.HasPrefix(in, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			if in == "~" {
				in = home
			} else if strings.HasPrefix(in, "~/") {
				in = filepath.Join(home, in[2:])
			}
		}
	}
	if !filepath.IsAbs(in) {
		if abs, err := filepath.Abs(in); err == nil {
			in = abs
		}
	}
	return filepath.Clean(in)
}

// Home returns the TABULA_HOME root.
//
// Honours $TABULA_HOME if set and non-empty, otherwise falls back to
// ~/.tabula. Read from the environment on every call.
func Home() string {
	overrideMu.RLock()
	o := override
	overrideMu.RUnlock()
	if o != "" {
		return o
	}
	return expand(os.Getenv("TABULA_HOME"))
}

// SetForTests overrides Home() until cleared with an empty string.
//
// Callers should defer SetForTests("") to restore env-driven behaviour.
func SetForTests(path string) {
	overrideMu.Lock()
	defer overrideMu.Unlock()
	if path == "" {
		override = ""
		return
	}
	override = expand(path)
}

// --- top-level subdirectories ---------------------------------------------

func ConfigDir() string  { return filepath.Join(Home(), "config") }
func StateDir() string   { return filepath.Join(Home(), "state") }
func DataDir() string    { return filepath.Join(Home(), "data") }
func CacheDir() string   { return filepath.Join(Home(), "cache") }
func RunDir() string     { return filepath.Join(Home(), "run") }
func LogsDir() string    { return filepath.Join(Home(), "logs") }
func PluginsDir() string { return filepath.Join(Home(), "plugins") }
func SkillsDir() string  { return filepath.Join(Home(), "skills") }
func TenantsDir() string { return filepath.Join(Home(), "tenants") }

// --- well-known files -----------------------------------------------------

// SecretsPath returns $TABULA_HOME/secrets.json.
func SecretsPath() string { return filepath.Join(Home(), "secrets.json") }

// GlobalConfigFile returns $TABULA_HOME/config/global.toml.
func GlobalConfigFile() string { return filepath.Join(ConfigDir(), "global.toml") }

// RuntimeConfigFile returns $TABULA_HOME/config/runtime.toml.
func RuntimeConfigFile() string { return filepath.Join(ConfigDir(), "runtime.toml") }

// ReloadTouchFile returns $TABULA_HOME/run/reload.touch, the trigger the
// installer touches after switching the active generation.
func ReloadTouchFile() string { return filepath.Join(RunDir(), "reload.touch") }

// --- tenant resolution ----------------------------------------------------

// TenantRoot returns the explicit $TABULA_TENANT_DIR if set, otherwise an
// empty string.
func TenantRoot() string {
	raw := strings.TrimSpace(os.Getenv("TABULA_TENANT_DIR"))
	if raw == "" {
		return ""
	}
	return expand(raw)
}

// TenantDir returns the tenant directory.
//
// If $TABULA_TENANT_DIR is set, it wins. Otherwise, when id is non-empty,
// the result is TenantsDir()/id. Returns an error if neither input is
// available.
func TenantDir(id string) (string, error) {
	if explicit := TenantRoot(); explicit != "" {
		return explicit, nil
	}
	id = strings.TrimSpace(id)
	if id != "" {
		return filepath.Join(TenantsDir(), id), nil
	}
	return "", fmt.Errorf("paths: TenantDir requires TABULA_TENANT_DIR or an explicit id")
}

// --- runtime helpers ------------------------------------------------------

// EnsureRuntimeDirs creates the standard top-level subdirectories under
// TABULA_HOME. Safe to call repeatedly.
func EnsureRuntimeDirs() error {
	for _, d := range []string{
		ConfigDir(), StateDir(), DataDir(), CacheDir(),
		RunDir(), LogsDir(), PluginsDir(), SkillsDir(), TenantsDir(),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("paths: mkdir %s: %w", d, err)
		}
	}
	return nil
}

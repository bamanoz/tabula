// Package layout centralizes TABULA_HOME filesystem conventions for Go code.
//
// Path expansion is expanduser-equivalent and absolute, but symlinks are not
// resolved. This mirrors the Python helper in tabula_plugin_sdk.paths and avoids
// macOS surprises where /var/folders/... canonicalizes to /private/var/folders/.
//
// Accessors do not create directories.
package layout

import (
	"os"
	"path/filepath"
	"strings"
)

const defaultHome = "~/.tabula"

func Home() string {
	return Expand(os.Getenv("TABULA_HOME"))
}

func Expand(in string) string {
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

func ConfigDir(home string) string  { return filepath.Join(Expand(home), "config") }
func RunDir(home string) string     { return filepath.Join(Expand(home), "run") }
func PluginsDir(home string) string { return filepath.Join(Expand(home), "plugins") }

func RuntimeConfigFile(home string) string {
	return filepath.Join(ConfigDir(home), "runtime.toml")
}

func RuntimeInstanceFile(home string) string {
	return filepath.Join(RunDir(home), "runtime-instance.json")
}

func ReloadTouchFile(home string) string {
	return filepath.Join(RunDir(home), "reload.touch")
}

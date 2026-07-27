package tabula

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPluginLoader_FailsFastWhenRuntimeConfigMissing pins down the contract
// from issue 004: kernel must not silently fall back to a default plugin
// directory when the installer has not yet written runtime.toml. The
// operator gets an explicit message pointing at `tabula-install`.
func TestPluginLoader_FailsFastWhenRuntimeConfigMissing(t *testing.T) {
	tabulaHome := t.TempDir()
	err := ensureRuntimeConfigExists(tabulaHome)
	if err == nil {
		t.Fatal("expected error when runtime.toml is absent")
	}
	msg := err.Error()
	if !strings.Contains(msg, "runtime.toml") {
		t.Fatalf("error must mention runtime.toml: %v", err)
	}
	if !strings.Contains(msg, "tabula-install") {
		t.Fatalf("error must mention tabula-install: %v", err)
	}
}

// TestPluginLoader_AcceptsEmptyPluginDirs verifies the second acceptance
// criterion: an installed runtime.toml with an empty plugin_dirs array is
// valid input (kernel loads zero plugins, no fallback to $TABULA_HOME/plugins).
func TestPluginLoader_AcceptsEmptyPluginDirs(t *testing.T) {
	tabulaHome := t.TempDir()
	configPath := filepath.Join(tabulaHome, "config", "runtime.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("plugin_dirs = []\nskill_dirs = []\n"), 0o644); err != nil {
		t.Fatalf("write runtime.toml: %v", err)
	}
	if err := ensureRuntimeConfigExists(tabulaHome); err != nil {
		t.Fatalf("ensureRuntimeConfigExists: %v", err)
	}
}

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRuntimeConfigUsesTokenFileAndExpandsEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TABULA_HOME", dir)
	path := filepath.Join(dir, "runtime.toml")
	writeFile(t, path, `[[kernel]]
id = "local"
url = "unix://${TABULA_HOME}/run/runtime.sock"
token_file = "${TABULA_HOME}/run/runtime-token"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	k, err := cfg.SingleKernel()
	if err != nil {
		t.Fatalf("SingleKernel: %v", err)
	}
	if k.URL != "unix://"+filepath.Join(dir, "run", "runtime.sock") {
		t.Fatalf("expanded url = %q", k.URL)
	}
	if k.TokenFile != filepath.Join(dir, "run", "runtime-token") {
		t.Fatalf("expanded token_file = %q", k.TokenFile)
	}
	if len(cfg.PluginDirs) != 1 || cfg.PluginDirs[0] != filepath.Join(dir, "plugins") {
		t.Fatalf("default plugin dirs = %#v", cfg.PluginDirs)
	}
}

func TestLoadRuntimeConfigExpandsPluginDirs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TABULA_HOME", dir)
	path := filepath.Join(dir, "runtime.toml")
	writeFile(t, path, `plugin_dirs = ["${TABULA_HOME}/plugins", "${TABULA_HOME}/extra-plugins"]

[[kernel]]
id = "local"
url = "unix:///tmp/runtime.sock"
token_file = "${TABULA_HOME}/run/runtime-token"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.PluginDirs) != 2 || cfg.PluginDirs[1] != filepath.Join(dir, "extra-plugins") {
		t.Fatalf("plugin dirs = %#v", cfg.PluginDirs)
	}
}

func TestLoadRuntimeConfigRejectsStaleTokenAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.toml")
	writeFile(t, path, `[[kernel]]
id = "local"
url = "unix:///tmp/runtime.sock"
token = "/tmp/runtime-token"
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported field") || !strings.Contains(err.Error(), "kernel.token") {
		t.Fatalf("expected unsupported token field error, got %v", err)
	}
}

func TestLoadRuntimeConfigRejectsPluralKernelAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.toml")
	writeFile(t, path, `[[kernels]]
id = "local"
url = "unix:///tmp/runtime.sock"
token_file = "/tmp/runtime-token"
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported field") || !strings.Contains(err.Error(), "kernels") {
		t.Fatalf("expected unsupported plural kernels field error, got %v", err)
	}
}

func TestLoadRuntimeConfigRejectsKernelSideRuntimeRegistryShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.toml")
	writeFile(t, path, `[[runtime]]
id = "local"
backend = "local"
token_file = "/tmp/runtime-token"
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported field") || !strings.Contains(err.Error(), "runtime") {
		t.Fatalf("expected unsupported kernel-side runtime registry field error, got %v", err)
	}
}

func TestLoadRuntimeConfigRejectsUnknownTopLevelFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.toml")
	writeFile(t, path, `runtime_id = "local"

[[kernel]]
id = "main"
url = "unix:///tmp/runtime.sock"
token_file = "/tmp/runtime-token"
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported field") || !strings.Contains(err.Error(), "runtime_id") {
		t.Fatalf("expected unsupported runtime_id field error, got %v", err)
	}
}

func TestSingleKernelRejectsMultiKernelForM2(t *testing.T) {
	cfg := Config{Kernels: []Kernel{{ID: "a", URL: "unix:///a", TokenFile: "/a"}, {ID: "b", URL: "unix:///b", TokenFile: "/b"}}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	_, err := cfg.SingleKernel()
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected single-kernel error, got %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

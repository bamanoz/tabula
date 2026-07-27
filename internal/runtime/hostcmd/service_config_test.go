package hostcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadRuntimeServiceConfigAssemblesRuntimeConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	t.Setenv("TABULA_URL", "ws://127.0.0.1:9999/ws")
	t.Setenv("TABULA_RUNTIME_LOG_LEVEL", "debug")
	pluginDir := filepath.Join(home, "plugins")
	skillDir := filepath.Join(home, "skills")
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	configPath := filepath.Join(home, "config", "runtime.toml")
	content := "" +
		"plugin_dirs = [\"" + pluginDir + "\"]\n" +
		"skill_dirs = [\"" + skillDir + "\"]\n" +
		"[[kernel]]\n" +
		"id = \"kernel-local\"\n" +
		"url = \"ws://127.0.0.1:8089/ws\"\n" +
		"token_file = \"" + filepath.Join(home, "run", "runtime-token") + "\"\n" +
		"tenants = [\"default\"]\n" +
		"[runtimes.python]\n" +
		"command = [\"python3\"]\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	cfg, err := loadRuntimeServiceConfig(runtimeServiceConfigOptions{
		ConfigPath: configPath,
		RuntimeID:  "runtime-test",
		TokenFile:  filepath.Join(home, "override-token"),
		Stderr:     &bytes.Buffer{},
		Now:        time.Unix(123, 0),
	})
	if err != nil {
		t.Fatalf("load runtime service config: %v", err)
	}

	if cfg.Home != home {
		t.Fatalf("Home = %q, want %q", cfg.Home, home)
	}
	if cfg.RuntimeID != "runtime-test" {
		t.Fatalf("RuntimeID = %q, want runtime-test", cfg.RuntimeID)
	}
	if cfg.Kernel.TokenFile != filepath.Join(home, "override-token") {
		t.Fatalf("TokenFile = %q", cfg.Kernel.TokenFile)
	}
	if cfg.PoolOptions.TabulaHome != home {
		t.Fatalf("PoolOptions.TabulaHome = %q", cfg.PoolOptions.TabulaHome)
	}
	if cfg.PoolOptions.KernelURL != "ws://127.0.0.1:9999/ws" {
		t.Fatalf("PoolOptions.KernelURL = %q", cfg.PoolOptions.KernelURL)
	}
	if len(cfg.PoolOptions.PythonPath) != 1 {
		t.Fatalf("PythonPath len = %d, want 1", len(cfg.PoolOptions.PythonPath))
	}
	if cfg.Logging.ConsoleLevel != "debug" {
		t.Fatalf("Logging.ConsoleLevel = %q, want debug", cfg.Logging.ConsoleLevel)
	}
	if cfg.ManifestStore == nil {
		t.Fatal("ManifestStore is nil")
	}
	if cfg.Policy == nil {
		t.Fatal("Policy is nil")
	}
}

func TestResolveRuntimeConfigPathUsesExplicitPath(t *testing.T) {
	got, err := resolveRuntimeConfigPath(" /tmp/runtime.toml ")
	if err != nil {
		t.Fatalf("resolve explicit path: %v", err)
	}
	if got != "/tmp/runtime.toml" {
		t.Fatalf("path = %q, want /tmp/runtime.toml", got)
	}
}

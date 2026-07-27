package tabula

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKernelServiceConfigAppliesEnvAndDotenv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	t.Setenv("TABULA_URL", "")
	t.Setenv("TABULA_PATH", "/snapshot/bin:/usr/bin")
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "kernel.toml"), []byte("[kernel]\nurl = \"ws://127.0.0.1/ws\"\n"), 0o644); err != nil {
		t.Fatalf("write kernel config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("TABULA_LOG_LEVEL=debug\nTABULA_LOG_TYPE=text\n"), 0o644); err != nil {
		t.Fatalf("write dotenv: %v", err)
	}

	cfg, err := loadKernelServiceConfig()
	if err != nil {
		t.Fatalf("load service config: %v", err)
	}

	if cfg.Home != home {
		t.Fatalf("Home = %q, want %q", cfg.Home, home)
	}
	if cfg.Kernel.URL != "ws://127.0.0.1/ws" {
		t.Fatalf("Kernel.URL = %q", cfg.Kernel.URL)
	}
	if cfg.ListenAddr != "127.0.0.1:8089" {
		t.Fatalf("ListenAddr = %q, want default port", cfg.ListenAddr)
	}
	if cfg.SavedPath != "/snapshot/bin:/usr/bin" {
		t.Fatalf("SavedPath = %q", cfg.SavedPath)
	}
	if cfg.Logging.ConsoleLevel != "debug" {
		t.Fatalf("Logging.ConsoleLevel = %q, want debug", cfg.Logging.ConsoleLevel)
	}
	if cfg.Logging.ConsoleFormat != "text" {
		t.Fatalf("Logging.ConsoleFormat = %q, want text", cfg.Logging.ConsoleFormat)
	}
}

func TestListenAddrFromKernelURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "host with port", raw: "ws://127.0.0.1:9090/ws", want: "127.0.0.1:9090"},
		{name: "host without port", raw: "ws://localhost/ws", want: "localhost:8089"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := listenAddrFromKernelURL(tt.raw)
			if err != nil {
				t.Fatalf("listen addr: %v", err)
			}
			if got != tt.want {
				t.Fatalf("listen addr = %q, want %q", got, tt.want)
			}
		})
	}
}

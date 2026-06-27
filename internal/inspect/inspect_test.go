package inspect

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
)

func TestBuildReadsRuntimeConfigAndRedactsPluginConfig(t *testing.T) {
	home := t.TempDir()
	plugins := filepath.Join(home, "plugins")
	writeInspectFile(t, filepath.Join(plugins, "fs", "plugin.toml"), inspectPluginManifest("fs", "fs_read"))
	writeRuntimeConfig(t, home, plugins)
	writeInspectFile(t, filepath.Join(home, "config", "global.toml"), `[plugins.fs]
api_key = "global-secret"
[plugins.fs.limits]
count = 1
`)
	writeInspectFile(t, filepath.Join(home, "config", "plugins", "fs", "config.toml"), `enabled = false
[limits]
count = 2
token = "plugin-secret"
`)
	writeInspectFile(t, filepath.Join(home, "tenants", "alpha", "config", "plugins", "fs", "defaults.toml"), `[limits]
grace = 9
`)
	writeInspectFile(t, filepath.Join(home, "tenants", "alpha", "config", "plugins", "fs", "config.toml"), `[limits]
count = 3
password = "tenant-secret"
`)

	report, err := Build(Options{Home: home, TenantID: "alpha", PluginID: "fs"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if report.Paths.Home != home || report.Runtime.Source != filepath.Join(home, "config", "runtime.toml") {
		t.Fatalf("unexpected paths: %+v runtime=%+v", report.Paths, report.Runtime)
	}
	if len(report.Plugins) != 1 || report.Plugins[0].ID != "fs" || report.Plugins[0].Enabled {
		t.Fatalf("plugins = %+v", report.Plugins)
	}
	if report.Plugin == nil {
		t.Fatal("expected plugin config")
	}
	data, err := json.Marshal(report.Plugin.Config)
	if err != nil {
		t.Fatalf("marshal plugin config: %v", err)
	}
	text := string(data)
	for _, secret := range []string{"global-secret", "plugin-secret", "tenant-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("secret %q leaked in %s", secret, text)
		}
	}
	if strings.Count(text, "redacted") != 3 {
		t.Fatalf("expected redactions in %s", text)
	}
}

func TestBuildFailsWhenRuntimeConfigMissing(t *testing.T) {
	_, err := Build(Options{Home: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "runtime.toml not found") {
		t.Fatalf("expected missing runtime.toml error, got %v", err)
	}
}

func TestBuildUsesRuntimeConfigOnly(t *testing.T) {
	home := t.TempDir()
	plugins := filepath.Join(home, "plugins")
	writeInspectFile(t, filepath.Join(plugins, "fs", "plugin.toml"), inspectPluginManifest("fs", "fs_read"))
	writeRuntimeConfig(t, home, plugins)

	if _, err := Build(Options{Home: home}); err != nil {
		t.Fatalf("Build: %v", err)
	}
}

func TestBuildHealthInvokesHealthToolAndSkipsOthers(t *testing.T) {
	home := t.TempDir()
	plugins := filepath.Join(home, "plugins")
	writeInspectFile(t, filepath.Join(plugins, "fs", "plugin.toml"), inspectPluginManifest("fs", "health"))
	writeInspectFile(t, filepath.Join(plugins, "exec", "plugin.toml"), inspectPluginManifest("exec", "exec_run"))
	writeInspectFile(t, filepath.Join(plugins, "fs", "run.py"), inspectHealthWorker())
	writeInspectFile(t, filepath.Join(plugins, "exec", "run.py"), inspectHealthWorker())
	writeRuntimeConfig(t, home, plugins)

	report, err := BuildHealth(context.Background(), Options{Home: home, TenantID: "alpha"})
	if err != nil {
		t.Fatalf("BuildHealth: %v", err)
	}
	if len(report.Plugins) != 2 {
		t.Fatalf("plugins = %+v", report.Plugins)
	}
	byID := map[string]PluginHealth{}
	for _, plugin := range report.Plugins {
		byID[plugin.PluginID] = plugin
	}
	if byID["fs"].Status != "ok" {
		t.Fatalf("fs health = %+v", byID["fs"])
	}
	if byID["exec"].Status != "skipped" {
		t.Fatalf("exec health = %+v", byID["exec"])
	}
}

func writeRuntimeConfig(t *testing.T, home, plugins string) {
	t.Helper()
	if err := runtimeconfig.Save(filepath.Join(home, "config", "runtime.toml"), runtimeconfig.Config{
		Kernels:    []runtimeconfig.Kernel{{ID: "local", URL: "unix:///tmp/runtime.sock", TokenFile: "/tmp/token", Tenants: []string{"alpha"}}},
		PluginDirs: []string{plugins},
		Tenants:    []runtimeconfig.Tenant{{ID: "alpha", PluginDirs: []string{filepath.Join(home, "tenants", "alpha", "plugins")}}},
		Distro:     runtimeconfig.Distro{Active: "code-immune", Dir: filepath.Join(home, "distrib", "code-immune")},
	}); err != nil {
		t.Fatalf("save runtime config: %v", err)
	}
}

func inspectPluginManifest(id, tool string) string {
	return `id = "` + id + `"
name = "` + id + `"
version = "0.1.0"
[worker]
command = ["python3", "run.py"]
mode = "warm"

[[tools]]
name = "` + tool + `"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`
}

func inspectHealthWorker() string {
	return `#!/usr/bin/env python3
import json
import sys

line = sys.stdin.readline()
if not line:
    sys.exit(2)
json.loads(line)
sys.stdout.write(json.dumps({"op": "init_ack", "ready": True, "tools": [{"name": "health"}], "subscriptions": []}) + "\n")
sys.stdout.flush()

for line in sys.stdin:
    frame = json.loads(line)
    if frame.get("op") == "shutdown":
        sys.exit(0)
    sys.stdout.write(json.dumps({"op": "result", "call_id": frame.get("call_id", ""), "ok": True, "data": {"status": "ok", "messages": [{"level": "info", "text": "ready"}]}}) + "\n")
    sys.stdout.flush()
`
}

func writeInspectFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

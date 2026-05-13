package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bamanoz/tabula/internal/tenant"
)

func TestLoaderMergesGlobalAndTenantConfig(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "config", "global.toml"), "[kernel]\nlog_level = \"info\"\n[kernel.flags]\nalpha = true\n")
	writeFile(t, filepath.Join(home, "tenants", "alpha", "config", "tenant.toml"), "[kernel]\nlog_level = \"debug\"\n[kernel.flags]\nbeta = true\n")
	loader := NewLoader(home)
	doc, err := loader.LoadTenant("alpha")
	if err != nil {
		t.Fatalf("LoadTenant: %v", err)
	}
	kernel := doc["kernel"].(Document)
	if kernel["log_level"] != "debug" {
		t.Fatalf("log_level = %#v", kernel["log_level"])
	}
	flags := kernel["flags"].(Document)
	if flags["alpha"] != true || flags["beta"] != true {
		t.Fatalf("flags = %#v", flags)
	}
}

func TestLoaderPrefersTenantEffectivePluginConfig(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "config", "plugins", "demo", "config.toml"), "enabled = true\n[limits]\ncount = 1\ngrace = 9\n")
	writeFile(t, filepath.Join(home, "tenants", "alpha", "config", "plugins", "demo", "config.toml"), "[limits]\ncount = 2\n")
	loader := NewLoader(home)
	doc, err := loader.LoadPlugin("alpha", "demo")
	if err != nil {
		t.Fatalf("LoadPlugin: %v", err)
	}
	if _, ok := doc["enabled"]; ok {
		t.Fatalf("enabled unexpectedly present: %#v", doc["enabled"])
	}
	limits := doc["limits"].(Document)
	if limits["count"] != int64(2) {
		t.Fatalf("limits = %#v", limits)
	}
	if _, ok := limits["grace"]; ok {
		t.Fatalf("limits unexpectedly merged global value: %#v", limits)
	}
}

func TestLoaderRejectsInvalidTenantID(t *testing.T) {
	_, err := NewLoader(t.TempDir()).LoadTenant("Bad_ID")
	if err == nil {
		t.Fatal("expected invalid tenant id error")
	}
	if err := tenant.ValidateID("alpha"); err != nil {
		t.Fatalf("sanity validate alpha: %v", err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

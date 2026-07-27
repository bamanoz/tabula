package tabula

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bamanoz/tabula/internal/kernel"
)

func TestLoadTenantInitMetaReadsPromptBuilderAndWorkspace(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "tenants", "claw-tabula", "config", "tenant.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir tenant config: %v", err)
	}
	if err := os.WriteFile(path, []byte(`[agent]
prompt_builder = "claw_prompt.builder"

[workspace]
project_root = "/repo"
`), 0o644); err != nil {
		t.Fatalf("write tenant config: %v", err)
	}

	raw, err := loadTenantInitMeta(home, "claw-tabula")
	if err != nil {
		t.Fatalf("loadTenantInitMeta: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	if meta["prompt_builder"] != "claw_prompt.builder" {
		t.Fatalf("unexpected prompt_builder: %#v", meta["prompt_builder"])
	}
	workspace, ok := meta["workspace"].(map[string]any)
	if !ok || workspace["path"] != "/repo" {
		t.Fatalf("unexpected workspace meta: %#v", meta["workspace"])
	}
}

func TestConfigureTenantInitMetaLoadsNewTenantWorkspace(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "tenants", "mj", "config", "tenant.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir tenant config: %v", err)
	}
	if err := os.WriteFile(path, []byte(`[agent]
prompt_builder = "code_immune_prompt.builder"

[workspace]
project_root = "/Users/mak/src/mj"
`), 0o644); err != nil {
		t.Fatalf("write tenant config: %v", err)
	}

	hub := kernel.NewHub(json.RawMessage(`[]`), nil)
	hub.SetInitMeta(json.RawMessage(`{"workspace":{"path":"/private/tmp/tabula-browser-project"}}`))
	if err := configureTenantInitMeta(home, hub); err != nil {
		t.Fatalf("configureTenantInitMeta: %v", err)
	}

	got := hub.InitMetaForTenant("mj")
	var meta map[string]any
	if err := json.Unmarshal(got, &meta); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	workspace := meta["workspace"].(map[string]any)
	if workspace["path"] != "/Users/mak/src/mj" {
		t.Fatalf("tenant workspace did not replace stale global workspace: %#v", workspace)
	}
}

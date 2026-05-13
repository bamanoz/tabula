package tabula

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTenantCreateListShowDeleteCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	if code := tenantCreateCmd([]string{"alpha", "--display-name", "Alpha Project"}); code != 0 {
		t.Fatalf("tenant create exit = %d", code)
	}
	if code := tenantCreateCmd([]string{"alpha"}); code == 0 {
		t.Fatal("duplicate tenant create should fail")
	}
	if code := tenantCreateCmd([]string{"alpha", "--exists-ok"}); code != 0 {
		t.Fatalf("tenant create --exists-ok exit = %d", code)
	}
	var out bytes.Buffer
	if code := tenantListCmd([]string{"--json"}, &out); code != 0 {
		t.Fatalf("tenant list exit = %d", code)
	}
	var list []tenantCLIRecord
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &list); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(list) != 1 || list[0].ID != "alpha" || list[0].DisplayName != "Alpha Project" {
		t.Fatalf("list = %#v", list)
	}
	out.Reset()
	if code := tenantShowCmd([]string{"alpha", "--json"}, &out); code != 0 {
		t.Fatalf("tenant show exit = %d", code)
	}
	if !strings.Contains(out.String(), `"id":"alpha"`) || !strings.Contains(out.String(), `"active_session_count":0`) {
		t.Fatalf("unexpected show json: %s", out.String())
	}
	if code := tenantDeleteCmd([]string{"alpha"}); code != 0 {
		t.Fatalf("tenant delete exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(home, "tenants", "alpha")); !os.IsNotExist(err) {
		t.Fatalf("tenant dir still exists or unexpected err: %v", err)
	}
}

func TestTenantDeleteDefaultRequiresForce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	if code := tenantCreateCmd([]string{"default"}); code != 0 {
		t.Fatalf("create default exit = %d", code)
	}
	if code := tenantDeleteCmd([]string{"default"}); code == 0 {
		t.Fatal("delete default without force should fail")
	}
	if code := tenantDeleteCmd([]string{"default", "--force"}); code != 0 {
		t.Fatalf("delete default --force exit = %d", code)
	}
}

func TestTenantCreateDefaultSeedsWorkspaceRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	if code := tenantCreateCmd([]string{"default"}); code != 0 {
		t.Fatalf("create default exit = %d", code)
	}
	data, err := os.ReadFile(filepath.Join(home, "tenants", "default", "config", "tenant.toml"))
	if err != nil {
		t.Fatalf("read default tenant config: %v", err)
	}
	if got := string(data); !strings.Contains(got, "[workspace]") || !strings.Contains(got, `project_root = "${TABULA_HOME}"`) {
		t.Fatalf("default tenant config missing workspace root: %s", got)
	}
}

func TestTenantCreateRejectsInvalidID(t *testing.T) {
	t.Setenv("TABULA_HOME", t.TempDir())
	if code := tenantCreateCmd([]string{"Bad_ID"}); code == 0 {
		t.Fatal("invalid tenant id should fail")
	}
}

func TestTenantCreateFansOutActiveRuntimeSurface(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	for _, rel := range []string{
		"skills/test-skill/SKILL.md",
		"plugins/test-plugin/plugin.toml",
		"clients/test-client/client.toml",
		"templates/SYSTEM.md",
		"_lib/python/src/pkg/__init__.py",
	} {
		path := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if code := tenantCreateCmd([]string{"alpha"}); code != 0 {
		t.Fatalf("tenant create exit = %d", code)
	}
	for _, rel := range []string{
		"skills/test-skill/SKILL.md",
		"plugins/test-plugin/plugin.toml",
		"clients/test-client/client.toml",
		"templates/SYSTEM.md",
		"_lib",
	} {
		path := filepath.Join(home, "tenants", "alpha", rel)
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("tenant runtime surface missing %s: %v", rel, err)
		}
	}
}

func TestTenantCreateSeedsConfigTemplatesWithoutOverwrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	for rel, body := range map[string]string{
		"config/global.toml.example":                            "[kernel]\nmode = \"local\"\n",
		"config/plugins/demo/config.toml.example":               "enabled = true\n",
		"tenants/alpha/config/global.toml.example":              "keep-me\n",
		"tenants/alpha/config/plugins/demo/config.toml.example": "keep-plugin\n",
	} {
		path := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if code := tenantCreateCmd([]string{"alpha", "--exists-ok"}); code != 0 {
		t.Fatalf("tenant create exit = %d", code)
	}
	checks := map[string]string{
		"tenants/alpha/config/global.toml.example":              "keep-me\n",
		"tenants/alpha/config/plugins/demo/config.toml.example": "keep-plugin\n",
	}
	for rel, want := range checks {
		data, err := os.ReadFile(filepath.Join(home, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if got := string(data); got != want {
			t.Fatalf("%s = %q, want %q", rel, got, want)
		}
	}
	if code := tenantCreateCmd([]string{"beta"}); code != 0 {
		t.Fatalf("tenant create beta exit = %d", code)
	}
	checks = map[string]string{
		"tenants/beta/config/global.toml.example":              "[kernel]\nmode = \"local\"\n",
		"tenants/beta/config/plugins/demo/config.toml.example": "enabled = true\n",
	}
	for rel, want := range checks {
		data, err := os.ReadFile(filepath.Join(home, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if got := string(data); got != want {
			t.Fatalf("%s = %q, want %q", rel, got, want)
		}
	}
}

func TestTenantSetWorkspaceRootWritesTenantConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	if code := tenantCreateCmd([]string{"alpha"}); code != 0 {
		t.Fatalf("tenant create exit = %d", code)
	}
	workspace := filepath.Join(home, "workspace")
	if code := tenantSetCmd([]string{"alpha", "--workspace-root", workspace}); code != 0 {
		t.Fatalf("tenant set exit = %d", code)
	}
	path := filepath.Join(home, "tenants", "alpha", "config", "tenant.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tenant config: %v", err)
	}
	if got := string(data); !strings.Contains(got, "[workspace]") || !strings.Contains(got, fmt.Sprintf("project_root = %q", workspace)) {
		t.Fatalf("tenant config missing workspace root: %s", got)
	}
	if code := tenantSetCmd([]string{"alpha", "--workspace-root", filepath.Join(home, "other")}); code != 0 {
		t.Fatalf("tenant set update exit = %d", code)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tenant config: %v", err)
	}
	if strings.Count(string(data), "project_root") != 1 {
		t.Fatalf("tenant config duplicated project_root: %s", string(data))
	}
}

func TestTenantSetWorkspaceRootRejectsUnknownTenant(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	if code := tenantSetCmd([]string{"missing", "--workspace-root", home}); code == 0 {
		t.Fatal("tenant set unexpectedly succeeded for missing tenant")
	}
}

package tenant

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateIDRejectsInvalidAndReservedNames(t *testing.T) {
	for _, id := range []string{"", "Default", "a_b", "-bad", "admin", "kernel", "system", "runtime"} {
		if err := ValidateID(id); err == nil {
			t.Fatalf("ValidateID(%q) succeeded", id)
		}
	}
	if err := ValidateID("my-project-1"); err != nil {
		t.Fatalf("ValidateID valid id: %v", err)
	}
}

func TestFSStoreCreateListGetDelete(t *testing.T) {
	store := NewFSStore(t.TempDir())
	store.now = func() time.Time { return time.Unix(123, 0).UTC() }
	if err := store.Create(Tenant{ID: "alpha", DisplayName: "Alpha"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Create(Tenant{ID: "alpha"}); !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
	tenant, ok, err := store.Get("alpha")
	if err != nil || !ok {
		t.Fatalf("Get: %#v, %v, %v", tenant, ok, err)
	}
	if tenant.DisplayName != "Alpha" || !tenant.CreatedAt.Equal(time.Unix(123, 0).UTC()) {
		t.Fatalf("tenant = %#v", tenant)
	}
	list, err := store.List()
	if err != nil || len(list) != 1 || list[0].ID != "alpha" {
		t.Fatalf("List = %#v, %v", list, err)
	}
	if mode := fileMode(t, filepath.Join(store.tenantDir("alpha"), "tenant.toml")); mode != 0o600 {
		t.Fatalf("tenant.toml mode = %#o", mode)
	}
	if mode := fileMode(t, store.tenantsDir()); mode != 0o700 {
		t.Fatalf("tenants dir mode = %#o", mode)
	}
	if err := store.Delete("alpha"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, err := store.Get("alpha"); err != nil || ok {
		t.Fatalf("Get deleted = ok %v err %v", ok, err)
	}
}

func TestPrepareBootLayoutCreatesDefaultTenant(t *testing.T) {
	home := t.TempDir()
	if err := PrepareBootLayout(home); err != nil {
		t.Fatalf("PrepareBootLayout: %v", err)
	}
	store := NewFSStore(home)
	if _, ok, err := store.Get(DefaultID); err != nil || !ok {
		t.Fatalf("default tenant missing: ok=%v err=%v", ok, err)
	}
	for _, rel := range []string{"config", "config/plugins", "state/plugins", "state/skills", "state/sessions", "cache", "logs", "skills", "plugins", "apps", "templates"} {
		if info, err := os.Stat(filepath.Join(home, "tenants", DefaultID, rel)); err != nil || !info.IsDir() {
			t.Fatalf("missing tenant dir %s: %v", rel, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(home, "tenants", DefaultID, "config", "tenant.toml"))
	if err != nil {
		t.Fatalf("read default tenant config: %v", err)
	}
	if got := string(data); !strings.Contains(got, "[workspace]") || !strings.Contains(got, `project_root = "${TABULA_HOME}"`) {
		t.Fatalf("default workspace root missing: %s", got)
	}
}

func TestPrepareBootLayoutPreservesExistingDefaultWorkspaceRoot(t *testing.T) {
	home := t.TempDir()
	store := NewFSStore(home)
	if err := store.Create(Tenant{ID: DefaultID, DisplayName: "Default"}); err != nil {
		t.Fatalf("create default: %v", err)
	}
	path := filepath.Join(home, "tenants", DefaultID, "config", "tenant.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir tenant config: %v", err)
	}
	workspaceRoot := filepath.Join(home, "workspace")
	if err := os.WriteFile(path, []byte("[workspace]\nproject_root = "+quoteTOML(workspaceRoot)+"\n"), 0o600); err != nil {
		t.Fatalf("write tenant: %v", err)
	}
	if err := PrepareBootLayout(home); err != nil {
		t.Fatalf("PrepareBootLayout: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tenant config: %v", err)
	}
	if got := string(data); strings.Count(got, "project_root") != 1 || !strings.Contains(got, "project_root = "+quoteTOML(workspaceRoot)) {
		t.Fatalf("existing workspace root not preserved: %s", got)
	}
}

func TestPrepareBootLayoutMarksLegacyFlatLayoutWithoutBreakingRuntimeSurface(t *testing.T) {
	home := t.TempDir()
	for _, rel := range []string{"skills/timer", "plugins/sessions", "apps/driver", "templates/default", "state/sessions"} {
		if err := os.MkdirAll(filepath.Join(home, rel), 0o755); err != nil {
			t.Fatalf("mkdir legacy %s: %v", rel, err)
		}
	}
	if err := PrepareBootLayout(home); err != nil {
		t.Fatalf("PrepareBootLayout: %v", err)
	}
	for _, rel := range []string{"skills/timer", "plugins/sessions", "apps/driver", "templates/default", "state/sessions"} {
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Fatalf("legacy runtime surface %s should remain until M4-04 fan-out: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "tenants", migrationMarker)); err != nil {
		t.Fatalf("migration marker missing: %v", err)
	}
	if err := PrepareBootLayout(home); err != nil {
		t.Fatalf("second PrepareBootLayout: %v", err)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

func quoteTOML(value string) string {
	return "\"" + strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), "\"", "\\\"") + "\""
}

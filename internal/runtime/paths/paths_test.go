package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeUsesEnv(t *testing.T) {
	SetForTests("")
	t.Cleanup(func() { SetForTests("") })
	dir := t.TempDir()
	t.Setenv("TABULA_HOME", dir)
	if got := Home(); got != filepath.Clean(dir) {
		t.Fatalf("Home() = %q, want %q", got, dir)
	}
}

func TestHomeFallsBackToDefault(t *testing.T) {
	SetForTests("")
	t.Cleanup(func() { SetForTests("") })
	t.Setenv("TABULA_HOME", "")
	user, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no user home dir")
	}
	want := filepath.Join(user, ".tabula")
	if got := Home(); got != want {
		t.Fatalf("Home() = %q, want %q", got, want)
	}
}

func TestHomeRereadsEnvEveryCall(t *testing.T) {
	SetForTests("")
	t.Cleanup(func() { SetForTests("") })
	first := t.TempDir()
	second := t.TempDir()
	t.Setenv("TABULA_HOME", first)
	if got := Home(); got != filepath.Clean(first) {
		t.Fatalf("first Home() = %q", got)
	}
	t.Setenv("TABULA_HOME", second)
	if got := Home(); got != filepath.Clean(second) {
		t.Fatalf("second Home() = %q", got)
	}
}

func TestHomeDoesNotResolveSymlinks(t *testing.T) {
	SetForTests("")
	t.Cleanup(func() { SetForTests("") })
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	t.Setenv("TABULA_HOME", link)
	got := Home()
	if got != filepath.Clean(link) {
		t.Fatalf("Home() = %q, want symlink path %q", got, link)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err == nil && got == resolved && resolved != filepath.Clean(link) {
		t.Fatalf("Home() should not canonicalise symlinks; got %q", got)
	}
}

func TestSetForTestsOverride(t *testing.T) {
	t.Cleanup(func() { SetForTests("") })
	dir := t.TempDir()
	SetForTests(dir)
	if got := Home(); got != filepath.Clean(dir) {
		t.Fatalf("Home() = %q, want %q", got, dir)
	}
	other := t.TempDir()
	t.Setenv("TABULA_HOME", other)
	if got := Home(); got != filepath.Clean(dir) {
		t.Fatalf("override should win, got %q", got)
	}
	SetForTests("")
	if got := Home(); got != filepath.Clean(other) {
		t.Fatalf("after clear Home() = %q, want %q", got, other)
	}
}

func TestSubdirAccessors(t *testing.T) {
	t.Cleanup(func() { SetForTests("") })
	dir := t.TempDir()
	SetForTests(dir)
	cases := map[string]string{
		ConfigDir():  filepath.Join(dir, "config"),
		StateDir():   filepath.Join(dir, "state"),
		DataDir():    filepath.Join(dir, "data"),
		CacheDir():   filepath.Join(dir, "cache"),
		RunDir():     filepath.Join(dir, "run"),
		LogsDir():    filepath.Join(dir, "logs"),
		PluginsDir(): filepath.Join(dir, "plugins"),
		SkillsDir():  filepath.Join(dir, "skills"),
		TenantsDir(): filepath.Join(dir, "tenants"),
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestAccessorsDoNotCreateDirs(t *testing.T) {
	t.Cleanup(func() { SetForTests("") })
	dir := t.TempDir()
	SetForTests(dir)
	_ = ConfigDir()
	_ = StateDir()
	_ = RunDir()
	_ = PluginsDir()
	for _, sub := range []string{"config", "state", "run", "plugins"} {
		if _, err := os.Stat(filepath.Join(dir, sub)); !os.IsNotExist(err) {
			t.Errorf("%s should not have been created (err=%v)", sub, err)
		}
	}
}

func TestWellKnownFiles(t *testing.T) {
	t.Cleanup(func() { SetForTests("") })
	dir := t.TempDir()
	SetForTests(dir)
	if got, want := SecretsPath(), filepath.Join(dir, "secrets.json"); got != want {
		t.Errorf("SecretsPath = %q, want %q", got, want)
	}
	if got, want := GlobalConfigFile(), filepath.Join(dir, "config", "global.toml"); got != want {
		t.Errorf("GlobalConfigFile = %q, want %q", got, want)
	}
	if got, want := RuntimeConfigFile(), filepath.Join(dir, "config", "runtime.toml"); got != want {
		t.Errorf("RuntimeConfigFile = %q, want %q", got, want)
	}
	if got, want := ReloadTouchFile(), filepath.Join(dir, "run", "reload.touch"); got != want {
		t.Errorf("ReloadTouchFile = %q, want %q", got, want)
	}
}

func TestTenantDirPrefersEnv(t *testing.T) {
	t.Cleanup(func() { SetForTests("") })
	SetForTests(t.TempDir())
	explicit := t.TempDir()
	t.Setenv("TABULA_TENANT_DIR", explicit)
	got, err := TenantDir("ignored")
	if err != nil {
		t.Fatalf("TenantDir: %v", err)
	}
	if got != filepath.Clean(explicit) {
		t.Fatalf("TenantDir = %q, want %q", got, explicit)
	}
}

func TestTenantDirUsesIDWhenEnvUnset(t *testing.T) {
	t.Cleanup(func() { SetForTests("") })
	dir := t.TempDir()
	SetForTests(dir)
	t.Setenv("TABULA_TENANT_DIR", "")
	got, err := TenantDir("alpha")
	if err != nil {
		t.Fatalf("TenantDir: %v", err)
	}
	want := filepath.Join(dir, "tenants", "alpha")
	if got != want {
		t.Fatalf("TenantDir = %q, want %q", got, want)
	}
}

func TestTenantDirWithoutInputsErrors(t *testing.T) {
	t.Cleanup(func() { SetForTests("") })
	SetForTests(t.TempDir())
	t.Setenv("TABULA_TENANT_DIR", "")
	if _, err := TenantDir(""); err == nil {
		t.Fatal("expected error from TenantDir with no inputs")
	}
}

func TestEnsureRuntimeDirsIdempotent(t *testing.T) {
	t.Cleanup(func() { SetForTests("") })
	dir := t.TempDir()
	SetForTests(dir)
	if err := EnsureRuntimeDirs(); err != nil {
		t.Fatalf("first EnsureRuntimeDirs: %v", err)
	}
	if err := EnsureRuntimeDirs(); err != nil {
		t.Fatalf("second EnsureRuntimeDirs: %v", err)
	}
	for _, sub := range []string{"config", "state", "data", "cache", "run", "logs", "plugins", "skills", "tenants"} {
		if _, err := os.Stat(filepath.Join(dir, sub)); err != nil {
			t.Errorf("%s missing: %v", sub, err)
		}
	}
}

func TestExpandHandlesTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no user home dir")
	}
	if got := expand("~/foo"); got != filepath.Join(home, "foo") {
		t.Errorf("expand(~/foo) = %q, want %q", got, filepath.Join(home, "foo"))
	}
	if got := expand("~"); got != filepath.Clean(home) {
		t.Errorf("expand(~) = %q, want %q", got, home)
	}
}

func TestExpandMakesRelativeAbsolute(t *testing.T) {
	got := expand("some/relative/path")
	if !filepath.IsAbs(got) {
		t.Fatalf("expand should produce absolute path, got %q", got)
	}
	if !strings.HasSuffix(got, filepath.Join("some", "relative", "path")) {
		t.Errorf("unexpected expand suffix: %q", got)
	}
}

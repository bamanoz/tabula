package layout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeUsesEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TABULA_HOME", dir)
	if got := Home(); got != filepath.Clean(dir) {
		t.Fatalf("Home() = %q, want %q", got, dir)
	}
}

func TestHomeFallsBackToDefault(t *testing.T) {
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
		t.Fatalf("Home() should not canonicalize symlinks; got %q", got)
	}
}

func TestAccessorsUseExplicitHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TABULA_HOME", t.TempDir())
	cases := map[string]string{
		ConfigDir(dir):           filepath.Join(dir, "config"),
		RunDir(dir):              filepath.Join(dir, "run"),
		PluginsDir(dir):          filepath.Join(dir, "plugins"),
		RuntimeConfigFile(dir):   filepath.Join(dir, "config", "runtime.toml"),
		RuntimeInstanceFile(dir): filepath.Join(dir, "run", "runtime-instance.json"),
		ReloadTouchFile(dir):     filepath.Join(dir, "run", "reload.touch"),
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestAccessorsDoNotCreateDirs(t *testing.T) {
	dir := t.TempDir()
	_ = ConfigDir(dir)
	_ = RunDir(dir)
	_ = PluginsDir(dir)
	for _, sub := range []string{"config", "run", "plugins"} {
		if _, err := os.Stat(filepath.Join(dir, sub)); !os.IsNotExist(err) {
			t.Errorf("%s should not have been created (err=%v)", sub, err)
		}
	}
}

func TestExpandHandlesTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no user home dir")
	}
	if got := Expand("~/foo"); got != filepath.Join(home, "foo") {
		t.Errorf("Expand(~/foo) = %q, want %q", got, filepath.Join(home, "foo"))
	}
	if got := Expand("~"); got != filepath.Clean(home) {
		t.Errorf("Expand(~) = %q, want %q", got, home)
	}
}

func TestExpandMakesRelativeAbsolute(t *testing.T) {
	got := Expand("some/relative/path")
	if !filepath.IsAbs(got) {
		t.Fatalf("Expand should produce absolute path, got %q", got)
	}
	if !strings.HasSuffix(got, filepath.Join("some", "relative", "path")) {
		t.Errorf("unexpected Expand suffix: %q", got)
	}
}

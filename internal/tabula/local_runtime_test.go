package tabula

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
)

func TestWriteLocalRuntimeConfig_RoundTrip(t *testing.T) {
	tabulaHome := t.TempDir()
	first := filepath.Join(tabulaHome, "plugins", "alpha", "plugin.toml")
	second := filepath.Join(tabulaHome, "plugins", "beta", "plugin.toml")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir plugin dir: %v", err)
		}
		if err := os.WriteFile(path, []byte("id = \"stub\"\n"), 0o644); err != nil {
			t.Fatalf("write plugin manifest stub: %v", err)
		}
	}

	if err := writeLocalRuntimeConfig(tabulaHome, []bootPluginEntry{{ManifestPath: second}, {ManifestPath: first}, {ManifestPath: second}}); err != nil {
		t.Fatalf("writeLocalRuntimeConfig: %v", err)
	}

	configPath := filepath.Join(tabulaHome, "config", "runtime.toml")
	cfg, err := runtimeconfig.Load(configPath)
	if err != nil {
		t.Fatalf("runtimeconfig.Load: %v", err)
	}
	kernelCfg, err := cfg.SingleKernel()
	if err != nil {
		t.Fatalf("cfg.SingleKernel: %v", err)
	}
	if kernelCfg.ID != runtimeauth.DefaultKernelID {
		t.Fatalf("expected kernel id %q, got %q", runtimeauth.DefaultKernelID, kernelCfg.ID)
	}
	if kernelCfg.URL != "unix://"+localRuntimeSocketPath(tabulaHome) {
		t.Fatalf("unexpected runtime url: %q", kernelCfg.URL)
	}
	if kernelCfg.TokenFile != runtimeauth.RuntimeTokenPath(tabulaHome) {
		t.Fatalf("unexpected token file: %q", kernelCfg.TokenFile)
	}
	if want := []string{"*"}; !reflect.DeepEqual(kernelCfg.Tenants, want) {
		t.Fatalf("expected kernel tenants %v, got %v", want, kernelCfg.Tenants)
	}
	if want := []string{first, second}; !reflect.DeepEqual(cfg.PluginDirs, want) {
		t.Fatalf("expected plugin dirs %v, got %v", want, cfg.PluginDirs)
	}
	if want := []string{filepath.Join(tabulaHome, "skills")}; !reflect.DeepEqual(cfg.SkillDirs, want) {
		t.Fatalf("expected skill dirs %v, got %v", want, cfg.SkillDirs)
	}
}

func TestWriteLocalRuntimeConfig_ResetsExistingTenantAllowlistByDefault(t *testing.T) {
	tabulaHome := t.TempDir()
	configPath := filepath.Join(tabulaHome, "config", "runtime.toml")
	if err := runtimeconfig.Save(configPath, runtimeconfig.Config{
		Kernels: []runtimeconfig.Kernel{{
			ID:        runtimeauth.DefaultKernelID,
			URL:       "unix:///tmp/old-runtime.sock",
			TokenFile: "/tmp/old-runtime-token",
			Tenants:   []string{"alpha"},
		}},
		PluginDirs: []string{"/tmp/old-plugin.toml"},
		SkillDirs:  []string{"/tmp/old-skills"},
	}); err != nil {
		t.Fatalf("seed runtime config: %v", err)
	}

	manifestPath := filepath.Join(tabulaHome, "plugins", "alpha", "plugin.toml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte("id = \"stub\"\n"), 0o644); err != nil {
		t.Fatalf("write plugin manifest stub: %v", err)
	}

	if err := writeLocalRuntimeConfig(tabulaHome, []bootPluginEntry{{ManifestPath: manifestPath}}); err != nil {
		t.Fatalf("writeLocalRuntimeConfig: %v", err)
	}

	cfg, err := runtimeconfig.Load(configPath)
	if err != nil {
		t.Fatalf("runtimeconfig.Load: %v", err)
	}
	kernelCfg, err := cfg.SingleKernel()
	if err != nil {
		t.Fatalf("cfg.SingleKernel: %v", err)
	}
	if want := []string{"*"}; !reflect.DeepEqual(kernelCfg.Tenants, want) {
		t.Fatalf("expected reset kernel tenants %v, got %v", want, kernelCfg.Tenants)
	}
}

func TestWriteLocalRuntimeConfig_PreservesExistingRuntimeConfigWhenRequested(t *testing.T) {
	t.Setenv("TABULA_PRESERVE_RUNTIME_CONFIG", "1")
	tabulaHome := t.TempDir()
	configPath := filepath.Join(tabulaHome, "config", "runtime.toml")
	if err := runtimeconfig.Save(configPath, runtimeconfig.Config{
		Kernels: []runtimeconfig.Kernel{{
			ID:        runtimeauth.DefaultKernelID,
			URL:       "unix:///tmp/runtime.sock",
			TokenFile: "/tmp/runtime.token",
			Tenants:   []string{"claw-tabula"},
		}},
		PluginDirs: []string{filepath.Join(tabulaHome, "tenants", "claw-tabula", "plugins")},
		SkillDirs:  []string{filepath.Join(tabulaHome, "tenants", "claw-tabula", "skills")},
	}); err != nil {
		t.Fatalf("seed runtime config: %v", err)
	}
	manifestPath := filepath.Join(tabulaHome, "plugins", "global", "plugin.toml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte("id = \"global\"\n"), 0o644); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}

	if err := writeLocalRuntimeConfig(tabulaHome, []bootPluginEntry{{ManifestPath: manifestPath}}); err != nil {
		t.Fatalf("writeLocalRuntimeConfig: %v", err)
	}

	cfg, err := runtimeconfig.Load(configPath)
	if err != nil {
		t.Fatalf("runtimeconfig.Load: %v", err)
	}
	if want := []string{filepath.Join(tabulaHome, "tenants", "claw-tabula", "plugins")}; !reflect.DeepEqual(cfg.PluginDirs, want) {
		t.Fatalf("plugin dirs = %#v, want %#v", cfg.PluginDirs, want)
	}
}

func TestLocalRuntimeSocketPathUsesShortTempPathForLongHome(t *testing.T) {
	tabulaHome := filepath.Join(t.TempDir(), strings.Repeat("long", 30))
	path := localRuntimeSocketPath(tabulaHome)

	if len(path) > 100 {
		t.Fatalf("socket path too long: %d %s", len(path), path)
	}
	if !strings.Contains(path, "tabula-rt-") {
		t.Fatalf("socket path missing stable temp prefix: %s", path)
	}
}

func TestLocalRuntimePluginPaths_DefaultsWhenNoEntries(t *testing.T) {
	tabulaHome := t.TempDir()
	got, err := localRuntimePluginPaths(tabulaHome, nil)
	if err != nil {
		t.Fatalf("localRuntimePluginPaths: %v", err)
	}
	want := []string{filepath.Join(tabulaHome, "plugins")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected default plugin path %v, got %v", want, got)
	}
}

func TestLocalRuntimePluginPaths_RejectsBlankManifestPath(t *testing.T) {
	_, err := localRuntimePluginPaths(t.TempDir(), []bootPluginEntry{{ManifestPath: "  "}})
	if err == nil {
		t.Fatal("expected blank manifest path error")
	}
}

func TestResolveLocalRuntimeBinary_PrefersSibling(t *testing.T) {
	dir := t.TempDir()
	tabulaPath := filepath.Join(dir, "tabula")
	runtimePath := filepath.Join(dir, localRuntimeBinaryName())
	for _, path := range []string{tabulaPath, runtimePath} {
		if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
			t.Fatalf("write stub binary: %v", err)
		}
	}

	got, err := resolveLocalRuntimeBinary(tabulaPath, func(string) (string, error) {
		return filepath.Join(dir, "bin", localRuntimeBinaryName()), nil
	})
	if err != nil {
		t.Fatalf("resolveLocalRuntimeBinary: %v", err)
	}
	if got != runtimePath {
		t.Fatalf("expected sibling runtime binary %q, got %q", runtimePath, got)
	}
}

func TestResolveLocalRuntimeBinary_FallsBackToPath(t *testing.T) {
	want := filepath.Join(string(filepath.Separator), "usr", "local", "bin", localRuntimeBinaryName())
	got, err := resolveLocalRuntimeBinary("", func(name string) (string, error) {
		if name != localRuntimeBinaryName() {
			t.Fatalf("unexpected lookup %q", name)
		}
		return want, nil
	})
	if err != nil {
		t.Fatalf("resolveLocalRuntimeBinary: %v", err)
	}
	if got != want {
		t.Fatalf("expected PATH runtime binary %q, got %q", want, got)
	}
}

func TestResolveLocalRuntimeBinary_ErrorsWhenUnavailable(t *testing.T) {
	_, err := resolveLocalRuntimeBinary("", func(string) (string, error) {
		return "", errors.New("missing")
	})
	if err == nil {
		t.Fatal("expected lookup failure")
	}
}

func TestNewLocalRuntimeCommand_UsesResolvedBinaryAndConfig(t *testing.T) {
	tabulaHome := t.TempDir()
	tabulaPath := filepath.Join(t.TempDir(), "tabula")
	if err := os.WriteFile(tabulaPath, []byte("stub"), 0o755); err != nil {
		t.Fatalf("write tabula stub: %v", err)
	}
	var lookedUp string
	cmd, err := newLocalRuntimeCommand(tabulaPath, tabulaHome, nil, func(name string) (string, error) {
		lookedUp = name
		return filepath.Join(string(filepath.Separator), "usr", "local", "bin", name), nil
	})
	if err != nil {
		t.Fatalf("newLocalRuntimeCommand: %v", err)
	}
	if lookedUp != localRuntimeBinaryName() {
		t.Fatalf("looked up %q", lookedUp)
	}
	if got, want := cmd.Path, filepath.Join(string(filepath.Separator), "usr", "local", "bin", localRuntimeBinaryName()); got != want {
		t.Fatalf("cmd.Path = %q, want %q", got, want)
	}
	wantArgs := []string{cmd.Path, "start", "--config", filepath.Join(tabulaHome, "config", "runtime.toml"), "--runtime-id", runtimeauth.LocalRuntimeID}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("cmd.Args = %#v, want %#v", cmd.Args, wantArgs)
	}
	if env := lastEnvValue(cmd.Env, "TABULA_HOME"); env != tabulaHome {
		t.Fatalf("TABULA_HOME env = %q", env)
	}
}

func TestManagedLocalRuntimePIDAndWaitForAttachment(t *testing.T) {
	cmd := helperLocalRuntimeCommand(t, "sleep")
	if err := cmd.Start(); err != nil {
		t.Fatalf("helper start: %v", err)
	}
	runtimeProc := newManagedLocalRuntime(cmd)
	if runtimeProc.PID() <= 0 {
		t.Fatalf("expected positive pid, got %d", runtimeProc.PID())
	}
	var attached atomic.Bool
	go func() {
		time.Sleep(50 * time.Millisecond)
		attached.Store(true)
	}()
	if err := runtimeProc.WaitForAttachment(attached.Load, 3*time.Second); err != nil {
		t.Fatalf("WaitForAttachment: %v", err)
	}
	if err := runtimeProc.Shutdown(3 * time.Second); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := runtimeProc.Wait(); err != nil {
		t.Fatalf("Wait after shutdown: %v", err)
	}
}

func TestManagedLocalRuntimeWaitForAttachmentFailsWhenProcessExits(t *testing.T) {
	cmd := helperLocalRuntimeCommand(t, "exit")
	if err := cmd.Start(); err != nil {
		t.Fatalf("helper start: %v", err)
	}
	runtimeProc := newManagedLocalRuntime(cmd)
	err := runtimeProc.WaitForAttachment(func() bool { return false }, 3*time.Second)
	if err == nil {
		t.Fatal("expected attach failure when process exits early")
	}
	if !strings.Contains(err.Error(), "exited before attach") {
		t.Fatalf("expected exited-before-attach error, got %q", err)
	}
	if waitErr := runtimeProc.Wait(); waitErr != nil {
		t.Fatalf("Wait returned unexpected error: %v", waitErr)
	}
}

func helperLocalRuntimeCommand(t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperLocalRuntimeProcess", "--")
	cmd.Env = append(os.Environ(), "TABULA_TEST_LOCAL_RUNTIME_HELPER=1", "TABULA_TEST_LOCAL_RUNTIME_MODE="+mode)
	return cmd
}

func TestHelperLocalRuntimeProcess(t *testing.T) {
	if os.Getenv("TABULA_TEST_LOCAL_RUNTIME_HELPER") != "1" {
		return
	}
	mode := os.Getenv("TABULA_TEST_LOCAL_RUNTIME_MODE")
	switch mode {
	case "exit":
		os.Exit(0)
	case "sleep":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		<-ctx.Done()
		os.Exit(0)
	default:
		os.Exit(2)
	}
}

func lastEnvValue(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimPrefix(env[i], prefix)
		}
	}
	return ""
}

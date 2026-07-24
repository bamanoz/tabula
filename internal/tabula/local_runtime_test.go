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
)

func TestEnsureRuntimeConfigExists_PassesWhenFilePresent(t *testing.T) {
	tabulaHome := t.TempDir()
	configPath := filepath.Join(tabulaHome, "config", "runtime.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("plugin_dirs = []\n"), 0o644); err != nil {
		t.Fatalf("seed runtime.toml: %v", err)
	}
	if err := ensureRuntimeConfigExists(tabulaHome); err != nil {
		t.Fatalf("ensureRuntimeConfigExists: %v", err)
	}
}

func TestEnsureRuntimeConfigExists_FailsWhenMissing(t *testing.T) {
	tabulaHome := t.TempDir()
	err := ensureRuntimeConfigExists(tabulaHome)
	if err == nil {
		t.Fatal("expected error when runtime.toml is missing")
	}
	if !strings.Contains(err.Error(), "tabula-install") {
		t.Fatalf("error must guide the user to run the installer: %v", err)
	}
}

func TestEnsureRuntimeConfigExists_FailsWhenPathIsDirectory(t *testing.T) {
	tabulaHome := t.TempDir()
	configPath := filepath.Join(tabulaHome, "config", "runtime.toml")
	if err := os.MkdirAll(configPath, 0o755); err != nil {
		t.Fatalf("mkdir runtime.toml as dir: %v", err)
	}
	err := ensureRuntimeConfigExists(tabulaHome)
	if err == nil {
		t.Fatal("expected error when runtime.toml is a directory")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureRuntimeConfigExists_RejectsBlankHome(t *testing.T) {
	if err := ensureRuntimeConfigExists("   "); err == nil {
		t.Fatal("expected error for blank TABULA_HOME")
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

func TestLocalRuntimeSocketPathUsesEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "runtime.sock")
	t.Setenv("TABULA_RUNTIME_SOCKET_PATH", override)

	if got := localRuntimeSocketPath(t.TempDir()); got != override {
		t.Fatalf("socket path = %q, want %q", got, override)
	}
}

func TestManagedLocalRuntimeSocketPathUsesRuntimeConfig(t *testing.T) {
	tabulaHome := t.TempDir()
	socketPath := filepath.Join(t.TempDir(), "runtime", "configured.sock")
	configDir := filepath.Join(tabulaHome, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	runtimeConfig := strings.Join([]string{
		`plugin_dirs = ["/plugins"]`,
		`skill_dirs = ["/skills"]`,
		`[[kernel]]`,
		`id = "main"`,
		`url = "unix://` + socketPath + `"`,
		`token_file = "` + filepath.Join(tabulaHome, "run", "runtime-token") + `"`,
		`tenants = ["*"]`,
		``,
	}, "\n")
	if err := os.WriteFile(filepath.Join(configDir, "runtime.toml"), []byte(runtimeConfig), 0o644); err != nil {
		t.Fatalf("write runtime.toml: %v", err)
	}

	got, err := managedLocalRuntimeSocketPath(tabulaHome)
	if err != nil {
		t.Fatalf("managedLocalRuntimeSocketPath: %v", err)
	}
	if got != socketPath {
		t.Fatalf("socket path = %q, want %q", got, socketPath)
	}
}

func TestUnixRuntimeSocketPathRejectsNonUnixURL(t *testing.T) {
	_, err := unixRuntimeSocketPath("ws://127.0.0.1:8089/ws")
	if err == nil {
		t.Fatal("expected non-unix URL to fail")
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
	cmd, err := newLocalRuntimeCommandForOS("linux", tabulaPath, tabulaHome, nil, func(name string) (string, error) {
		lookedUp = name
		return filepath.Join(string(filepath.Separator), "usr", "local", "bin", name), nil
	})
	if err != nil {
		t.Fatalf("newLocalRuntimeCommandForOS: %v", err)
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

func TestNewLocalRuntimeCommandForWindows_UsesTabulaHostSubcommand(t *testing.T) {
	tabulaHome := t.TempDir()
	tabulaPath := filepath.Join(t.TempDir(), "tabula.exe")
	cmd, err := newLocalRuntimeCommandForOS("windows", tabulaPath, tabulaHome, nil, func(name string) (string, error) {
		t.Fatalf("unexpected runtime binary lookup %q", name)
		return "", nil
	})
	if err != nil {
		t.Fatalf("newLocalRuntimeCommandForOS: %v", err)
	}
	if cmd.Path != tabulaPath {
		t.Fatalf("cmd.Path = %q, want %q", cmd.Path, tabulaPath)
	}
	wantArgs := []string{
		tabulaPath,
		"runtime",
		"host",
		"start",
		"--config",
		filepath.Join(tabulaHome, "config", "runtime.toml"),
		"--runtime-id",
		runtimeauth.LocalRuntimeID,
	}
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

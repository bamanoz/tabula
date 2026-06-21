package tabula

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDotenvKernelLoadsAndBootInheritsEnv verifies that the kernel is the
// single canonical loader of $TABULA_HOME/.env: after loadEnvFile runs,
// any subprocess started through the shared shell command helper inherits
// the loaded variables via os.Environ().
//
// This is the end-to-end contract that distro boot scripts depend on —
// they read only os.environ and never load .env themselves.
func TestDotenvKernelLoadsAndBootInheritsEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	contents := "TABULA_TEST_FROM_FILE=file_value\nTABULA_TEST_FROM_SHELL=file_value\n"
	if err := os.WriteFile(envPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	// Shell-set value must win over the file value.
	t.Setenv("TABULA_TEST_FROM_SHELL", "shell_value")
	// Ensure the file-only var is not pre-populated.
	os.Unsetenv("TABULA_TEST_FROM_FILE")
	t.Cleanup(func() {
		os.Unsetenv("TABULA_TEST_FROM_FILE")
	})

	loadEnvFile(envPath)

	// Sanity: kernel process sees both values, shell override wins.
	if got := os.Getenv("TABULA_TEST_FROM_FILE"); got != "file_value" {
		t.Fatalf("kernel env TABULA_TEST_FROM_FILE = %q, want %q", got, "file_value")
	}
	if got := os.Getenv("TABULA_TEST_FROM_SHELL"); got != "shell_value" {
		t.Fatalf("kernel env TABULA_TEST_FROM_SHELL = %q, want %q", got, "shell_value")
	}

	// Child subprocesses must inherit both vars.
	cmd := mainShellCommand(`printf "FROM_FILE=%s\nFROM_SHELL=%s\n" "$TABULA_TEST_FROM_FILE" "$TABULA_TEST_FROM_SHELL"`)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("boot subprocess failed: %v; stderr=%s", err, stderr.String())
	}
	got := string(out)
	if !strings.Contains(got, "FROM_FILE=file_value") {
		t.Errorf("boot subprocess did not inherit file value: %q", got)
	}
	if !strings.Contains(got, "FROM_SHELL=shell_value") {
		t.Errorf("boot subprocess did not inherit shell override: %q", got)
	}
}

// TestDotenvMissingFileIsSilent ensures the kernel does not panic or
// otherwise misbehave when $TABULA_HOME/.env is absent.
func TestDotenvMissingFileIsSilent(t *testing.T) {
	dir := t.TempDir()
	loadEnvFile(filepath.Join(dir, ".env"))
}

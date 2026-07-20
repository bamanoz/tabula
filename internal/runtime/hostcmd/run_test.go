package hostcmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareRuntimeEnvironmentCanPreserveTruncatedPath(t *testing.T) {
	tabulaHome := t.TempDir()
	truncatedPath := `C:\repo\.tabula-dev\.venv-Windows-AMD64\Scripts;C:\repo\.tabula-dev\bin`
	fullPath := `C:\Windows\System32;C:\Program Files\Git\cmd;C:\Program Files\Go\bin`

	oldPath, hadPath := os.LookupEnv("PATH")
	oldTabulaPath, hadTabulaPath := os.LookupEnv("TABULA_PATH")
	t.Cleanup(func() {
		if hadPath {
			if err := os.Setenv("PATH", oldPath); err != nil {
				t.Fatalf("restore PATH: %v", err)
			}
		} else {
			if err := os.Unsetenv("PATH"); err != nil {
				t.Fatalf("unset PATH: %v", err)
			}
		}
		if hadTabulaPath {
			if err := os.Setenv("TABULA_PATH", oldTabulaPath); err != nil {
				t.Fatalf("restore TABULA_PATH: %v", err)
			}
		} else {
			if err := os.Unsetenv("TABULA_PATH"); err != nil {
				t.Fatalf("unset TABULA_PATH: %v", err)
			}
		}
	})

	if err := os.Setenv("PATH", fullPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	if err := os.Unsetenv("TABULA_PATH"); err != nil {
		t.Fatalf("unset TABULA_PATH: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tabulaHome, ".env"), []byte("TABULA_PATH="+truncatedPath+"\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	prepareRuntimeEnvironment(tabulaHome)

	if got := os.Getenv("PATH"); got != truncatedPath {
		t.Fatalf("PATH = %q, want truncated TABULA_PATH %q", got, truncatedPath)
	}
}

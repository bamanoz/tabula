package clientauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const KernelClientTokenFileName = "kernel-client-token"

// Path returns the local kernel client token path under TABULA_HOME.
func Path(tabulaHome string) string {
	return filepath.Join(tabulaHome, "run", KernelClientTokenFileName)
}

// Generate returns a local bearer token for kernel WebSocket clients.
func Generate() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate kernel client token: %w", err)
	}
	return "ktk_" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// WriteFile writes token with run-dir 0700 and file 0600 permissions.
func WriteFile(path, token string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("kernel client token path is required")
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("kernel client token is required")
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create kernel client token dir: %w", err)
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return fmt.Errorf("chmod kernel client token dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("write kernel client token file: %w", err)
	}
	_, writeErr := f.WriteString(token + "\n")
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write kernel client token file: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close kernel client token file: %w", closeErr)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod kernel client token file: %w", err)
	}
	return nil
}

func Matches(expected, got string) bool {
	expected = strings.TrimSpace(expected)
	got = strings.TrimSpace(got)
	if expected == "" || got == "" {
		return false
	}
	if len(expected) != len(got) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(got)) == 1
}

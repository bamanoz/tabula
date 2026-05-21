package kernel

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

// KernelClientTokenPath returns the local kernel client token path under TABULA_HOME.
func KernelClientTokenPath(tabulaHome string) string {
	return filepath.Join(tabulaHome, "run", KernelClientTokenFileName)
}

// IssueKernelClientTokenFile generates and writes a fresh kernel client token.
func IssueKernelClientTokenFile(path string) (string, error) {
	token, err := GenerateKernelClientToken()
	if err != nil {
		return "", err
	}
	if err := WriteKernelClientTokenFile(path, token); err != nil {
		return "", err
	}
	return token, nil
}

// GenerateKernelClientToken returns a local bearer token for kernel WebSocket clients.
func GenerateKernelClientToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate kernel client token: %w", err)
	}
	return "ktk_" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// WriteKernelClientTokenFile writes token with run-dir 0700 and file 0600 permissions.
func WriteKernelClientTokenFile(path, token string) error {
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

func tokenMatches(expected, got string) bool {
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

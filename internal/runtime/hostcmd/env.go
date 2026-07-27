package hostcmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func prepareRuntimeEnvironment(tabulaHome string) {
	loadRuntimeEnvFile(filepath.Join(tabulaHome, ".env"))
	if savedPath := strings.TrimSpace(os.Getenv("TABULA_PATH")); savedPath != "" {
		_ = os.Setenv("PATH", savedPath)
	}
}

func loadRuntimeEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	for scanner := bufio.NewScanner(f); scanner.Scan(); {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, strings.TrimSpace(value))
	}
}

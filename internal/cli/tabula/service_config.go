package tabula

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/bamanoz/tabula/internal/layout"
	"github.com/bamanoz/tabula/internal/logging"
)

type kernelServiceConfig struct {
	Home       string
	Kernel     *kernelConfig
	ListenAddr string
	WSEndpoint string
	Logging    logging.Config
	SavedPath  string
}

func loadKernelServiceConfig() (*kernelServiceConfig, error) {
	home := layout.Home()
	loadEnvFile(filepath.Join(home, ".env"))

	kernelCfg, err := loadKernelConfigFile(home)
	if err != nil {
		return nil, fmt.Errorf("kernel config failed: %w", err)
	}
	listenAddr, err := listenAddrFromKernelURL(kernelCfg.URL)
	if err != nil {
		return nil, err
	}
	return &kernelServiceConfig{
		Home:       home,
		Kernel:     kernelCfg,
		ListenAddr: listenAddr,
		WSEndpoint: kernelCfg.URL,
		Logging:    kernelLoggingConfig(home),
		SavedPath:  strings.TrimSpace(os.Getenv("TABULA_PATH")),
	}, nil
}

func kernelLoggingConfig(home string) logging.Config {
	return logging.ApplyEnv(logging.Config{
		Component: "kernel",
		FilePath:  filepath.Join(home, "logs", "kernel.log"),
		Compress:  true,
	}, "TABULA")
}

func listenAddrFromKernelURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid url %q: %w", rawURL, err)
	}
	listenAddr := u.Host
	if !strings.Contains(listenAddr, ":") {
		listenAddr += ":8089"
	}
	return listenAddr, nil
}

package tabula

import (
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bamanoz/tabula/internal/kernel"
	"github.com/bamanoz/tabula/internal/runtime/transport/wss"
	"github.com/bamanoz/tabula/internal/runtime/wire"

	"github.com/BurntSushi/toml"
)

type kernelConfigFile struct {
	Kernel     kernelConfigKernel     `toml:"kernel"`
	RuntimeWSS kernelConfigRuntimeWSS `toml:"runtime_wss"`
}

type kernelConfig struct {
	URL        string
	RuntimeWSS *runtimeWSSEndpoint
}

type kernelConfigKernel struct {
	URL string `toml:"url"`
}

type kernelConfigRuntimeWSS struct {
	Enabled    bool     `toml:"enabled"`
	Listen     string   `toml:"listen"`
	Path       string   `toml:"path"`
	Origins    []string `toml:"origins"`
	CertFile   string   `toml:"cert_file"`
	KeyFile    string   `toml:"key_file"`
	ClientCA   string   `toml:"client_ca"`
	ClientAuth string   `toml:"client_auth"`
}

type runtimeWSSEndpoint struct {
	Listen     string
	Path       string
	Origins    []string
	CertFile   string
	KeyFile    string
	ClientCA   string
	ClientAuth string
}

func loadKernelConfigFile(tabulaHome string) (*kernelConfig, error) {
	return loadKernelConfig(filepath.Join(tabulaHome, "config", "kernel.toml"))
}

func loadKernelConfig(path string) (*kernelConfig, error) {
	var cfg kernelConfigFile
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("load kernel config %s: %w", path, err)
	}
	url := strings.TrimSpace(os.ExpandEnv(cfg.Kernel.URL))
	if envURL := strings.TrimSpace(os.Getenv("TABULA_URL")); envURL != "" {
		url = envURL
	}
	if url == "" {
		return nil, fmt.Errorf("kernel.url is required in %s", path)
	}
	out := &kernelConfig{URL: url}
	if cfg.RuntimeWSS.Enabled {
		out.RuntimeWSS = &runtimeWSSEndpoint{
			Listen:     strings.TrimSpace(os.ExpandEnv(cfg.RuntimeWSS.Listen)),
			Path:       strings.TrimSpace(cfg.RuntimeWSS.Path),
			Origins:    cfg.RuntimeWSS.Origins,
			CertFile:   strings.TrimSpace(os.ExpandEnv(cfg.RuntimeWSS.CertFile)),
			KeyFile:    strings.TrimSpace(os.ExpandEnv(cfg.RuntimeWSS.KeyFile)),
			ClientCA:   strings.TrimSpace(os.ExpandEnv(cfg.RuntimeWSS.ClientCA)),
			ClientAuth: strings.TrimSpace(cfg.RuntimeWSS.ClientAuth),
		}
	}
	return out, nil
}

func (c *kernelConfig) runtimeWSSEndpoint() (runtimeWSSEndpoint, bool) {
	if c == nil || c.RuntimeWSS == nil {
		return runtimeWSSEndpoint{}, false
	}
	endpoint := *c.RuntimeWSS
	if strings.TrimSpace(endpoint.Path) == "" {
		endpoint.Path = wss.DefaultPath
	}
	return endpoint, true
}

func runtimePeerCertValidator(hub *kernel.Hub) func(*x509.Certificate) error {
	return func(cert *x509.Certificate) error {
		if cert == nil {
			return nil
		}
		cn := strings.TrimSpace(cert.Subject.CommonName)
		if cn == "" {
			return &wss.RejectError{Status: http.StatusUnauthorized, Code: string(wire.ErrorUnknownRuntime), Message: "client certificate common name is required"}
		}
		if hub == nil || !hub.RuntimeConfigured(cn) {
			return &wss.RejectError{Status: http.StatusUnauthorized, Code: string(wire.ErrorUnknownRuntime), Message: fmt.Sprintf("runtime %q is not configured", cn)}
		}
		return nil
	}
}

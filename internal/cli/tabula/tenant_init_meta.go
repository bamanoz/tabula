package tabula

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/bamanoz/tabula/internal/kernel"
)

type tenantInitMetaConfig struct {
	Agent struct {
		PromptBuilder string `toml:"prompt_builder"`
	} `toml:"agent"`
	Workspace struct {
		ProjectRoot string `toml:"project_root"`
	} `toml:"workspace"`
	Claw struct {
		Workspace struct {
			Path string `toml:"path"`
		} `toml:"workspace"`
	} `toml:"claw"`
}

func configureTenantInitMeta(tabulaHome string, hub *kernel.Hub) error {
	if hub == nil || strings.TrimSpace(tabulaHome) == "" {
		return nil
	}
	root := filepath.Join(tabulaHome, "tenants")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		meta, err := loadTenantInitMeta(tabulaHome, entry.Name())
		if err != nil {
			return err
		}
		if len(meta) > 0 {
			hub.SetTenantInitMeta(entry.Name(), meta)
		}
	}
	return nil
}

func loadTenantInitMeta(tabulaHome, tenantID string) (json.RawMessage, error) {
	path := filepath.Join(tabulaHome, "tenants", tenantID, "config", "tenant.toml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var cfg tenantInitMetaConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("load tenant init meta %s: %w", path, err)
	}
	meta := map[string]any{}
	if promptBuilder := strings.TrimSpace(cfg.Agent.PromptBuilder); promptBuilder != "" {
		meta["prompt_builder"] = promptBuilder
	}
	workspacePath := strings.TrimSpace(cfg.Workspace.ProjectRoot)
	if workspacePath == "" {
		workspacePath = strings.TrimSpace(cfg.Claw.Workspace.Path)
	}
	if workspacePath != "" {
		meta["workspace"] = map[string]any{"path": workspacePath, "source": "tenant", "kind": "assistant"}
	}
	if len(meta) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

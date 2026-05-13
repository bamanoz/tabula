// Package config loads tenant-aware kernel configuration overlays.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/bamanoz/tabula/internal/tenant"
)

type Document map[string]any

type Loader struct {
	home string
}

func NewLoader(tabulaHome string) *Loader {
	return &Loader{home: filepath.Clean(strings.TrimSpace(tabulaHome))}
}

func (l *Loader) LoadTenant(tenantID string) (Document, error) {
	if err := tenant.ValidateID(tenantID); err != nil {
		return nil, err
	}
	merged := Document{}
	for _, path := range []string{
		filepath.Join(l.home, "config", "global.toml"),
		filepath.Join(l.home, "tenants", tenantID, "config", "tenant.toml"),
	} {
		doc, err := loadDocument(path)
		if err != nil {
			return nil, err
		}
		merged = mergeDocuments(merged, doc)
	}
	return merged, nil
}

func (l *Loader) LoadPlugin(tenantID, pluginID string) (Document, error) {
	if err := tenant.ValidateID(tenantID); err != nil {
		return nil, err
	}
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return nil, fmt.Errorf("plugin id is required")
	}
	tenantPath := filepath.Join(l.home, "tenants", tenantID, "config", "plugins", pluginID, "config.toml")
	if _, err := os.Stat(tenantPath); err == nil {
		return loadDocument(tenantPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return loadDocument(filepath.Join(l.home, "config", "plugins", pluginID, "config.toml"))
}

func loadDocument(path string) (Document, error) {
	var doc Document
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Document{}, nil
		}
		return nil, err
	}
	if _, err := toml.DecodeFile(path, &doc); err != nil {
		return nil, fmt.Errorf("load config %s: %w", path, err)
	}
	if doc == nil {
		return Document{}, nil
	}
	return cloneDocument(doc), nil
}

func mergeDocuments(base, overlay Document) Document {
	out := cloneDocument(base)
	for key, value := range overlay {
		existing, existingOK := documentValue(out[key])
		next, nextOK := documentValue(value)
		if existingOK && nextOK {
			out[key] = mergeDocuments(existing, next)
			continue
		}
		out[key] = cloneValue(value)
	}
	return out
}

func cloneDocument(in map[string]any) Document {
	out := Document{}
	for key, value := range in {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case Document:
		return cloneDocument(typed)
	case map[string]any:
		return cloneDocument(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return typed
	}
}

func documentValue(value any) (Document, bool) {
	switch typed := value.(type) {
	case Document:
		return typed, true
	case map[string]any:
		return Document(typed), true
	default:
		return nil, false
	}
}

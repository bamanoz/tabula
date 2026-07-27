// Package inspect builds read-only config and health reports from runtime.toml.
package inspect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/bamanoz/tabula/internal/layout"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy"
	"github.com/bamanoz/tabula/internal/runtime/host/policy/bare"
	runtimewire "github.com/bamanoz/tabula/internal/runtime/wire"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
	"github.com/bamanoz/tabula/internal/tenant"
)

const healthToolName = "health"

type document map[string]any

type Options struct {
	Home     string
	TenantID string
	PluginID string
}

type Report struct {
	Paths   PathReport     `json:"paths"`
	Runtime RuntimeReport  `json:"runtime"`
	Distro  DistroReport   `json:"distro,omitempty"`
	Tenants []TenantReport `json:"tenants"`
	Plugins []PluginReport `json:"plugins"`
	Plugin  *PluginConfig  `json:"plugin,omitempty"`
}

type PathReport struct {
	Home          string `json:"home"`
	ConfigDir     string `json:"config_dir"`
	StateDir      string `json:"state_dir"`
	DataDir       string `json:"data_dir"`
	CacheDir      string `json:"cache_dir"`
	RunDir        string `json:"run_dir"`
	LogsDir       string `json:"logs_dir"`
	PluginsDir    string `json:"plugins_dir"`
	SkillsDir     string `json:"skills_dir"`
	TenantsDir    string `json:"tenants_dir"`
	RuntimeConfig string `json:"runtime_config"`
}

type RuntimeReport struct {
	Source     string         `json:"source"`
	Kernels    []KernelReport `json:"kernels"`
	PluginDirs []string       `json:"plugin_dirs"`
	SkillDirs  []string       `json:"skill_dirs"`
}

type KernelReport struct {
	ID      string   `json:"id"`
	URL     string   `json:"url"`
	Tenants []string `json:"tenants"`
}

type DistroReport struct {
	Active string `json:"active"`
	Dir    string `json:"dir"`
}

type TenantReport struct {
	ID         string   `json:"id"`
	PluginDirs []string `json:"plugin_dirs"`
	SkillDirs  []string `json:"skill_dirs"`
	OverlayDir string   `json:"overlay_dir"`
	Selected   bool     `json:"selected,omitempty"`
}

type PluginReport struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Runtime      string   `json:"runtime"`
	PluginDir    string   `json:"plugin_dir"`
	ManifestPath string   `json:"manifest_path"`
	Tools        []string `json:"tools"`
	Enabled      bool     `json:"enabled"`
}

type PluginConfig struct {
	ID      string            `json:"id"`
	Tenant  string            `json:"tenant"`
	Sources []PluginConfigSrc `json:"sources"`
	Config  map[string]any    `json:"config"`
}

type PluginConfigSrc struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

type HealthReport struct {
	Plugins []PluginHealth `json:"plugins"`
}

type PluginHealth struct {
	PluginID string          `json:"plugin_id"`
	Status   string          `json:"status"`
	Messages []HealthMessage `json:"messages"`
}

type HealthMessage struct {
	Level string `json:"level"`
	Text  string `json:"text"`
}

func Build(opts Options) (Report, error) {
	home := resolveHome(opts.Home)
	runtimePath := filepath.Join(home, "config", "runtime.toml")
	cfg, err := loadRuntimeConfig(runtimePath)
	if err != nil {
		return Report{}, err
	}
	tenantID := selectTenant(opts.TenantID, cfg)
	report := Report{
		Paths:   pathReport(home),
		Runtime: runtimeReport(runtimePath, cfg),
		Distro:  DistroReport{Active: cfg.Distro.Active, Dir: cfg.Distro.Dir},
		Tenants: tenantReports(home, tenantID, cfg),
	}
	plugins, err := pluginReports(cfg.PluginDirs)
	if err != nil {
		return Report{}, err
	}
	if len(cfg.Tenants) > 0 {
		for _, catalog := range cfg.Tenants {
			tenantPlugins, err := pluginReports(catalog.PluginDirs)
			if err != nil {
				return Report{}, err
			}
			plugins = append(plugins, tenantPlugins...)
		}
	}
	if opts.PluginID != "" {
		pluginConfig, err := loadPluginConfig(home, tenantID, opts.PluginID)
		if err != nil {
			return Report{}, err
		}
		report.Plugin = &pluginConfig
		applyEnabledFromConfig(plugins, opts.PluginID, pluginConfig.Config)
	}
	report.Plugins = plugins
	return report, nil
}

func BuildHealth(ctx context.Context, opts Options) (HealthReport, error) {
	home := resolveHome(opts.Home)
	runtimePath := filepath.Join(home, "config", "runtime.toml")
	cfg, err := loadRuntimeConfig(runtimePath)
	if err != nil {
		return HealthReport{}, err
	}
	tenantID := selectTenant(opts.TenantID, cfg)
	pluginDirs := healthPluginDirs(tenantID, cfg)
	pluginReports, err := pluginReports(pluginDirs)
	if err != nil {
		return HealthReport{}, err
	}
	plugins := make([]PluginHealth, 0, len(pluginReports))
	for _, item := range pluginReports {
		plugin, err := manifest.Load(item.ManifestPath)
		if err != nil {
			return HealthReport{}, err
		}
		if !manifestHasTool(plugin.Tools, healthToolName) {
			plugins = append(plugins, PluginHealth{PluginID: plugin.ID, Status: "skipped", Messages: []HealthMessage{{Level: "info", Text: "plugin does not expose health tool"}}})
			continue
		}
		plugins = append(plugins, invokeHealthTool(ctx, home, tenantID, plugin))
	}
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].PluginID < plugins[j].PluginID })
	return HealthReport{Plugins: plugins}, nil
}

func healthPluginDirs(tenantID string, cfg runtimeconfig.Config) []string {
	dirs := append([]string(nil), cfg.PluginDirs...)
	for _, item := range cfg.Tenants {
		if item.ID == tenantID {
			dirs = append(dirs, item.PluginDirs...)
			break
		}
	}
	return dirs
}

func loadRuntimeConfig(path string) (runtimeconfig.Config, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return runtimeconfig.Config{}, fmt.Errorf("runtime.toml not found at %s; run tabula-install first", path)
		}
		return runtimeconfig.Config{}, fmt.Errorf("stat runtime.toml %s: %w", path, err)
	}
	cfg, err := runtimeconfig.Load(path)
	if err != nil {
		return runtimeconfig.Config{}, err
	}
	return cfg, nil
}

func resolveHome(home string) string {
	home = strings.TrimSpace(home)
	if home == "" {
		return layout.Home()
	}
	if !filepath.IsAbs(home) {
		if abs, err := filepath.Abs(home); err == nil {
			home = abs
		}
	}
	return filepath.Clean(home)
}

func pathReport(home string) PathReport {
	return PathReport{
		Home:          home,
		ConfigDir:     filepath.Join(home, "config"),
		StateDir:      filepath.Join(home, "state"),
		DataDir:       filepath.Join(home, "data"),
		CacheDir:      filepath.Join(home, "cache"),
		RunDir:        filepath.Join(home, "run"),
		LogsDir:       filepath.Join(home, "logs"),
		PluginsDir:    filepath.Join(home, "plugins"),
		SkillsDir:     filepath.Join(home, "skills"),
		TenantsDir:    filepath.Join(home, "tenants"),
		RuntimeConfig: filepath.Join(home, "config", "runtime.toml"),
	}
}

func runtimeReport(path string, cfg runtimeconfig.Config) RuntimeReport {
	kernels := make([]KernelReport, 0, len(cfg.Kernels))
	for _, kernel := range cfg.Kernels {
		kernels = append(kernels, KernelReport{ID: kernel.ID, URL: kernel.URL, Tenants: append([]string(nil), kernel.Tenants...)})
	}
	return RuntimeReport{Source: path, Kernels: kernels, PluginDirs: append([]string(nil), cfg.PluginDirs...), SkillDirs: append([]string(nil), cfg.SkillDirs...)}
}

func tenantReports(home, selected string, cfg runtimeconfig.Config) []TenantReport {
	if len(cfg.Tenants) == 0 {
		return []TenantReport{{ID: selected, OverlayDir: filepath.Join(home, "tenants", selected), Selected: true}}
	}
	reports := make([]TenantReport, 0, len(cfg.Tenants))
	for _, item := range cfg.Tenants {
		reports = append(reports, TenantReport{ID: item.ID, PluginDirs: append([]string(nil), item.PluginDirs...), SkillDirs: append([]string(nil), item.SkillDirs...), OverlayDir: filepath.Join(home, "tenants", item.ID), Selected: item.ID == selected})
	}
	return reports
}

func selectTenant(requested string, cfg runtimeconfig.Config) string {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return requested
	}
	if len(cfg.Tenants) > 0 {
		return cfg.Tenants[0].ID
	}
	for _, kernel := range cfg.Kernels {
		for _, tenantID := range kernel.Tenants {
			if tenantID != "*" {
				return tenantID
			}
		}
	}
	return tenant.DefaultID
}

func pluginReports(pluginDirs []string) ([]PluginReport, error) {
	reports := []PluginReport{}
	seen := map[string]struct{}{}
	for _, root := range pluginDirs {
		paths, err := pluginManifestPaths(root)
		if err != nil {
			return nil, err
		}
		for _, manifestPath := range paths {
			plugin, err := manifest.Load(manifestPath)
			if err != nil {
				return nil, err
			}
			if _, ok := seen[plugin.ID]; ok {
				continue
			}
			seen[plugin.ID] = struct{}{}
			reports = append(reports, PluginReport{ID: plugin.ID, Name: plugin.Name, Version: plugin.Version, Runtime: inspectPluginRuntime(plugin), PluginDir: plugin.RootDir, ManifestPath: manifestPath, Tools: toolNames(plugin.Tools), Enabled: true})
		}
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].ID < reports[j].ID })
	return reports, nil
}

func pluginManifestPaths(root string) ([]string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat plugin search dir %s: %w", root, err)
	}
	if !info.IsDir() {
		if filepath.Base(root) == "plugin.toml" {
			return []string{root}, nil
		}
		return nil, fmt.Errorf("plugin search path %s is not a directory or plugin.toml", root)
	}
	seen := map[string]struct{}{}
	var paths []string
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read plugin search dir %s: %w", root, err)
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name(), "plugin.toml")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			paths = append(paths, path)
			seen[path] = struct{}{}
		}
	}
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) == "plugin.toml" {
			if _, ok := seen[path]; ok {
				return nil
			}
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk plugin search dir %s: %w", root, err)
	}
	sort.Strings(paths)
	return paths, nil
}

func toolNames(tools []manifest.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func loadPluginConfig(home, tenantID, pluginID string) (PluginConfig, error) {
	if strings.TrimSpace(pluginID) == "" {
		return PluginConfig{}, fmt.Errorf("plugin id is required")
	}
	if err := tenant.ValidateID(tenantID); err != nil {
		return PluginConfig{}, err
	}
	sources := []string{
		filepath.Join(home, "config", "global.toml"),
		filepath.Join(home, "config", "plugins", pluginID, "config.toml"),
		filepath.Join(home, "tenants", tenantID, "config", "plugins", pluginID, "defaults.toml"),
		filepath.Join(home, "tenants", tenantID, "config", "plugins", pluginID, "config.toml"),
		filepath.Join(home, "tenants", tenantID, "config", "plugins", pluginID, "overrides.toml"),
	}
	merged := document{}
	items := make([]PluginConfigSrc, 0, len(sources))
	for i, path := range sources {
		exists := fileExists(path)
		items = append(items, PluginConfigSrc{Path: path, Exists: exists})
		if !exists {
			continue
		}
		doc, err := loadTOMLDocument(path)
		if err != nil {
			return PluginConfig{}, err
		}
		if i == 0 {
			doc = pluginSubdocument(doc, pluginID)
		}
		merged = mergeDocuments(merged, doc)
	}
	return PluginConfig{ID: pluginID, Tenant: tenantID, Sources: items, Config: redactDocument(merged)}, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func loadTOMLDocument(path string) (document, error) {
	var doc map[string]any
	if _, err := toml.DecodeFile(path, &doc); err != nil {
		return nil, fmt.Errorf("load config %s: %w", path, err)
	}
	if doc == nil {
		return document{}, nil
	}
	return document(doc), nil
}

func pluginSubdocument(doc document, pluginID string) document {
	plugins, ok := documentValue(doc["plugins"])
	if !ok {
		return document{}
	}
	plugin, ok := documentValue(plugins[pluginID])
	if !ok {
		return document{}
	}
	return plugin
}

func mergeDocuments(base, overlay document) document {
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

func documentValue(value any) (document, bool) {
	switch typed := value.(type) {
	case document:
		return typed, true
	case map[string]any:
		return document(typed), true
	case map[string]map[string]any:
		out := document{}
		for key, item := range typed {
			out[key] = document(item)
		}
		return out, true
	default:
		return nil, false
	}
}

func cloneDocument(in map[string]any) document {
	out := document{}
	for key, value := range in {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case document:
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

func redactDocument(in map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range in {
		if isSecretKey(key) {
			out[key] = "<redacted>"
			continue
		}
		out[key] = redactValue(value)
	}
	return out
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case document:
		return redactDocument(typed)
	case map[string]any:
		return redactDocument(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = redactValue(item)
		}
		return out
	default:
		return typed
	}
}

func isSecretKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	secretKeys := []string{"secret", "token", "password", "passphrase", "api_key", "apikey", "access_key", "private_key", "client_secret"}
	for _, marker := range secretKeys {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func applyEnabledFromConfig(plugins []PluginReport, pluginID string, cfg map[string]any) {
	enabled := true
	if raw, ok := cfg["enabled"].(bool); ok {
		enabled = raw
	}
	for i := range plugins {
		if plugins[i].ID == pluginID {
			plugins[i].Enabled = enabled
		}
	}
}

func manifestHasTool(tools []manifest.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func invokeHealthTool(ctx context.Context, home, tenantID string, plugin manifest.Plugin) PluginHealth {
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	worker, err := bare.New().Spawn(callCtx, policy.SpawnReq{
		KernelID:    "inspect",
		TenantID:    tenantID,
		TargetID:    plugin.ID,
		TargetKind:  runtimewire.TargetKindPlugin,
		HarnessKind: plugin.Capability().HarnessKind,
		Command:     plugin.LaunchCommand(),
		Runtime:     inspectPluginRuntime(plugin),
		Entry:       plugin.Entry,
		Manifest:    plugin.RawJSON(),
		Env:         map[string]string{"TABULA_HOME": home, "TABULA_TENANT_DIR": filepath.Join(home, "tenants", tenantID)},
		WorkingDir:  plugin.RootDir,
		Mode:        policy.SpawnModeCold,
	})
	if err != nil {
		return PluginHealth{PluginID: plugin.ID, Status: "error", Messages: []HealthMessage{{Level: "error", Text: err.Error()}}}
	}
	defer func() {
		_ = worker.Shutdown(context.Background())
		_, _ = worker.Wait()
	}()
	if _, err := worker.Init(callCtx, workerwire.WorkerInit{Op: workerwire.OpInit, KernelID: "inspect", TenantID: tenantID, TargetID: plugin.ID, Manifest: plugin.RawJSON()}); err != nil {
		return PluginHealth{PluginID: plugin.ID, Status: "error", Messages: []HealthMessage{{Level: "error", Text: err.Error()}}}
	}
	result, err := worker.Call(callCtx, workerwire.WorkerCall{Op: workerwire.OpCall, CallID: "health-" + plugin.ID, Tool: healthToolName, Args: json.RawMessage(`{}`)})
	if err != nil {
		return PluginHealth{PluginID: plugin.ID, Status: "error", Messages: []HealthMessage{{Level: "error", Text: err.Error()}}}
	}
	if !result.OK {
		message := "health tool failed"
		if result.Error != nil && result.Error.Message != "" {
			message = result.Error.Message
		}
		return PluginHealth{PluginID: plugin.ID, Status: "error", Messages: []HealthMessage{{Level: "error", Text: message}}}
	}
	parsed, err := parseHealthPayload(plugin.ID, result.Data)
	if err != nil {
		return PluginHealth{PluginID: plugin.ID, Status: "error", Messages: []HealthMessage{{Level: "error", Text: err.Error()}}}
	}
	return parsed
}

func inspectPluginRuntime(plugin manifest.Plugin) string {
	if runtime := strings.TrimSpace(plugin.Runtime); runtime != "" {
		return runtime
	}
	if kind := plugin.Capability().HarnessKind; kind != runtimewire.HarnessKindUnknown {
		return string(kind)
	}
	return ""
}

func parseHealthPayload(pluginID string, data []byte) (PluginHealth, error) {
	var raw struct {
		Status   string          `json:"status"`
		Messages []HealthMessage `json:"messages"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return PluginHealth{}, fmt.Errorf("decode health payload: %w", err)
	}
	status := strings.ToLower(strings.TrimSpace(raw.Status))
	switch status {
	case "ok", "warn", "error", "skipped":
	default:
		return PluginHealth{}, fmt.Errorf("health payload has unsupported status %q", raw.Status)
	}
	messages := raw.Messages
	if len(messages) == 0 {
		messages = []HealthMessage{{Level: "info", Text: "health tool returned no messages"}}
	}
	for i := range messages {
		messages[i].Level = strings.ToLower(strings.TrimSpace(messages[i].Level))
		if messages[i].Level == "" {
			messages[i].Level = "info"
		}
		messages[i].Text = strings.TrimSpace(messages[i].Text)
	}
	return PluginHealth{PluginID: pluginID, Status: status, Messages: messages}, nil
}

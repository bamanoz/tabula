// Package manifest discovers plugin and skill manifests for tabula-runtime.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"

	"github.com/bamanoz/tabula/internal/runtime/wire"
)

var pluginIDPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)
var executionGroupPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)
var semverPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$`)

// Tool mirrors an advisory [[tools]] entry from plugin.toml.
type Tool struct {
	Name                string               `toml:"name" json:"name"`
	Description         string               `toml:"description" json:"description,omitempty"`
	Schema              json.RawMessage      `json:"schema,omitempty"`
	DeadlineMS          int                  `toml:"deadline_ms" json:"deadline_ms,omitempty"`
	Concurrency         wire.ToolConcurrency `toml:"concurrency" json:"concurrency,omitempty"`
	ExecutionGroup      string               `toml:"execution_group" json:"execution_group,omitempty"`
	ConflictsWithGroups []string             `toml:"conflicts_with_groups" json:"conflicts_with_groups,omitempty"`
}

// Worker mirrors a canonical [worker] block from plugin.toml.
type Worker struct {
	Command []string         `json:"command,omitempty"`
	Mode    wire.WorkerMode  `json:"mode,omitempty"`
	Scope   wire.WorkerScope `json:"scope,omitempty"`
}

type toolTOML struct {
	Name                string               `toml:"name"`
	Description         string               `toml:"description"`
	SchemaJSON          string               `toml:"schema_json"`
	DeadlineMS          int                  `toml:"deadline_ms"`
	Concurrency         wire.ToolConcurrency `toml:"concurrency"`
	ExecutionGroup      string               `toml:"execution_group"`
	ConflictsWithGroups []string             `toml:"conflicts_with_groups"`
}

// Plugin is the runtime daemon's normalized view of one plugin.toml.
type Plugin struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Version     string           `json:"version"`
	Runtime     string           `json:"runtime"`
	Entry       string           `json:"entry"`
	Description string           `json:"description,omitempty"`
	Kind        *Kind            `json:"kind,omitempty"`
	Worker      *Worker          `json:"worker,omitempty"`
	WorkerMode  wire.WorkerMode  `json:"worker_mode,omitempty"`
	WorkerScope wire.WorkerScope `json:"worker_scope,omitempty"`
	Tools       []Tool           `json:"tools,omitempty"`
	Hooks       []Hook           `json:"hooks,omitempty"`
	Requires    *Requires        `json:"requires,omitempty"`
	RootDir     string           `json:"-"`
}

// Kind classifies a plugin for runtime-side composition rules. It is distinct
// from wire target kind, which remains the protocol namespace (plugin/skill).
type Kind struct {
	Name      string `toml:"name" json:"name"`
	Singleton bool   `toml:"singleton" json:"singleton,omitempty"`
}

// Hook mirrors an advisory [[hooks]] entry from plugin.toml. Runtime does not
// route hooks in M2, but preserving this field in the normalized manifest keeps
// the runtime-side parser aligned with current plugin authoring schema.
type Hook struct {
	Event               string               `toml:"event" json:"event"`
	Priority            int                  `toml:"priority" json:"priority,omitempty"`
	TimeoutMS           *int64               `toml:"timeout_ms" json:"timeout_ms,omitempty"`
	Concurrency         wire.ToolConcurrency `toml:"concurrency" json:"concurrency,omitempty"`
	ExecutionGroup      string               `toml:"execution_group" json:"execution_group,omitempty"`
	ConflictsWithGroups []string             `toml:"conflicts_with_groups" json:"conflicts_with_groups,omitempty"`
}

// Requires mirrors the current plugin compatibility block. The runtime does not
// enforce SemVer ranges in M2, but it validates that migrated plugin manifests
// keep the same schema invariants as the existing kernel parser.
type Requires struct {
	Kernel          string `json:"kernel"`
	ProtocolVersion any    `json:"protocol_version"`
	SDK             string `json:"sdk"`
}

type pluginTOML struct {
	ID          string          `toml:"id"`
	Name        string          `toml:"name"`
	Version     string          `toml:"version"`
	Runtime     string          `toml:"runtime"`
	Entry       string          `toml:"entry"`
	Description string          `toml:"description"`
	Kind        *Kind           `toml:"kind"`
	Worker      *workerTOML     `toml:"worker"`
	WorkerMode  wire.WorkerMode `toml:"worker_mode"`
	Tools       []toolTOML      `toml:"tools"`
	Hooks       []Hook          `toml:"hooks"`
	Requires    *requiresTOML   `toml:"requires"`
}

type workerTOML struct {
	Command []string         `toml:"command"`
	Mode    wire.WorkerMode  `toml:"mode"`
	Scope   wire.WorkerScope `toml:"scope"`
}

type requiresTOML struct {
	Kernel          string `toml:"kernel"`
	ProtocolVersion any    `toml:"protocol_version"`
	SDK             string `toml:"sdk"`
}

// Index is an immutable target_id -> manifest map.
type Index struct {
	plugins map[string]Plugin
	skills  map[string]Skill
}

// Empty returns an index with no available targets.
func Empty() *Index { return &Index{plugins: map[string]Plugin{}, skills: map[string]Skill{}} }

// LoadDirs reads every plugin.toml below dirs. Missing search dirs are ignored
// so a runtime can start before any plugins have been installed.
func LoadDirs(dirs []string) (*Index, error) {
	return LoadSearchDirs(dirs, inferSkillSearchDirs(dirs))
}

// LoadSearchDirs reads plugin and skill manifests from explicit search roots.
func LoadSearchDirs(pluginDirs []string, skillDirs []string) (*Index, error) {
	plugins := map[string]Plugin{}
	skills := map[string]Skill{}
	for _, dir := range pluginDirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		paths, err := manifestPaths(dir)
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			plugin, err := Load(path)
			if err != nil {
				return nil, err
			}
			if existing, ok := plugins[plugin.ID]; ok {
				return nil, fmt.Errorf("plugin manifest %s duplicates target id %q from %s", path, plugin.ID, filepath.Join(existing.RootDir, "plugin.toml"))
			}
			plugins[plugin.ID] = plugin
		}
	}
	if err := validatePluginKindSingletons(plugins); err != nil {
		return nil, err
	}
	for _, dir := range skillDirs {
		paths, err := skillManifestPaths(dir)
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			skill, err := LoadSkill(path)
			if err != nil {
				slog.Warn("skip malformed skill manifest", "path", path, "err", err)
				continue
			}
			if existing, ok := skills[skill.TargetID()]; ok {
				return nil, fmt.Errorf("skill manifest %s duplicates target id %q from %s", path, skill.TargetID(), filepath.Join(existing.RootDir, "SKILL.md"))
			}
			skills[skill.TargetID()] = skill
		}
	}
	return &Index{plugins: plugins, skills: skills}, nil
}

func validatePluginKindSingletons(plugins map[string]Plugin) error {
	byKind := map[string][]string{}
	singletonKinds := map[string]struct{}{}
	for id, plugin := range plugins {
		if plugin.Kind == nil || plugin.Kind.Name == "" {
			continue
		}
		byKind[plugin.Kind.Name] = append(byKind[plugin.Kind.Name], id)
		if plugin.Kind.Singleton {
			singletonKinds[plugin.Kind.Name] = struct{}{}
		}
	}
	for kind := range singletonKinds {
		ids := byKind[kind]
		if len(ids) <= 1 {
			continue
		}
		sort.Strings(ids)
		return fmt.Errorf("plugin kind %q is singleton but multiple plugins declare it: %s", kind, strings.Join(ids, ", "))
	}
	return nil
}

func inferSkillSearchDirs(dirs []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, raw := range dirs {
		if explicitSkillSearchDir(raw) {
			candidate, err := filepath.Abs(strings.TrimSpace(raw))
			if err != nil {
				continue
			}
			candidate = filepath.Clean(candidate)
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			result = append(result, candidate)
			continue
		}
		candidate := inferSkillSearchDir(raw)
		if candidate == "" {
			continue
		}
		candidate = filepath.Clean(candidate)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		result = append(result, candidate)
	}
	sort.Strings(result)
	return result
}

func explicitSkillSearchDir(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if filepath.Base(raw) == "SKILL.md" {
		return true
	}
	info, err := os.Stat(raw)
	if err != nil {
		return false
	}
	return info.IsDir() && filepath.Base(raw) == "skills"
}

func inferSkillSearchDir(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(abs), "/")
	for i, part := range parts {
		if part == "plugins" {
			parts[i] = "skills"
			return filepath.FromSlash(strings.Join(parts[:i+1], "/"))
		}
	}
	info, err := os.Stat(abs)
	if err == nil && info.IsDir() {
		return filepath.Join(abs, "skills")
	}
	return ""
}

func manifestPaths(root string) ([]string, error) {
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

// Load reads and validates one plugin.toml file.
func Load(path string) (Plugin, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Plugin{}, fmt.Errorf("plugin manifest path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Plugin{}, fmt.Errorf("resolve plugin manifest %s: %w", path, err)
	}
	var raw pluginTOML
	if _, err := toml.DecodeFile(abs, &raw); err != nil {
		return Plugin{}, fmt.Errorf("load plugin manifest %s: %w", abs, err)
	}
	workerMode := raw.WorkerMode
	if raw.Worker != nil && raw.Worker.Mode != "" {
		if workerMode != "" && workerMode != raw.Worker.Mode {
			return Plugin{}, fmt.Errorf("plugin manifest %s: worker.mode %q conflicts with worker_mode %q", abs, raw.Worker.Mode, workerMode)
		}
		workerMode = raw.Worker.Mode
	}
	plugin := Plugin{
		ID:          strings.TrimSpace(raw.ID),
		Name:        strings.TrimSpace(raw.Name),
		Version:     strings.TrimSpace(raw.Version),
		Runtime:     strings.TrimSpace(raw.Runtime),
		Entry:       strings.TrimSpace(raw.Entry),
		Description: strings.TrimSpace(raw.Description),
		Kind:        normalizeKind(raw.Kind),
		WorkerMode:  workerMode,
		WorkerScope: wire.WorkerScopeTenant,
		Tools:       make([]Tool, 0, len(raw.Tools)),
		Hooks:       make([]Hook, 0, len(raw.Hooks)),
		RootDir:     filepath.Dir(abs),
	}
	if plugin.WorkerMode == "" {
		plugin.WorkerMode = wire.WorkerModeWarm
	}
	if raw.Worker != nil && raw.Worker.Scope != "" {
		plugin.WorkerScope = raw.Worker.Scope
	}
	if raw.Worker != nil {
		plugin.Worker = &Worker{Command: trimNonemptyStrings(raw.Worker.Command), Mode: plugin.WorkerMode, Scope: plugin.WorkerScope}
	}
	for i, tool := range raw.Tools {
		parsed, err := parseTool(tool)
		if err != nil {
			return Plugin{}, fmt.Errorf("plugin manifest %s: tools[%d]: %w", abs, i, err)
		}
		plugin.Tools = append(plugin.Tools, normalizeTool(parsed))
	}
	for _, hook := range raw.Hooks {
		plugin.Hooks = append(plugin.Hooks, normalizeHook(hook))
	}
	if raw.Requires != nil {
		plugin.Requires = &Requires{Kernel: strings.TrimSpace(raw.Requires.Kernel), ProtocolVersion: raw.Requires.ProtocolVersion, SDK: strings.TrimSpace(raw.Requires.SDK)}
	}
	if err := plugin.Validate(); err != nil {
		return Plugin{}, fmt.Errorf("plugin manifest %s: %w", abs, err)
	}
	return plugin, nil
}

func normalizeKind(kind *Kind) *Kind {
	if kind == nil {
		return nil
	}
	return &Kind{Name: strings.TrimSpace(kind.Name), Singleton: kind.Singleton}
}

// Validate checks runtime-owned invariants before a worker can be spawned.
func (p Plugin) Validate() error {
	if !pluginIDPattern.MatchString(p.ID) {
		return fmt.Errorf("invalid id %q", p.ID)
	}
	if p.Name == "" {
		return fmt.Errorf("name is required")
	}
	if p.Version == "" {
		return fmt.Errorf("version is required")
	}
	if !semverPattern.MatchString(p.Version) {
		return fmt.Errorf("version %q must be SemVer X.Y.Z[-pre]", p.Version)
	}
	switch p.WorkerMode {
	case wire.WorkerModeWarm, wire.WorkerModeCold:
	default:
		return fmt.Errorf("worker_mode %q must be warm or cold", p.WorkerMode)
	}
	switch p.WorkerScope {
	case "", wire.WorkerScopeTenant, wire.WorkerScopeRuntime:
	default:
		return fmt.Errorf("worker.scope %q must be tenant or runtime", p.WorkerScope)
	}
	if p.WorkerMode == wire.WorkerModeCold && p.WorkerScope == wire.WorkerScopeRuntime {
		return fmt.Errorf("worker.scope runtime requires worker_mode warm")
	}
	if p.Kind != nil && p.Kind.Name == "" {
		return fmt.Errorf("kind.name is required")
	}
	hasWorkerCommand := p.hasWorkerCommand()
	if p.Worker != nil {
		if err := validateWorkerCommand(p.Worker.Command); err != nil {
			return err
		}
	}
	if hasWorkerCommand {
		if p.Runtime != "" || p.Entry != "" {
			switch p.Runtime {
			case "python":
			default:
				return fmt.Errorf("unsupported runtime %q (expected python)", p.Runtime)
			}
			if p.Entry == "" {
				return fmt.Errorf("entry is required")
			}
			if err := validateRelativePath("entry", p.Entry); err != nil {
				return err
			}
		}
	} else {
		switch p.Runtime {
		case "python":
		default:
			return fmt.Errorf("unsupported runtime %q (expected python)", p.Runtime)
		}
		if p.Entry == "" {
			return fmt.Errorf("entry is required")
		}
		if err := validateRelativePath("entry", p.Entry); err != nil {
			return err
		}
	}
	for i, tool := range p.Tools {
		if tool.Name == "" {
			return fmt.Errorf("tools[%d].name is required", i)
		}
		if tool.DeadlineMS < 0 {
			return fmt.Errorf("tools[%d].deadline_ms must be >= 0", i)
		}
		switch tool.Concurrency {
		case wire.ToolConcurrencySerial, wire.ToolConcurrencyParallel:
		default:
			return fmt.Errorf("tools[%d].concurrency %q must be serial or parallel", i, tool.Concurrency)
		}
		if !executionGroupPattern.MatchString(tool.ExecutionGroup) {
			return fmt.Errorf("tools[%d].execution_group %q is invalid", i, tool.ExecutionGroup)
		}
		seen := map[string]struct{}{}
		for j, group := range tool.ConflictsWithGroups {
			if !executionGroupPattern.MatchString(group) {
				return fmt.Errorf("tools[%d].conflicts_with_groups[%d] %q is invalid", i, j, group)
			}
			if _, dup := seen[group]; dup {
				return fmt.Errorf("tools[%d].conflicts_with_groups has duplicate %q", i, group)
			}
			seen[group] = struct{}{}
		}
	}
	for i, hook := range p.Hooks {
		if hook.Event == "" {
			return fmt.Errorf("hooks[%d].event is required", i)
		}
		if hook.TimeoutMS != nil && *hook.TimeoutMS < 0 {
			return fmt.Errorf("hooks[%d].timeout_ms must be >= 0", i)
		}
		switch hook.Concurrency {
		case wire.ToolConcurrencySerial, wire.ToolConcurrencyParallel:
		default:
			return fmt.Errorf("hooks[%d].concurrency %q must be serial or parallel", i, hook.Concurrency)
		}
		if !executionGroupPattern.MatchString(hook.ExecutionGroup) {
			return fmt.Errorf("hooks[%d].execution_group %q is invalid", i, hook.ExecutionGroup)
		}
		seen := map[string]struct{}{}
		for j, group := range hook.ConflictsWithGroups {
			if !executionGroupPattern.MatchString(group) {
				return fmt.Errorf("hooks[%d].conflicts_with_groups[%d] %q is invalid", i, j, group)
			}
			if _, dup := seen[group]; dup {
				return fmt.Errorf("hooks[%d].conflicts_with_groups has duplicate %q", i, group)
			}
			seen[group] = struct{}{}
		}
	}
	if p.WorkerMode == wire.WorkerModeCold && len(p.Hooks) > 0 {
		return fmt.Errorf("worker_mode cold does not support hooks")
	}
	if err := validateRequires(p.Requires, !hasWorkerCommand); err != nil {
		return err
	}
	return nil
}

func trimNonemptyStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = strings.TrimSpace(item)
	}
	return out
}

func validateWorkerCommand(command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("worker.command must be a non-empty argv list")
	}
	for i, arg := range command {
		if arg == "" {
			return fmt.Errorf("worker.command[%d] must be a non-empty string", i)
		}
		if i == 0 && strings.ContainsAny(arg, `/\\`) && !filepath.IsAbs(arg) {
			if err := validateRelativePath("worker.command[0]", arg); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateRelativePath(field, value string) error {
	if filepath.IsAbs(value) {
		return fmt.Errorf("%s must be relative", field)
	}
	clean := filepath.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s must not contain '..'", field)
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' })
	for _, part := range parts {
		if part == ".." {
			return fmt.Errorf("%s must not contain '..'", field)
		}
	}
	return nil
}

func validateRequires(r *Requires, sdkRequired bool) error {
	if r == nil {
		return fmt.Errorf("[requires] block is required (declare kernel, protocol_version%s per docs/PROTOCOL.md §3)", requiresSDKSuffix(sdkRequired))
	}
	if strings.TrimSpace(r.Kernel) == "" {
		return fmt.Errorf("requires.kernel is required")
	}
	if err := validateProtocolVersion(r.ProtocolVersion); err != nil {
		return err
	}
	if strings.TrimSpace(r.SDK) == "" {
		if !sdkRequired {
			return nil
		}
		return fmt.Errorf("requires.sdk is required")
	}
	if err := validateSDKRequirement(r.SDK); err != nil {
		return err
	}
	return nil
}

func requiresSDKSuffix(required bool) string {
	if required {
		return ", sdk"
	}
	return ""
}

func validateProtocolVersion(value any) error {
	if value == nil {
		return fmt.Errorf("requires.protocol_version is required")
	}
	if n, ok := protocolVersionInt(value); ok {
		if n < 1 {
			return fmt.Errorf("requires.protocol_version must be a positive integer")
		}
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return fmt.Errorf("requires.protocol_version must be a positive integer or an array of positive integers")
	}
	if len(items) == 0 {
		return fmt.Errorf("requires.protocol_version must list at least one version")
	}
	seen := map[int]struct{}{}
	for i, item := range items {
		n, ok := protocolVersionInt(item)
		if !ok || n < 1 {
			return fmt.Errorf("requires.protocol_version[%d] must be a positive integer", i)
		}
		if _, dup := seen[n]; dup {
			return fmt.Errorf("requires.protocol_version has duplicate %d", n)
		}
		seen[n] = struct{}{}
	}
	return nil
}

func protocolVersionInt(value any) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

func validateSDKRequirement(text string) error {
	idx := -1
	for i, r := range text {
		if r == '>' || r == '<' || r == '=' {
			idx = i
			break
		}
	}
	if idx <= 0 || idx == len(text)-1 {
		return fmt.Errorf("requires.sdk %q: expected '<name><range>' (e.g. tabula-plugin-sdk>=0.1.0)", text)
	}
	if strings.TrimSpace(text[:idx]) == "" || strings.TrimSpace(text[idx:]) == "" {
		return fmt.Errorf("requires.sdk %q: expected '<name><range>' (e.g. tabula-plugin-sdk>=0.1.0)", text)
	}
	if err := validateConstraint(text[idx:]); err != nil {
		return fmt.Errorf("requires.sdk range: %w", err)
	}
	return nil
}

func validateConstraint(text string) error {
	parts := strings.Split(strings.TrimSpace(text), ",")
	if len(parts) == 0 {
		return fmt.Errorf("empty constraint")
	}
	seen := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		seen = true
		for _, op := range []string{"<=", ">=", "==", "<", ">", "="} {
			if strings.HasPrefix(part, op) {
				version := strings.TrimSpace(part[len(op):])
				if !semverPattern.MatchString(version) {
					return fmt.Errorf("not a valid version: %q", version)
				}
				goto next
			}
		}
		if !semverPattern.MatchString(part) {
			return fmt.Errorf("not a valid version: %q", part)
		}
	next:
	}
	if !seen {
		return fmt.Errorf("empty constraint")
	}
	return nil
}

// RawJSON returns the manifest subset sent in WorkerInit.
func (p Plugin) RawJSON() json.RawMessage {
	data, _ := json.Marshal(p)
	return data
}

// LaunchCommand returns the argv used to spawn the plugin worker.
func (p Plugin) LaunchCommand() []string {
	if p.hasWorkerCommand() {
		return append([]string(nil), p.Worker.Command...)
	}
	return nil
}

// LaunchPath returns the most relevant plugin-owned launch path for diagnostics.
func (p Plugin) LaunchPath() string {
	if p.hasWorkerCommand() {
		if len(p.Worker.Command) > 1 {
			kind := detectHarnessKindFromCommand(p.Worker.Command)
			candidate := strings.TrimSpace(p.Worker.Command[1])
			if kind != wire.HarnessKindUnknown && candidate != "" && !strings.HasPrefix(candidate, "-") {
				if filepath.IsAbs(candidate) || p.RootDir == "" {
					return candidate
				}
				return filepath.Join(p.RootDir, candidate)
			}
		}
		argv0 := p.Worker.Command[0]
		if filepath.IsAbs(argv0) || p.RootDir == "" || !strings.ContainsAny(argv0, `/\\`) {
			return argv0
		}
		return filepath.Join(p.RootDir, argv0)
	}
	if filepath.IsAbs(p.Entry) || p.RootDir == "" {
		return p.Entry
	}
	return filepath.Join(p.RootDir, p.Entry)
}

func (p Plugin) hasWorkerCommand() bool {
	return p.Worker != nil && len(p.Worker.Command) > 0
}

// Capability returns the Runtime API capability view for this plugin.
func (p Plugin) Capability() wire.Capability {
	tools := make([]wire.ToolSpec, 0, len(p.Tools))
	for _, tool := range p.Tools {
		tools = append(tools, wire.ToolSpec{
			Name:                tool.Name,
			Description:         tool.Description,
			Schema:              append(json.RawMessage(nil), tool.Schema...),
			DeadlineMS:          int64(tool.DeadlineMS),
			Concurrency:         tool.Concurrency,
			ExecutionGroup:      tool.ExecutionGroup,
			ConflictsWithGroups: append([]string(nil), tool.ConflictsWithGroups...),
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	hooks := make([]wire.HookSpec, 0, len(p.Hooks))
	for _, hook := range p.Hooks {
		hooks = append(hooks, wire.HookSpec{
			Event:               hook.Event,
			Priority:            hook.Priority,
			TimeoutMS:           hook.TimeoutMS,
			Concurrency:         hook.Concurrency,
			ExecutionGroup:      hook.ExecutionGroup,
			ConflictsWithGroups: append([]string(nil), hook.ConflictsWithGroups...),
		})
	}
	sort.Slice(hooks, func(i, j int) bool {
		if hooks[i].Event == hooks[j].Event {
			return hooks[i].Priority < hooks[j].Priority
		}
		return hooks[i].Event < hooks[j].Event
	})
	return wire.Capability{
		Target:      wire.Target{Kind: wire.TargetKindPlugin, ID: p.ID},
		Tools:       tools,
		Hooks:       hooks,
		Revision:    1,
		State:       wire.CapabilityStateManifestLoaded,
		Source:      wire.CapabilitySourceManifest,
		WorkerMode:  p.WorkerMode,
		WorkerScope: p.WorkerScope,
		HarnessKind: detectPluginHarnessKind(p.Runtime, p.LaunchCommand()),
	}
}

func detectPluginHarnessKind(runtime string, command []string) wire.HarnessKind {
	if kind := detectHarnessKindFromCommand(command); kind != wire.HarnessKindUnknown {
		return kind
	}
	switch strings.TrimSpace(runtime) {
	case "python":
		return wire.HarnessKindPython
	default:
		return wire.HarnessKindUnknown
	}
}

func detectHarnessKindFromCommand(command []string) wire.HarnessKind {
	if len(command) == 0 {
		return wire.HarnessKindUnknown
	}
	base := strings.ToLower(filepath.Base(strings.TrimSpace(command[0])))
	switch base {
	case "python", "python3":
		return wire.HarnessKindPython
	case "bash", "sh", "zsh", "dash":
		return wire.HarnessKindBash
	case "node", "nodejs":
		return wire.HarnessKindNode
	default:
		return wire.HarnessKindUnknown
	}
}

func normalizeTool(tool Tool) Tool {
	normalized := Tool{
		Name:           strings.TrimSpace(tool.Name),
		Description:    strings.TrimSpace(tool.Description),
		Schema:         append(json.RawMessage(nil), tool.Schema...),
		DeadlineMS:     tool.DeadlineMS,
		Concurrency:    tool.Concurrency,
		ExecutionGroup: strings.TrimSpace(tool.ExecutionGroup),
	}
	if normalized.Concurrency == "" {
		normalized.Concurrency = wire.ToolConcurrencySerial
	}
	if normalized.ExecutionGroup == "" {
		normalized.ExecutionGroup = normalized.Name
	}
	groups := make([]string, 0, len(tool.ConflictsWithGroups))
	for _, group := range tool.ConflictsWithGroups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		groups = append(groups, group)
	}
	if len(groups) == 0 {
		groups = []string{normalized.ExecutionGroup}
	}
	normalized.ConflictsWithGroups = groups
	return normalized
}

func normalizeHook(hook Hook) Hook {
	normalized := Hook{
		Event:          strings.TrimSpace(hook.Event),
		Priority:       hook.Priority,
		TimeoutMS:      hook.TimeoutMS,
		Concurrency:    hook.Concurrency,
		ExecutionGroup: strings.TrimSpace(hook.ExecutionGroup),
	}
	if normalized.Concurrency == "" {
		normalized.Concurrency = wire.ToolConcurrencySerial
	}
	if normalized.ExecutionGroup == "" {
		normalized.ExecutionGroup = "hook-" + normalized.Event
	}
	groups := make([]string, 0, len(hook.ConflictsWithGroups))
	for _, group := range hook.ConflictsWithGroups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		groups = append(groups, group)
	}
	if len(groups) == 0 {
		groups = []string{normalized.ExecutionGroup}
	}
	normalized.ConflictsWithGroups = groups
	return normalized
}

func parseTool(raw toolTOML) (Tool, error) {
	tool := Tool{
		Name:                raw.Name,
		Description:         raw.Description,
		DeadlineMS:          raw.DeadlineMS,
		Concurrency:         raw.Concurrency,
		ExecutionGroup:      raw.ExecutionGroup,
		ConflictsWithGroups: append([]string(nil), raw.ConflictsWithGroups...),
	}
	if strings.TrimSpace(raw.SchemaJSON) == "" {
		return tool, nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(raw.SchemaJSON), &parsed); err != nil {
		return Tool{}, fmt.Errorf("schema_json must be valid JSON: %w", err)
	}
	if _, ok := parsed.(map[string]any); !ok {
		return Tool{}, fmt.Errorf("schema_json must decode to a JSON object")
	}
	tool.Schema = json.RawMessage(raw.SchemaJSON)
	return tool, nil
}

// Get returns the plugin for target id.
func (i *Index) Get(id string) (Plugin, bool) {
	if i == nil {
		return Plugin{}, false
	}
	plugin, ok := i.plugins[id]
	return plugin, ok
}

// GetSkill returns the skill for target id.
func (i *Index) GetSkill(id string) (Skill, bool) {
	if i == nil {
		return Skill{}, false
	}
	skill, ok := i.skills[id]
	return skill, ok
}

// Capabilities returns executable capabilities in stable target-id order.
// Skills are prompt artifacts only; executable capabilities are plugin-owned.
func (i *Index) Capabilities() []wire.Capability {
	if i == nil {
		return nil
	}
	ids := make([]string, 0, len(i.plugins))
	for id := range i.plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]wire.Capability, 0, len(ids))
	for _, id := range ids {
		if plugin, ok := i.plugins[id]; ok {
			out = append(out, plugin.Capability())
		}
	}
	return out
}

// PluginCapabilities returns plugin-only capabilities in stable target-id order.
func (i *Index) PluginCapabilities() []wire.Capability {
	if i == nil {
		return nil
	}
	ids := make([]string, 0, len(i.plugins))
	for id := range i.plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]wire.Capability, 0, len(ids))
	for _, id := range ids {
		out = append(out, i.plugins[id].Capability())
	}
	return out
}

// Store owns the reloadable manifest index.
type Store struct {
	dirs       []string
	pluginDirs []string
	skillDirs  []string
	tabulaHome string
	tenants    map[string]SearchDirs
	mu         sync.RWMutex
	index      *Index
	byTenant   map[string]*Index
}

// SearchDirs names the manifest search roots for one tenant/app catalog.
type SearchDirs struct {
	PluginDirs []string
	SkillDirs  []string
}

// NewStore loads an initial manifest store from dirs.
func NewStore(dirs []string) (*Store, error) {
	idx, err := LoadDirs(dirs)
	if err != nil {
		return nil, err
	}
	return &Store{dirs: append([]string(nil), dirs...), index: idx}, nil
}

// NewSearchStore loads an initial manifest store from explicit plugin and skill dirs.
func NewSearchStore(pluginDirs []string, skillDirs []string) (*Store, error) {
	idx, err := LoadSearchDirs(pluginDirs, skillDirs)
	if err != nil {
		return nil, err
	}
	dirs := append(append([]string{}, pluginDirs...), skillDirs...)
	return &Store{dirs: dirs, pluginDirs: append([]string(nil), pluginDirs...), skillDirs: append([]string(nil), skillDirs...), index: idx}, nil
}

// SetTabulaHome configures the runtime home used to discover tenant-local
// runtime surfaces during reloads of a runtime that started before an app was
// materialized.
func (s *Store) SetTabulaHome(home string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.tabulaHome = strings.TrimSpace(home)
	s.mu.Unlock()
}

// NewTenantStore loads separate manifest indexes for tenant/app catalogs.
func NewTenantStore(tenants map[string]SearchDirs) (*Store, error) {
	byTenant, err := loadTenantIndexes(tenants)
	if err != nil {
		return nil, err
	}
	return &Store{index: Empty(), tenants: cloneTenantSearchDirs(tenants), byTenant: byTenant}, nil
}

// Reload re-reads all configured search dirs.
func (s *Store) Reload() error {
	if s == nil {
		return nil
	}
	var idx *Index
	var byTenant map[string]*Index
	var err error
	if len(s.tenants) > 0 {
		byTenant, err = loadTenantIndexes(s.tenants)
	} else if s.pluginDirs != nil || s.skillDirs != nil {
		idx, err = LoadSearchDirs(s.pluginDirs, s.skillDirs)
	} else {
		idx, err = LoadDirs(s.dirs)
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	if len(s.tenants) > 0 {
		s.byTenant = byTenant
	} else {
		s.index = idx
	}
	s.mu.Unlock()
	return nil
}

// ReloadTenant re-reads one configured tenant/app catalog. It falls back to a
// full reload for non-tenant stores.
func (s *Store) ReloadTenant(tenantID string) error {
	if s == nil {
		return nil
	}
	if len(s.tenants) == 0 {
		return s.reloadDiscoveredTenant(tenantID)
	}
	dirs, ok := s.tenants[tenantID]
	if !ok {
		return s.reloadDiscoveredTenant(tenantID)
	}
	idx, err := LoadSearchDirs(dirs.PluginDirs, dirs.SkillDirs)
	if err != nil {
		return fmt.Errorf("load tenant %q manifests: %w", tenantID, err)
	}
	s.mu.Lock()
	s.byTenant[tenantID] = idx
	s.mu.Unlock()
	return nil
}

func (s *Store) reloadDiscoveredTenant(tenantID string) error {
	if err := wire.ValidateTenantID(tenantID); err != nil {
		return err
	}
	s.mu.RLock()
	home := s.tabulaHome
	s.mu.RUnlock()
	if home == "" {
		return s.Reload()
	}
	dirs := SearchDirs{
		PluginDirs: []string{filepath.Join(home, "tenants", tenantID, "plugins")},
		SkillDirs:  []string{filepath.Join(home, "tenants", tenantID, "skills")},
	}
	idx, err := LoadSearchDirs(dirs.PluginDirs, dirs.SkillDirs)
	if err != nil {
		return fmt.Errorf("load tenant %q manifests: %w", tenantID, err)
	}
	s.mu.Lock()
	if s.tenants == nil {
		s.tenants = map[string]SearchDirs{}
	}
	if s.byTenant == nil {
		s.byTenant = map[string]*Index{}
	}
	s.tenants[tenantID] = dirs
	s.byTenant[tenantID] = idx
	s.mu.Unlock()
	return nil
}

// Get returns the current plugin for target id.
func (s *Store) Get(id string) (Plugin, bool) {
	if s == nil {
		return Plugin{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.byTenant) > 0 {
		return Plugin{}, false
	}
	return s.index.Get(id)
}

// GetForTenant returns the current plugin for target id in a tenant/app catalog.
func (s *Store) GetForTenant(tenantID, id string) (Plugin, bool) {
	if s == nil {
		return Plugin{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if idx := s.indexForTenantLocked(tenantID); idx != nil {
		return idx.Get(id)
	}
	return Plugin{}, false
}

// GetSkill returns the current skill for target id.
func (s *Store) GetSkill(id string) (Skill, bool) {
	if s == nil {
		return Skill{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index.GetSkill(id)
}

// Capabilities returns the current stable capability list.
func (s *Store) Capabilities() []wire.Capability {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.byTenant) > 0 {
		var out []wire.Capability
		for _, tenantID := range sortedIndexKeys(s.byTenant) {
			for _, capability := range s.byTenant[tenantID].Capabilities() {
				capability.Tenants = []string{tenantID}
				out = append(out, capability)
			}
		}
		return out
	}
	return s.index.Capabilities()
}

// CapabilitiesForTenant returns executable capabilities for one tenant/app catalog.
func (s *Store) CapabilitiesForTenant(tenantID string) []wire.Capability {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	idx := s.indexForTenantLocked(tenantID)
	if idx == nil {
		return nil
	}
	out := idx.Capabilities()
	for i := range out {
		out[i].Tenants = []string{tenantID}
	}
	return out
}

// PluginsForTenant returns executable plugin manifests for one tenant/app catalog.
func (s *Store) PluginsForTenant(tenantID string) []Plugin {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	idx := s.indexForTenantLocked(tenantID)
	if idx == nil {
		return nil
	}
	ids := sortedPluginKeys(idx.plugins)
	out := make([]Plugin, 0, len(ids))
	for _, id := range ids {
		out = append(out, idx.plugins[id])
	}
	return out
}

func sortedPluginKeys(plugins map[string]Plugin) []string {
	keys := make([]string, 0, len(plugins))
	for key := range plugins {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// TenantIDs returns the configured tenant/app catalog ids.
func (s *Store) TenantIDs() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.byTenant))
	for tenantID := range s.byTenant {
		out = append(out, tenantID)
	}
	sort.Strings(out)
	return out
}

// PluginCapabilities returns plugin-only capability metadata for the worker pool.
func (s *Store) PluginCapabilities() []wire.Capability {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.byTenant) > 0 {
		var out []wire.Capability
		for _, tenantID := range sortedIndexKeys(s.byTenant) {
			for _, capability := range s.byTenant[tenantID].PluginCapabilities() {
				capability.Tenants = []string{tenantID}
				out = append(out, capability)
			}
		}
		return out
	}
	return s.index.PluginCapabilities()
}

func sortedIndexKeys(indexes map[string]*Index) []string {
	keys := make([]string, 0, len(indexes))
	for key := range indexes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *Store) indexForTenantLocked(tenantID string) *Index {
	if len(s.byTenant) == 0 {
		return s.index
	}
	return s.byTenant[tenantID]
}

func loadTenantIndexes(tenants map[string]SearchDirs) (map[string]*Index, error) {
	byTenant := make(map[string]*Index, len(tenants))
	for tenantID, dirs := range tenants {
		idx, err := LoadSearchDirs(dirs.PluginDirs, dirs.SkillDirs)
		if err != nil {
			return nil, fmt.Errorf("load tenant %q manifests: %w", tenantID, err)
		}
		byTenant[tenantID] = idx
	}
	if err := validateTenantPluginKindSingletons(byTenant); err != nil {
		return nil, err
	}
	return byTenant, nil
}

func validateTenantPluginKindSingletons(byTenant map[string]*Index) error {
	byKind := map[string]map[string]struct{}{}
	singletonKinds := map[string]struct{}{}
	for _, idx := range byTenant {
		if idx == nil {
			continue
		}
		for id, plugin := range idx.plugins {
			if plugin.Kind == nil || plugin.Kind.Name == "" {
				continue
			}
			if byKind[plugin.Kind.Name] == nil {
				byKind[plugin.Kind.Name] = map[string]struct{}{}
			}
			byKind[plugin.Kind.Name][id] = struct{}{}
			if plugin.Kind.Singleton {
				singletonKinds[plugin.Kind.Name] = struct{}{}
			}
		}
	}
	for kind := range singletonKinds {
		if len(byKind[kind]) <= 1 {
			continue
		}
		ids := make([]string, 0, len(byKind[kind]))
		for id := range byKind[kind] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return fmt.Errorf("plugin kind %q is singleton but multiple plugin ids declare it across tenant catalogs: %s", kind, strings.Join(ids, ", "))
	}
	return nil
}

func cloneTenantSearchDirs(in map[string]SearchDirs) map[string]SearchDirs {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]SearchDirs, len(in))
	for tenantID, dirs := range in {
		out[tenantID] = SearchDirs{PluginDirs: append([]string(nil), dirs.PluginDirs...), SkillDirs: append([]string(nil), dirs.SkillDirs...)}
	}
	return out
}

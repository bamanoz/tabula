// Package manifest discovers plugin worker manifests for tabula-runtime.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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
var semverPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$`)

// Tool mirrors an advisory [[tools]] entry from plugin.toml.
type Tool struct {
	Name        string `toml:"name" json:"name"`
	Description string `toml:"description" json:"description,omitempty"`
	DeadlineMS  int    `toml:"deadline_ms" json:"deadline_ms,omitempty"`
}

// Plugin is the runtime daemon's normalized view of one plugin.toml.
type Plugin struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Runtime     string    `json:"runtime"`
	Entry       string    `json:"entry"`
	Description string    `json:"description,omitempty"`
	Tools       []Tool    `json:"tools,omitempty"`
	Hooks       []Hook    `json:"hooks,omitempty"`
	Requires    *Requires `json:"requires,omitempty"`
	RootDir     string    `json:"-"`
}

// Hook mirrors an advisory [[hooks]] entry from plugin.toml. Runtime does not
// route hooks in M2, but preserving this field in the normalized manifest keeps
// the runtime-side parser aligned with current plugin authoring schema.
type Hook struct {
	Event    string `toml:"event" json:"event"`
	Priority int    `toml:"priority" json:"priority,omitempty"`
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
	ID          string        `toml:"id"`
	Name        string        `toml:"name"`
	Version     string        `toml:"version"`
	Runtime     string        `toml:"runtime"`
	Entry       string        `toml:"entry"`
	Description string        `toml:"description"`
	Tools       []Tool        `toml:"tools"`
	Hooks       []Hook        `toml:"hooks"`
	Requires    *requiresTOML `toml:"requires"`
}

type requiresTOML struct {
	Kernel          string `toml:"kernel"`
	ProtocolVersion any    `toml:"protocol_version"`
	SDK             string `toml:"sdk"`
}

// Index is an immutable target_id -> manifest map.
type Index struct {
	plugins map[string]Plugin
}

// Empty returns an index with no available plugin targets.
func Empty() *Index { return &Index{plugins: map[string]Plugin{}} }

// LoadDirs reads every plugin.toml below dirs. Missing search dirs are ignored
// so a runtime can start before any plugins have been installed.
func LoadDirs(dirs []string) (*Index, error) {
	plugins := map[string]Plugin{}
	for _, dir := range dirs {
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
	return &Index{plugins: plugins}, nil
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
	var paths []string
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) == "plugin.toml" {
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
	plugin := Plugin{
		ID:          strings.TrimSpace(raw.ID),
		Name:        strings.TrimSpace(raw.Name),
		Version:     strings.TrimSpace(raw.Version),
		Runtime:     strings.TrimSpace(raw.Runtime),
		Entry:       strings.TrimSpace(raw.Entry),
		Description: strings.TrimSpace(raw.Description),
		Tools:       make([]Tool, 0, len(raw.Tools)),
		Hooks:       make([]Hook, 0, len(raw.Hooks)),
		RootDir:     filepath.Dir(abs),
	}
	for _, tool := range raw.Tools {
		plugin.Tools = append(plugin.Tools, Tool{Name: strings.TrimSpace(tool.Name), Description: strings.TrimSpace(tool.Description), DeadlineMS: tool.DeadlineMS})
	}
	for _, hook := range raw.Hooks {
		plugin.Hooks = append(plugin.Hooks, Hook{Event: strings.TrimSpace(hook.Event), Priority: hook.Priority})
	}
	if raw.Requires != nil {
		plugin.Requires = &Requires{Kernel: strings.TrimSpace(raw.Requires.Kernel), ProtocolVersion: raw.Requires.ProtocolVersion, SDK: strings.TrimSpace(raw.Requires.SDK)}
	}
	if err := plugin.Validate(); err != nil {
		return Plugin{}, fmt.Errorf("plugin manifest %s: %w", abs, err)
	}
	return plugin, nil
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
	for i, tool := range p.Tools {
		if tool.Name == "" {
			return fmt.Errorf("tools[%d].name is required", i)
		}
		if tool.DeadlineMS < 0 {
			return fmt.Errorf("tools[%d].deadline_ms must be >= 0", i)
		}
	}
	for i, hook := range p.Hooks {
		if hook.Event == "" {
			return fmt.Errorf("hooks[%d].event is required", i)
		}
	}
	if err := validateRequires(p.Requires); err != nil {
		return err
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

func validateRequires(r *Requires) error {
	if r == nil {
		return fmt.Errorf("[requires] block is required (declare kernel, protocol_version, sdk per docs/PROTOCOL.md §3)")
	}
	if strings.TrimSpace(r.Kernel) == "" {
		return fmt.Errorf("requires.kernel is required")
	}
	if err := validateProtocolVersion(r.ProtocolVersion); err != nil {
		return err
	}
	if strings.TrimSpace(r.SDK) == "" {
		return fmt.Errorf("requires.sdk is required")
	}
	if err := validateSDKRequirement(r.SDK); err != nil {
		return err
	}
	return nil
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

// HasTool reports whether this manifest declares tool.
func (p Plugin) HasTool(tool string) bool {
	for _, candidate := range p.Tools {
		if candidate.Name == tool {
			return true
		}
	}
	return false
}

// RawJSON returns the manifest subset sent in WorkerInit.
func (p Plugin) RawJSON() json.RawMessage {
	data, _ := json.Marshal(p)
	return data
}

// Capability returns the Runtime API capability view for this plugin.
func (p Plugin) Capability() wire.Capability {
	tools := make([]wire.ToolSpec, 0, len(p.Tools))
	for _, tool := range p.Tools {
		tools = append(tools, wire.ToolSpec{Name: tool.Name, Description: tool.Description, DeadlineMS: int64(tool.DeadlineMS)})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	hooks := make([]wire.HookSpec, 0, len(p.Hooks))
	for _, hook := range p.Hooks {
		hooks = append(hooks, wire.HookSpec{Event: hook.Event, Priority: hook.Priority})
	}
	sort.Slice(hooks, func(i, j int) bool {
		if hooks[i].Event == hooks[j].Event {
			return hooks[i].Priority < hooks[j].Priority
		}
		return hooks[i].Event < hooks[j].Event
	})
	return wire.Capability{
		Target:   wire.Target{Kind: wire.TargetKindPlugin, ID: p.ID},
		Tools:    tools,
		Hooks:    hooks,
		Revision: 1,
		State:    wire.CapabilityStateManifestLoaded,
		Source:   wire.CapabilitySourceManifest,
	}
}

// Get returns the plugin for target id.
func (i *Index) Get(id string) (Plugin, bool) {
	if i == nil {
		return Plugin{}, false
	}
	plugin, ok := i.plugins[id]
	return plugin, ok
}

// Capabilities returns capabilities in stable target-id order.
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
		out = append(out, i.plugins[id].Capability())
	}
	return out
}

// Store owns the reloadable manifest index.
type Store struct {
	dirs  []string
	mu    sync.RWMutex
	index *Index
}

// NewStore loads an initial manifest store from dirs.
func NewStore(dirs []string) (*Store, error) {
	idx, err := LoadDirs(dirs)
	if err != nil {
		return nil, err
	}
	return &Store{dirs: append([]string(nil), dirs...), index: idx}, nil
}

// Reload re-reads all configured search dirs.
func (s *Store) Reload() error {
	if s == nil {
		return nil
	}
	idx, err := LoadDirs(s.dirs)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.index = idx
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
	return s.index.Get(id)
}

// Capabilities returns the current stable capability list.
func (s *Store) Capabilities() []wire.Capability {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index.Capabilities()
}

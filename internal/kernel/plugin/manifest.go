package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

var (
	pluginIDPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)
	semverPattern   = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
)

var validRuntimes = map[string]struct{}{
	"python": {},
	"node":   {},
}

// ManifestError reports an invalid plugin.toml manifest. Configuration
// errors of this type are non-transient: the kernel logs them, refuses to
// start the plugin, and does not auto-restart it (creative protocol §2.6).
type ManifestError struct {
	Path string
	Err  error
}

func (e *ManifestError) Error() string {
	if e == nil {
		return "plugin manifest: <nil>"
	}
	if e.Path == "" {
		return fmt.Sprintf("plugin manifest: %v", e.Err)
	}
	return fmt.Sprintf("plugin manifest %s: %v", e.Path, e.Err)
}

func (e *ManifestError) Unwrap() error { return e.Err }

// Manifest is the parsed contents of a plugin's plugin.toml file (per
// creative `memory-bank/creative/creative-manifest-schemas.md`).
//
// Tools and Hooks here are advisory only — the authoritative tool catalog
// and hook subscription list come from the plugin's `register` reply over
// the JSON-RPC channel (see protocol.go RegisterParams). The manifest
// duplicates them for tooling/UX (e.g. distro install can pre-warm tool
// docs without spawning every plugin).
type Manifest struct {
	// [plugin]
	ID          string
	Name        string
	Version     string
	Description string

	// Runtime / entry — how the kernel spawns the plugin process.
	// Runtime selects the launcher family (currently "python" or "node").
	// Entry is the path (relative to RootDir) to the plugin's entry script.
	Runtime string
	Entry   string

	// Tools is the advisory tool list declared in plugin.toml. The
	// authoritative list is the register-reply Tools[].
	Tools []ManifestTool

	// Hooks is the advisory hook subscription list. Authoritative source
	// is the register-reply Subscriptions[].
	Hooks []ManifestHook

	// Config is the default config block from plugin.toml [config]; user
	// overrides are merged on top before being shipped in
	// register_request.config.
	Config map[string]any

	// RootDir is the absolute filesystem path to the plugin's source
	// directory (the directory containing plugin.toml). Used to resolve
	// Entry and to set Cwd for the spawned subprocess.
	RootDir string
}

// ManifestTool mirrors a [[tools]] entry in plugin.toml.
type ManifestTool struct {
	Name        string
	Description string
	DeadlineMs  int
}

// ManifestHook mirrors a [[hooks]] entry in plugin.toml.
type ManifestHook struct {
	Event    string
	Priority int
}

// BootEntry is the boot-time descriptor consumed by Hub.LoadPlugins
// (creative-plugin-runtime.md §3). Each entry points at a manifest on
// disk plus an optional per-plugin user config override.
type BootEntry struct {
	// ManifestPath is the absolute or distro-relative path to plugin.toml.
	ManifestPath string
	// Config is the user override merged on top of Manifest.Config before
	// being delivered to the plugin in register_request.config.
	Config map[string]any
}

// LoadManifest parses and validates a plugin.toml file according to the
// frozen schema in creative-manifest-schemas.md §1.
func LoadManifest(path string) (*Manifest, error) {
	if strings.TrimSpace(path) == "" {
		return nil, manifestErr(path, errors.New("path is required"))
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, manifestErr(path, fmt.Errorf("resolve absolute path: %w", err))
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, manifestErr(abs, err)
	}
	if info.IsDir() {
		abs = filepath.Join(abs, "plugin.toml")
	}

	var raw manifestTOML
	md, err := toml.DecodeFile(abs, &raw)
	if err != nil {
		return nil, manifestErr(abs, err)
	}
	for _, key := range md.Undecoded() {
		// Unknown keys are intentionally tolerated for forward compatibility
		// (creative-manifest-schemas.md §1). A future runtime logger may surface
		// these as WARNs; the parser keeps returning a valid Manifest.
		_ = key
	}

	m := &Manifest{
		ID:          strings.TrimSpace(raw.ID),
		Name:        strings.TrimSpace(raw.Name),
		Version:     strings.TrimSpace(raw.Version),
		Description: strings.TrimSpace(raw.Description),
		Runtime:     strings.TrimSpace(raw.Runtime),
		Entry:       strings.TrimSpace(raw.Entry),
		Tools:       make([]ManifestTool, 0, len(raw.Tools)),
		Hooks:       make([]ManifestHook, 0, len(raw.Hooks)),
		Config:      map[string]any{},
		RootDir:     filepath.Dir(abs),
	}
	if raw.Config.Defaults != nil {
		m.Config = normalizeMap(raw.Config.Defaults)
	}
	for _, tool := range raw.Tools {
		m.Tools = append(m.Tools, ManifestTool{
			Name:        strings.TrimSpace(tool.Name),
			Description: strings.TrimSpace(tool.Description),
			DeadlineMs:  tool.DeadlineMs,
		})
	}
	for _, hook := range raw.Hooks {
		m.Hooks = append(m.Hooks, ManifestHook{
			Event:    strings.TrimSpace(hook.Event),
			Priority: hook.Priority,
		})
	}
	if err := ValidateManifest(m); err != nil {
		return nil, manifestErr(abs, err)
	}
	return m, nil
}

// ValidateManifest checks the schema invariants that are independent of file
// I/O. Runtime uses this after merging or constructing manifests in tests.
func ValidateManifest(m *Manifest) error {
	if m == nil {
		return errors.New("manifest is nil")
	}
	if m.ID == "" {
		return errors.New("id is required")
	}
	if !pluginIDPattern.MatchString(m.ID) {
		return fmt.Errorf("id %q must match ^[a-z0-9_-]+$", m.ID)
	}
	if m.Name == "" {
		return errors.New("name is required")
	}
	if m.Version == "" {
		return errors.New("version is required")
	}
	if !semverPattern.MatchString(m.Version) {
		return fmt.Errorf("version %q must be SemVer X.Y.Z", m.Version)
	}
	if m.Runtime == "" {
		return errors.New("runtime is required")
	}
	if _, ok := validRuntimes[m.Runtime]; !ok {
		return fmt.Errorf("runtime %q is unsupported (expected python or node)", m.Runtime)
	}
	if m.Entry == "" {
		return errors.New("entry is required")
	}
	if err := validateRelativePath("entry", m.Entry); err != nil {
		return err
	}
	for i, tool := range m.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			return fmt.Errorf("tools[%d].name is required", i)
		}
		if tool.DeadlineMs < 0 {
			return fmt.Errorf("tools[%d].deadline_ms must be >= 0", i)
		}
	}
	for i, hook := range m.Hooks {
		if strings.TrimSpace(hook.Event) == "" {
			return fmt.Errorf("hooks[%d].event is required", i)
		}
	}
	return nil
}

func manifestErr(path string, err error) error {
	return &ManifestError{Path: path, Err: err}
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

func normalizeMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = normalizeValue(v)
	}
	return out
}

func normalizeValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return normalizeMap(typed)
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, normalizeMap(item))
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, normalizeValue(item))
		}
		return out
	default:
		return v
	}
}

type manifestTOML struct {
	ID          string             `toml:"id"`
	Name        string             `toml:"name"`
	Version     string             `toml:"version"`
	Description string             `toml:"description"`
	Runtime     string             `toml:"runtime"`
	Entry       string             `toml:"entry"`
	Tags        []string           `toml:"tags"`
	Config      manifestConfigTOML `toml:"config"`
	Tools       []manifestToolTOML `toml:"tools"`
	Hooks       []manifestHookTOML `toml:"hooks"`
}

type manifestConfigTOML struct {
	Schema   map[string]any `toml:"schema"`
	Defaults map[string]any `toml:"defaults"`
}

type manifestToolTOML struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	DeadlineMs  int    `toml:"deadline_ms"`
}

type manifestHookTOML struct {
	Event    string `toml:"event"`
	Priority int    `toml:"priority"`
}

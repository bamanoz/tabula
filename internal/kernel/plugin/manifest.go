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

// validRuntimes lists the plugin runtimes the kernel can spawn today.
//
// Currently Python only. A TypeScript SDK exists for skills
// (`@tabula/skill-sdk`) but no Node-side equivalent of `tabula_plugin_sdk`
// yet. Re-add `"node"` here when a Node plugin SDK ships and the contract
// test suite passes against it (see docs/plans/HERMES_COMPARISON_FOLLOWUPS.md
// §P0.1).
var validRuntimes = map[string]struct{}{
	"python": {},
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
	// Runtime selects the launcher family (currently "python" only;
	// see validRuntimes).
	// Entry is the path (relative to RootDir) to the plugin's entry script.
	Runtime string
	Entry   string

	// Tools is the advisory tool list declared in plugin.toml. The
	// authoritative list is the register-reply Tools[].
	Tools []ManifestTool

	// Hooks is the advisory hook subscription list. Authoritative source
	// is the register-reply Subscriptions[].
	Hooks []ManifestHook

	// Config is legacy register_request config. Runtime plugin config is loaded
	// by plugins via tabula_plugin_sdk.load_plugin_config.
	Config map[string]any

	// Requires declares the plugin's compatibility requirements (kernel
	// version, stdio protocol versions, SDK package version). See
	// docs/PROTOCOL.md §3 for the contract. Currently optional during
	// rollout; will become mandatory once all in-tree and downstream
	// plugin.toml files have been migrated.
	Requires *Requires

	// RootDir is the absolute filesystem path to the plugin's source
	// directory (the directory containing plugin.toml). Used to resolve
	// Entry and to set Cwd for the spawned subprocess.
	RootDir string
}

// Requires expresses a plugin's compatibility contract per docs/PROTOCOL.md §3.
//
// All fields are mandatory once the rollout is complete. During the
// transitional phase a missing block is tolerated by the manifest parser
// but logged as a deprecation warning by callers that care.
type Requires struct {
	// Kernel is a SemVer constraint, e.g. ">=0.9.0,<1.0.0". The kernel
	// version (read from $TABULA_HOME/VERSION or the binary's VERSION)
	// must satisfy this range.
	Kernel Constraint

	// ProtocolVersions enumerates the stdio protocol versions this plugin
	// can speak. Non-empty. The kernel's [Min,Max] range must intersect
	// this set; the negotiation picks the maximum value in the
	// intersection.
	ProtocolVersions []int

	// SDK is the SDK package contract: name + version range.
	SDK SDKRequirement

	// Raw retains the original TOML view for diagnostics and for
	// downstream tools that want to render the constraint as authored.
	Raw RequiresRaw
}

// SDKRequirement names a specific SDK package and the range of versions
// the plugin is compatible with.
type SDKRequirement struct {
	// Name is the SDK package identifier, e.g. "tabula-plugin-sdk" for
	// Python or "@tabula/skill-sdk" for TypeScript.
	Name string
	// Range is a SemVer constraint on the installed SDK version.
	Range Constraint
}

// RequiresRaw is the verbatim TOML form. Keep it for error messages and
// for the lock file (we record what the author wrote, not our normalized
// AST).
type RequiresRaw struct {
	Kernel          string
	ProtocolVersion any // int or []int as authored
	SDK             string
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
	ManifestPath string `json:"manifest_path"`
	// Config is a legacy boot-time override delivered to the plugin in
	// register_request.config. New plugins should use load_plugin_config instead.
	Config map[string]any `json:"config"`
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
	// Do not load runtime config from plugin.toml. Plugin config lives in
	// config/global.toml and config/plugins/<plugin-id>/config.toml.
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
	if raw.Requires != nil {
		req, err := parseRequires(raw.Requires)
		if err != nil {
			return nil, manifestErr(abs, err)
		}
		m.Requires = req
	}
	if err := ValidateManifest(m); err != nil {
		return nil, manifestErr(abs, err)
	}
	return m, nil
}

// parseRequires converts the raw TOML form into a validated Requires.
// All three fields are mandatory once the block is present; the
// manifest-level decision about whether the block itself is mandatory is
// in ValidateManifest.
func parseRequires(raw *requiresTOML) (*Requires, error) {
	if raw == nil {
		return nil, nil
	}
	out := &Requires{
		Raw: RequiresRaw{
			Kernel:          strings.TrimSpace(raw.Kernel),
			ProtocolVersion: raw.ProtocolVersion,
			SDK:             strings.TrimSpace(raw.SDK),
		},
	}
	if out.Raw.Kernel == "" {
		return nil, errors.New("requires.kernel is required")
	}
	cs, err := ParseConstraint(out.Raw.Kernel)
	if err != nil {
		return nil, fmt.Errorf("requires.kernel: %w", err)
	}
	out.Kernel = cs

	versions, err := parseProtocolVersionField(raw.ProtocolVersion)
	if err != nil {
		return nil, err
	}
	out.ProtocolVersions = versions

	if out.Raw.SDK == "" {
		return nil, errors.New("requires.sdk is required")
	}
	name, rangeText, err := splitSDKRequirement(out.Raw.SDK)
	if err != nil {
		return nil, err
	}
	rangeCS, err := ParseConstraint(rangeText)
	if err != nil {
		return nil, fmt.Errorf("requires.sdk range: %w", err)
	}
	out.SDK = SDKRequirement{Name: name, Range: rangeCS}
	return out, nil
}

func parseProtocolVersionField(v any) ([]int, error) {
	if v == nil {
		return nil, errors.New("requires.protocol_version is required")
	}
	asInt := func(x any) (int, bool) {
		switch n := x.(type) {
		case int:
			return n, true
		case int32:
			return int(n), true
		case int64:
			return int(n), true
		}
		return 0, false
	}
	switch x := v.(type) {
	case []any:
		if len(x) == 0 {
			return nil, errors.New("requires.protocol_version must list at least one version")
		}
		out := make([]int, 0, len(x))
		seen := make(map[int]struct{}, len(x))
		for i, item := range x {
			n, ok := asInt(item)
			if !ok || n < 1 {
				return nil, fmt.Errorf("requires.protocol_version[%d] must be a positive integer", i)
			}
			if _, dup := seen[n]; dup {
				return nil, fmt.Errorf("requires.protocol_version has duplicate %d", n)
			}
			seen[n] = struct{}{}
			out = append(out, n)
		}
		return out, nil
	default:
		n, ok := asInt(v)
		if !ok || n < 1 {
			return nil, errors.New("requires.protocol_version must be a positive integer or an array of positive integers")
		}
		return []int{n}, nil
	}
}

// splitSDKRequirement splits "<name><range>" where <range> begins at the
// first ASCII operator character. Examples:
//
//	"tabula-plugin-sdk>=0.1.0,<0.2.0"
//	"@tabula/skill-sdk>=0.1.0"
func splitSDKRequirement(text string) (name, rangeText string, err error) {
	idx := -1
	for i, r := range text {
		if r == '>' || r == '<' || r == '=' {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return "", "", fmt.Errorf("requires.sdk %q: expected '<name><range>' (e.g. tabula-plugin-sdk>=0.1.0)", text)
	}
	name = strings.TrimSpace(text[:idx])
	rangeText = strings.TrimSpace(text[idx:])
	if name == "" {
		return "", "", fmt.Errorf("requires.sdk %q: missing package name", text)
	}
	if rangeText == "" {
		return "", "", fmt.Errorf("requires.sdk %q: missing version range", text)
	}
	return name, rangeText, nil
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
		return fmt.Errorf("runtime %q is unsupported (expected python)", m.Runtime)
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
	if m.Requires == nil {
		return errors.New("[requires] block is required (declare kernel, protocol_version, sdk per docs/PROTOCOL.md §3)")
	}
	if err := validateRequires(m.Requires); err != nil {
		return err
	}
	return nil
}

func validateRequires(r *Requires) error {
	if len(r.Kernel.Clauses) == 0 {
		return errors.New("requires.kernel is required")
	}
	if len(r.ProtocolVersions) == 0 {
		return errors.New("requires.protocol_version is required")
	}
	for i, n := range r.ProtocolVersions {
		if n < 1 {
			return fmt.Errorf("requires.protocol_version[%d] must be >= 1", i)
		}
	}
	if r.SDK.Name == "" {
		return errors.New("requires.sdk name is required")
	}
	if len(r.SDK.Range.Clauses) == 0 {
		return errors.New("requires.sdk range is required")
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

type manifestTOML struct {
	ID          string             `toml:"id"`
	Name        string             `toml:"name"`
	Version     string             `toml:"version"`
	Description string             `toml:"description"`
	Runtime     string             `toml:"runtime"`
	Entry       string             `toml:"entry"`
	Tags        []string           `toml:"tags"`
	Tools       []manifestToolTOML `toml:"tools"`
	Hooks       []manifestHookTOML `toml:"hooks"`
	Requires    *requiresTOML      `toml:"requires"`
}

type requiresTOML struct {
	Kernel          string `toml:"kernel"`
	ProtocolVersion any    `toml:"protocol_version"` // int or []int
	SDK             string `toml:"sdk"`
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

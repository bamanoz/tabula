package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManifestParsesAndValidatesPluginToml(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.toml")
	writePluginToml(t, path, `
id = "mcp_bridge"
name = "MCP Bridge"
version = "1.2.3"
description = "Bridges downstream MCP servers"
runtime = "python"
entry = "run.py"
tags = ["mcp"]

[[tools]]
name = "mcp__fs__read"
description = "Read via MCP"
deadline_ms = 60000

[[hooks]]
event = "before_tool_call"
priority = 80

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=0.1.0,<0.2.0"
`)

	manifest, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if manifest.ID != "mcp_bridge" || manifest.Name != "MCP Bridge" || manifest.Version != "1.2.3" {
		t.Fatalf("unexpected identity: %#v", manifest)
	}
	if manifest.Runtime != "python" || manifest.Entry != "run.py" {
		t.Fatalf("unexpected runtime/entry: %s %s", manifest.Runtime, manifest.Entry)
	}
	if manifest.RootDir != dir {
		t.Fatalf("RootDir = %q, want %q", manifest.RootDir, dir)
	}
	if len(manifest.Config) != 0 {
		t.Fatalf("manifest config should not load from plugin.toml: %#v", manifest.Config)
	}
	if len(manifest.Tools) != 1 || manifest.Tools[0].Name != "mcp__fs__read" || manifest.Tools[0].DeadlineMs != 60000 {
		t.Fatalf("unexpected tools: %#v", manifest.Tools)
	}
	if len(manifest.Hooks) != 1 || manifest.Hooks[0].Event != "before_tool_call" || manifest.Hooks[0].Priority != 80 {
		t.Fatalf("unexpected hooks: %#v", manifest.Hooks)
	}
}

func TestLoadManifestAcceptsDirectoryPath(t *testing.T) {
	dir := t.TempDir()
	writePluginToml(t, filepath.Join(dir, "plugin.toml"), minimalManifest("hello", "python", "run.py"))

	manifest, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest(dir) error = %v", err)
	}
	if manifest.RootDir != dir || manifest.ID != "hello" {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestLoadManifestRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "bad id",
			body: minimalManifest("BadID", "python", "run.py"),
		},
		{
			name: "bad semver",
			body: `id = "hello"
name = "Hello"
version = "1.2"
runtime = "python"
entry = "run.py"
`,
		},
		{
			name: "bad runtime",
			body: minimalManifest("hello", "ruby", "run.py"),
		},
		{
			name: "absolute entry",
			body: minimalManifest("hello", "python", "/tmp/run.py"),
		},
		{
			name: "parent entry",
			body: minimalManifest("hello", "python", "../run.py"),
		},
		{
			name: "empty tool name",
			body: minimalManifest("hello", "python", "run.py") + `
[[tools]]
name = ""
`,
		},
		{
			name: "negative deadline",
			body: minimalManifest("hello", "python", "run.py") + `
[[tools]]
name = "hello"
deadline_ms = -1
`,
		},
		{
			name: "empty hook event",
			body: minimalManifest("hello", "python", "run.py") + `
[[hooks]]
event = ""
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "plugin.toml")
			writePluginToml(t, path, tt.body)

			_, err := LoadManifest(path)
			if err == nil {
				t.Fatalf("LoadManifest() expected error")
			}
			var manifestErr *ManifestError
			if !errors.As(err, &manifestErr) {
				t.Fatalf("error %T is not *ManifestError: %v", err, err)
			}
		})
	}
}

func TestValidateManifestCanBeUsedDirectly(t *testing.T) {
	kernelC, _ := ParseConstraint(">=0.9.0,<1.0.0")
	sdkC, _ := ParseConstraint(">=0.1.0,<0.2.0")
	manifest := &Manifest{
		ID: "hello", Name: "Hello", Version: "0.1.0", Runtime: "python", Entry: "run.py",
		Requires: &Requires{
			Kernel:           kernelC,
			ProtocolVersions: []int{1},
			SDK:              SDKRequirement{Name: "tabula-plugin-sdk", Range: sdkC},
			Raw:              RequiresRaw{Kernel: ">=0.9.0,<1.0.0", ProtocolVersion: 1, SDK: "tabula-plugin-sdk>=0.1.0,<0.2.0"},
		},
	}
	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("ValidateManifest() error = %v", err)
	}
}

func TestValidateManifestRejectsNodeRuntime(t *testing.T) {
	manifest := &Manifest{ID: "hello", Name: "Hello", Version: "0.1.0", Runtime: "node", Entry: "dist/index.js"}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatalf("ValidateManifest() expected error for node runtime")
	}
}

// minimalManifest builds a minimal valid plugin.toml (with [requires] so the
// mandatory-block validator is happy). Tests that exercise the [requires]
// rules directly should use minimalManifestNoRequires and append their own
// block.
func minimalManifest(id, runtime, entry string) string {
	return minimalManifestNoRequires(id, runtime, entry) + standardRequiresBlock()
}

// minimalManifestNoRequires returns a manifest body without the [requires]
// block, for tests that build their own [requires] table or want to
// exercise the missing-block error path.
func minimalManifestNoRequires(id, runtime, entry string) string {
	return `id = "` + id + `"
name = "Hello"
version = "0.1.0"
runtime = "` + runtime + `"
entry = "` + entry + `"
`
}

func standardRequiresBlock() string {
	return `
[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=0.1.0,<0.2.0"
`
}

func writePluginToml(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write plugin.toml: %v", err)
	}
}

func TestLoadManifestParsesRequiresBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.toml")
	writePluginToml(t, path, minimalManifestNoRequires("hello", "python", "run.py")+`
[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=0.1.0,<0.2.0"
`)

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Requires == nil {
		t.Fatalf("Requires is nil")
	}
	if got := m.Requires.Raw.Kernel; got != ">=0.9.0,<1.0.0" {
		t.Errorf("Raw.Kernel = %q", got)
	}
	if len(m.Requires.ProtocolVersions) != 1 || m.Requires.ProtocolVersions[0] != 1 {
		t.Errorf("ProtocolVersions = %v", m.Requires.ProtocolVersions)
	}
	if m.Requires.SDK.Name != "tabula-plugin-sdk" {
		t.Errorf("SDK.Name = %q", m.Requires.SDK.Name)
	}
	v090, _ := ParseVersion("0.9.0")
	if !m.Requires.Kernel.Matches(v090) {
		t.Errorf("Kernel constraint should match 0.9.0")
	}
	v010, _ := ParseVersion("0.1.0")
	if !m.Requires.SDK.Range.Matches(v010) {
		t.Errorf("SDK range should match 0.1.0")
	}
}

func TestLoadManifestRequiresProtocolVersionArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.toml")
	writePluginToml(t, path, minimalManifestNoRequires("hello", "python", "run.py")+`
[requires]
kernel = ">=0.9.0"
protocol_version = [1, 2]
sdk = "tabula-plugin-sdk>=0.1.0"
`)
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if got := m.Requires.ProtocolVersions; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("ProtocolVersions = %v", got)
	}
}

func TestLoadManifestRequiresInvalid(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing kernel",
			body: minimalManifestNoRequires("hello", "python", "run.py") + `
[requires]
protocol_version = 1
sdk = "tabula-plugin-sdk>=0.1.0"
`,
		},
		{
			name: "bad kernel range",
			body: minimalManifestNoRequires("hello", "python", "run.py") + `
[requires]
kernel = "not-a-range"
protocol_version = 1
sdk = "tabula-plugin-sdk>=0.1.0"
`,
		},
		{
			name: "missing protocol_version",
			body: minimalManifestNoRequires("hello", "python", "run.py") + `
[requires]
kernel = ">=0.9.0"
sdk = "tabula-plugin-sdk>=0.1.0"
`,
		},
		{
			name: "protocol_version zero",
			body: minimalManifestNoRequires("hello", "python", "run.py") + `
[requires]
kernel = ">=0.9.0"
protocol_version = 0
sdk = "tabula-plugin-sdk>=0.1.0"
`,
		},
		{
			name: "protocol_version empty array",
			body: minimalManifestNoRequires("hello", "python", "run.py") + `
[requires]
kernel = ">=0.9.0"
protocol_version = []
sdk = "tabula-plugin-sdk>=0.1.0"
`,
		},
		{
			name: "missing sdk",
			body: minimalManifestNoRequires("hello", "python", "run.py") + `
[requires]
kernel = ">=0.9.0"
protocol_version = 1
`,
		},
		{
			name: "sdk without range",
			body: minimalManifestNoRequires("hello", "python", "run.py") + `
[requires]
kernel = ">=0.9.0"
protocol_version = 1
sdk = "tabula-plugin-sdk"
`,
		},
		{
			name: "sdk without name",
			body: minimalManifestNoRequires("hello", "python", "run.py") + `
[requires]
kernel = ">=0.9.0"
protocol_version = 1
sdk = ">=0.1.0"
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "plugin.toml")
			writePluginToml(t, path, tt.body)
			if _, err := LoadManifest(path); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestLoadManifestRejectsMissingRequiresBlock(t *testing.T) {
	// [requires] is mandatory: ValidateManifest rejects manifests without it.
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.toml")
	writePluginToml(t, path, minimalManifestNoRequires("hello", "python", "run.py"))
	_, err := LoadManifest(path)
	if err == nil {
		t.Fatalf("expected error for missing [requires] block")
	}
	if !strings.Contains(err.Error(), "[requires]") {
		t.Fatalf("expected error to mention [requires], got: %v", err)
	}
}

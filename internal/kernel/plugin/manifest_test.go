package plugin

import (
	"errors"
	"os"
	"path/filepath"
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

[config.defaults]
enabled = true
limit = 3

[[tools]]
name = "mcp__fs__read"
description = "Read via MCP"
deadline_ms = 60000

[[hooks]]
event = "before_tool_call"
priority = 80
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
	if got := manifest.Config["enabled"]; got != true {
		t.Fatalf("config.enabled = %#v", got)
	}
	if got := manifest.Config["limit"]; got != int64(3) {
		t.Fatalf("config.limit = %#v", got)
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
	manifest := &Manifest{ID: "hello", Name: "Hello", Version: "0.1.0", Runtime: "node", Entry: "dist/index.js"}
	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("ValidateManifest() error = %v", err)
	}
}

func minimalManifest(id, runtime, entry string) string {
	return `id = "` + id + `"
name = "Hello"
version = "0.1.0"
runtime = "` + runtime + `"
entry = "` + entry + `"
`
}

func writePluginToml(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write plugin.toml: %v", err)
	}
}

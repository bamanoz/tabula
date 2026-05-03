package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestLoadDirsDiscoversPluginManifestsAndCapabilities(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, filepath.Join(dir, "fs", "plugin.toml"), `id = "fs"
name = "Filesystem"
version = "0.1.0"
runtime = "python"
entry = "run.py"

[[tools]]
name = "fs_write"

[[tools]]
name = "fs_read"

[[hooks]]
event = "before_tool_call"
priority = 50

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`)

	idx, err := LoadDirs([]string{dir})
	if err != nil {
		t.Fatalf("LoadDirs: %v", err)
	}
	plugin, ok := idx.Get("fs")
	if !ok {
		t.Fatal("expected fs plugin")
	}
	if plugin.RootDir != filepath.Join(dir, "fs") || plugin.Runtime != "python" || plugin.Entry != "run.py" {
		t.Fatalf("unexpected plugin: %#v", plugin)
	}
	if len(plugin.Hooks) != 1 || plugin.Hooks[0].Event != "before_tool_call" || plugin.Requires == nil || plugin.Requires.SDK == "" {
		t.Fatalf("expected hooks and requires to survive normalized parse: %#v", plugin)
	}
	caps := idx.Capabilities()
	if len(caps) != 1 || caps[0].Target.ID != "fs" || len(caps[0].Tools) != 2 || caps[0].Tools[0].Name != "fs_read" || caps[0].Tools[1].Name != "fs_write" || len(caps[0].Hooks) != 1 || caps[0].Hooks[0].Event != "before_tool_call" || caps[0].State != wire.CapabilityStateManifestLoaded || caps[0].Source != wire.CapabilitySourceManifest {
		t.Fatalf("unexpected capabilities: %#v", caps)
	}
}

func TestLoadDirsIgnoresMissingSearchDir(t *testing.T) {
	idx, err := LoadDirs([]string{filepath.Join(t.TempDir(), "missing")})
	if err != nil {
		t.Fatalf("LoadDirs: %v", err)
	}
	if len(idx.Capabilities()) != 0 {
		t.Fatalf("expected no capabilities: %#v", idx.Capabilities())
	}
}

func TestLoadDirsRejectsDuplicateTargetIDs(t *testing.T) {
	dir := t.TempDir()
	body := `id = "fs"
name = "Filesystem"
version = "0.1.0"
runtime = "python"
entry = "run.py"

[[tools]]
name = "read_file"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`
	writePlugin(t, filepath.Join(dir, "a", "plugin.toml"), body)
	writePlugin(t, filepath.Join(dir, "b", "plugin.toml"), body)
	if _, err := LoadDirs([]string{dir}); err == nil {
		t.Fatal("expected duplicate target error")
	}
}

func TestLoadRejectsInvalidEntryAndTool(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, filepath.Join(dir, "plugin.toml"), `id = "fs"
name = "Filesystem"
version = "0.1.0"
runtime = "python"
entry = "../run.py"

[[tools]]
name = ""

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`)
	if _, err := Load(filepath.Join(dir, "plugin.toml")); err == nil {
		t.Fatal("expected invalid manifest error")
	}
}

func TestLoadRejectsSchemaParityDrift(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing requires",
			body: `id = "fs"
name = "Filesystem"
version = "0.1.0"
runtime = "python"
entry = "run.py"
`,
		},
		{
			name: "unsupported runtime",
			body: `id = "fs"
name = "Filesystem"
version = "0.1.0"
runtime = "bash"
entry = "run.sh"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`,
		},
		{
			name: "invalid version",
			body: `id = "fs"
name = "Filesystem"
version = "dev"
runtime = "python"
entry = "run.py"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`,
		},
		{
			name: "invalid hook",
			body: `id = "fs"
name = "Filesystem"
version = "0.1.0"
runtime = "python"
entry = "run.py"

[[hooks]]
event = ""

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`,
		},
		{
			name: "invalid sdk constraint",
			body: `id = "fs"
name = "Filesystem"
version = "0.1.0"
runtime = "python"
entry = "run.py"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=not-a-version"
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writePlugin(t, filepath.Join(dir, "plugin.toml"), tt.body)
			if _, err := Load(filepath.Join(dir, "plugin.toml")); err == nil {
				t.Fatal("expected manifest schema-parity error")
			}
		})
	}
}

func writePlugin(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write plugin: %v", err)
	}
}

package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestLoadDirsDiscoversPluginManifestsAndCapabilities(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, filepath.Join(dir, "fs", "plugin.toml"), `id = "fs"
name = "Filesystem"
version = "0.1.0"

[worker]
command = ["python3", "run.py"]
mode = "warm"

[[tools]]
name = "fs_write"

[[tools]]
name = "fs_read"

[[hooks]]
event = "before_tool_call"
priority = 50
timeout_ms = 0

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
	if plugin.RootDir != filepath.Join(dir, "fs") || plugin.Worker == nil || len(plugin.Worker.Command) != 2 || plugin.Worker.Command[0] != "python3" || plugin.Worker.Command[1] != "run.py" {
		t.Fatalf("unexpected plugin: %#v", plugin)
	}
	if len(plugin.Hooks) != 1 || plugin.Hooks[0].Event != "before_tool_call" || plugin.Requires == nil || plugin.Requires.SDK == "" {
		t.Fatalf("expected hooks and requires to survive normalized parse: %#v", plugin)
	}
	if plugin.Hooks[0].TimeoutMS == nil || *plugin.Hooks[0].TimeoutMS != 0 {
		t.Fatalf("expected hook timeout_ms to survive normalized parse: %#v", plugin.Hooks[0])
	}
	caps := idx.Capabilities()
	if len(caps) != 1 || caps[0].Target.ID != "fs" || len(caps[0].Tools) != 2 || caps[0].Tools[0].Name != "fs_read" || caps[0].Tools[1].Name != "fs_write" || len(caps[0].Hooks) != 1 || caps[0].Hooks[0].Event != "before_tool_call" || caps[0].State != wire.CapabilityStateManifestLoaded || caps[0].Source != wire.CapabilitySourceManifest {
		t.Fatalf("unexpected capabilities: %#v", caps)
	}
	if caps[0].Hooks[0].TimeoutMS == nil || *caps[0].Hooks[0].TimeoutMS != 0 {
		t.Fatalf("expected hook timeout_ms in capability: %#v", caps[0].Hooks[0])
	}
	if caps[0].WorkerMode != wire.WorkerModeWarm || caps[0].HarnessKind != wire.HarnessKindPython {
		t.Fatalf("unexpected plugin capability runtime metadata: %#v", caps[0])
	}
	if caps[0].Tools[0].Concurrency != wire.ToolConcurrencySerial || caps[0].Tools[0].ExecutionGroup != "fs_read" || len(caps[0].Tools[0].ConflictsWithGroups) != 1 || caps[0].Tools[0].ConflictsWithGroups[0] != "fs_read" {
		t.Fatalf("expected default execution policy on fs_read, got %#v", caps[0].Tools[0])
	}
	if caps[0].Tools[1].Concurrency != wire.ToolConcurrencySerial || caps[0].Tools[1].ExecutionGroup != "fs_write" || len(caps[0].Tools[1].ConflictsWithGroups) != 1 || caps[0].Tools[1].ConflictsWithGroups[0] != "fs_write" {
		t.Fatalf("expected default execution policy on fs_write, got %#v", caps[0].Tools[1])
	}
}

func TestLoadDirsParsesPluginKind(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, filepath.Join(dir, "driver", "plugin.toml"), `id = "driver"
name = "Driver"
version = "0.1.0"

[kind]
name = "driver"
singleton = true

[worker]
command = ["python3", "run.py"]
mode = "warm"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`)

	idx, err := LoadDirs([]string{dir})
	if err != nil {
		t.Fatalf("LoadDirs: %v", err)
	}
	plugin, ok := idx.Get("driver")
	if !ok || plugin.Kind == nil || plugin.Kind.Name != "driver" || !plugin.Kind.Singleton {
		t.Fatalf("kind not parsed: %#v ok=%v", plugin, ok)
	}
}

func TestLoadDirsRejectsDuplicateSingletonPluginKind(t *testing.T) {
	dir := t.TempDir()
	body := func(id string) string {
		return `id = "` + id + `"
name = "Driver"
version = "0.1.0"

[kind]
name = "driver"
singleton = true

[worker]
command = ["python3", "run.py"]
mode = "warm"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`
	}
	writePlugin(t, filepath.Join(dir, "driver-a", "plugin.toml"), body("driver-a"))
	writePlugin(t, filepath.Join(dir, "driver-b", "plugin.toml"), body("driver-b"))

	_, err := LoadDirs([]string{dir})
	if err == nil || !strings.Contains(err.Error(), `plugin kind "driver" is singleton`) {
		t.Fatalf("expected singleton kind error, got %v", err)
	}
}

func TestNewTenantStoreAllowsSameSingletonPluginIDAcrossTenants(t *testing.T) {
	alphaDir := t.TempDir()
	betaDir := t.TempDir()
	body := `id = "driver"
name = "Driver"
version = "0.1.0"

[kind]
name = "driver"
singleton = true

[worker]
command = ["python3", "run.py"]
mode = "warm"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`
	writePlugin(t, filepath.Join(alphaDir, "driver", "plugin.toml"), body)
	writePlugin(t, filepath.Join(betaDir, "driver", "plugin.toml"), body)
	if _, err := NewTenantStore(map[string]SearchDirs{"alpha": {PluginDirs: []string{alphaDir}}, "beta": {PluginDirs: []string{betaDir}}}); err != nil {
		t.Fatalf("NewTenantStore: %v", err)
	}
}

func TestNewTenantStoreRejectsDifferentSingletonPluginIDsAcrossTenants(t *testing.T) {
	alphaDir := t.TempDir()
	betaDir := t.TempDir()
	body := func(id string) string {
		return `id = "` + id + `"
name = "Driver"
version = "0.1.0"

[kind]
name = "driver"
singleton = true

[worker]
command = ["python3", "run.py"]
mode = "warm"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`
	}
	writePlugin(t, filepath.Join(alphaDir, "driver-a", "plugin.toml"), body("driver-a"))
	writePlugin(t, filepath.Join(betaDir, "driver-b", "plugin.toml"), body("driver-b"))
	_, err := NewTenantStore(map[string]SearchDirs{"alpha": {PluginDirs: []string{alphaDir}}, "beta": {PluginDirs: []string{betaDir}}})
	if err == nil || !strings.Contains(err.Error(), `plugin kind "driver" is singleton`) {
		t.Fatalf("expected singleton kind error, got %v", err)
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

[worker]
command = ["python3", "run.py"]
mode = "warm"

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

func TestLoadDirsDiscoversSymlinkedPluginDirectories(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "fs")
	writePlugin(t, filepath.Join(source, "plugin.toml"), pluginManifest("fs", "fs_read"))
	if err := os.Symlink(source, filepath.Join(root, "fs")); err != nil {
		t.Fatalf("symlink plugin dir: %v", err)
	}

	idx, err := LoadDirs([]string{root})
	if err != nil {
		t.Fatalf("LoadDirs: %v", err)
	}
	caps := idx.Capabilities()
	if len(caps) != 1 || caps[0].Target.ID != "fs" || len(caps[0].Tools) != 1 || caps[0].Tools[0].Name != "fs_read" {
		t.Fatalf("capabilities = %#v", caps)
	}
}

func TestLoadAcceptsColdWorkerModeForToolOnlyPlugin(t *testing.T) {
	dir := t.TempDir()
	body := `id = "question"
name = "Question"
version = "0.1.0"

[worker]
command = ["python3", "run.py"]
mode = "cold"

[[tools]]
name = "question"
schema_json = '{"type":"object","properties":{"questions":{"type":"array"}},"required":["questions"]}'

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`
	path := filepath.Join(dir, "question", "plugin.toml")
	writePlugin(t, path, body)
	plugin, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if plugin.WorkerMode != wire.WorkerModeCold {
		t.Fatalf("worker mode = %q, want cold", plugin.WorkerMode)
	}
	if got := string(plugin.Tools[0].Schema); got != `{"type":"object","properties":{"questions":{"type":"array"}},"required":["questions"]}` {
		t.Fatalf("tool schema = %s", got)
	}
	if plugin.Capability().WorkerMode != wire.WorkerModeCold {
		t.Fatalf("capability worker mode = %q, want cold", plugin.Capability().WorkerMode)
	}
	if got := string(plugin.Capability().Tools[0].Schema); got != `{"type":"object","properties":{"questions":{"type":"array"}},"required":["questions"]}` {
		t.Fatalf("capability tool schema = %s", got)
	}
}

func TestLoadAcceptsWorkerCommandWithoutSDKRequirement(t *testing.T) {
	dir := t.TempDir()
	body := `id = "testbed-cold-node"
name = "Testbed Cold Node"
version = "0.1.0"
description = "Deterministic node fixture"

[worker]
command = ["node", "scripts/run.js"]
mode = "cold"

[[tools]]
name = "testbed_cold_node"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
`
	path := filepath.Join(dir, "testbed-cold-node", "plugin.toml")
	writePlugin(t, path, body)
	plugin, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if plugin.Worker == nil || plugin.WorkerMode != wire.WorkerModeCold {
		t.Fatalf("expected canonical worker config, got %#v", plugin)
	}
	if got := plugin.Worker.Command; len(got) != 2 || got[0] != "node" || got[1] != "scripts/run.js" {
		t.Fatalf("worker.command = %#v", got)
	}
	if plugin.Requires == nil || plugin.Requires.SDK != "" {
		t.Fatalf("expected optional sdk requirement, got %#v", plugin.Requires)
	}
	if cap := plugin.Capability(); cap.HarnessKind != wire.HarnessKindNode || cap.WorkerMode != wire.WorkerModeCold {
		t.Fatalf("unexpected capability: %#v", cap)
	}
}

func TestLoadRejectsEmptyWorkerCommand(t *testing.T) {
	dir := t.TempDir()
	body := `id = "demo"
name = "Demo"
version = "0.1.0"

[worker]
command = []

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
`
	path := filepath.Join(dir, "demo", "plugin.toml")
	writePlugin(t, path, body)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "worker.command must be a non-empty argv list") {
		t.Fatalf("expected worker.command validation error, got %v", err)
	}
}

func TestLoadAcceptsRuntimeWorkerScopeForWarmPlugin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.toml")
	writePlugin(t, path, `id = "gateway"
name = "Gateway"
version = "1.0.0"
runtime = "python"
entry = "run.py"

[worker]
command = ["python3", "run.py"]
mode = "warm"
scope = "runtime"

[[tools]]
name = "status"
description = "Status"
schema_json = "{}"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`)

	plugin, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if plugin.WorkerScope != wire.WorkerScopeRuntime || plugin.Worker == nil || plugin.Worker.Scope != wire.WorkerScopeRuntime {
		t.Fatalf("worker scope = plugin:%q worker:%#v", plugin.WorkerScope, plugin.Worker)
	}
	if plugin.Capability().WorkerScope != wire.WorkerScopeRuntime {
		t.Fatalf("capability worker scope = %q", plugin.Capability().WorkerScope)
	}
}

func TestLoadRejectsRuntimeWorkerScopeForColdPlugin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.toml")
	writePlugin(t, path, `id = "gateway"
name = "Gateway"
version = "1.0.0"
runtime = "python"
entry = "run.py"

[worker]
command = ["python3", "run.py"]
mode = "cold"
scope = "runtime"

[[tools]]
name = "status"
description = "Status"
schema_json = "{}"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`)

	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "worker.scope runtime requires worker_mode warm") {
		t.Fatalf("Load err = %v", err)
	}
}

func TestLoadRejectsColdWorkerModeForHookPlugin(t *testing.T) {
	dir := t.TempDir()
	body := `id = "fs"
name = "Filesystem"
version = "0.1.0"

[worker]
command = ["python3", "run.py"]
mode = "cold"

[[tools]]
name = "fs_read"

[[hooks]]
event = "before_tool_call"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`
	path := filepath.Join(dir, "fs", "plugin.toml")
	writePlugin(t, path, body)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "worker_mode cold does not support hooks") {
		t.Fatalf("expected cold hook plugin rejection, got %v", err)
	}
}

func TestLoadAcceptsExplicitToolExecutionPolicy(t *testing.T) {
	dir := t.TempDir()
	body := `id = "fs"
name = "Filesystem"
version = "0.1.0"

[worker]
command = ["python3", "run.py"]
mode = "warm"

[[tools]]
name = "fs_grep"
concurrency = "parallel"
execution_group = "fs-read"
conflicts_with_groups = ["fs-write"]

[[tools]]
name = "fs_write"
concurrency = "serial"
execution_group = "fs-write"
conflicts_with_groups = ["fs-read", "fs-write"]

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`
	path := filepath.Join(dir, "fs", "plugin.toml")
	writePlugin(t, path, body)
	plugin, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if plugin.Tools[0].Concurrency != wire.ToolConcurrencyParallel || plugin.Tools[0].ExecutionGroup != "fs-read" || len(plugin.Tools[0].ConflictsWithGroups) != 1 || plugin.Tools[0].ConflictsWithGroups[0] != "fs-write" {
		t.Fatalf("unexpected fs_grep execution policy: %#v", plugin.Tools[0])
	}
	if plugin.Tools[1].Concurrency != wire.ToolConcurrencySerial || plugin.Tools[1].ExecutionGroup != "fs-write" || len(plugin.Tools[1].ConflictsWithGroups) != 2 {
		t.Fatalf("unexpected fs_write execution policy: %#v", plugin.Tools[1])
	}
	cap := plugin.Capability()
	if cap.Tools[0].Name != "fs_grep" || cap.Tools[0].Concurrency != wire.ToolConcurrencyParallel || cap.Tools[0].ExecutionGroup != "fs-read" || len(cap.Tools[0].ConflictsWithGroups) != 1 || cap.Tools[0].ConflictsWithGroups[0] != "fs-write" {
		t.Fatalf("unexpected capability tool policy: %#v", cap.Tools[0])
	}
}

func TestLoadRejectsInvalidToolExecutionPolicy(t *testing.T) {
	dir := t.TempDir()
	body := `id = "fs"
name = "Filesystem"
version = "0.1.0"

[worker]
command = ["python3", "run.py"]
mode = "warm"

[[tools]]
name = "fs_grep"
concurrency = "sideways"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`
	path := filepath.Join(dir, "fs", "plugin.toml")
	writePlugin(t, path, body)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "concurrency") {
		t.Fatalf("expected invalid concurrency error, got %v", err)
	}
}

func TestTenantStoreAllowsDuplicatePluginIDsAcrossTenants(t *testing.T) {
	alphaDir := t.TempDir()
	betaDir := t.TempDir()
	writePlugin(t, filepath.Join(alphaDir, "fs", "plugin.toml"), pluginManifest("fs", "alpha_read"))
	writePlugin(t, filepath.Join(betaDir, "fs", "plugin.toml"), pluginManifest("fs", "beta_read"))

	store, err := NewTenantStore(map[string]SearchDirs{
		"alpha": {PluginDirs: []string{alphaDir}},
		"beta":  {PluginDirs: []string{betaDir}},
	})
	if err != nil {
		t.Fatalf("NewTenantStore: %v", err)
	}
	alphaPlugin, ok := store.GetForTenant("alpha", "fs")
	if !ok || alphaPlugin.Tools[0].Name != "alpha_read" {
		t.Fatalf("alpha plugin = %#v ok=%v", alphaPlugin, ok)
	}
	betaPlugin, ok := store.GetForTenant("beta", "fs")
	if !ok || betaPlugin.Tools[0].Name != "beta_read" {
		t.Fatalf("beta plugin = %#v ok=%v", betaPlugin, ok)
	}
	capabilities := store.Capabilities()
	if len(capabilities) != 2 || capabilities[0].Target.ID != "fs" || capabilities[0].Tenants[0] != "alpha" || capabilities[1].Tenants[0] != "beta" {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	if _, ok := store.Get("fs"); ok {
		t.Fatal("tenant store should not expose duplicate plugin through global lookup")
	}
}

func TestReloadTenantDiscoversTenantRuntimeSurface(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, filepath.Join(dir, "tenants", "alpha", "plugins", "fs", "plugin.toml"), pluginManifest("fs", "alpha_read"))
	store, err := NewSearchStore(nil, nil)
	if err != nil {
		t.Fatalf("NewSearchStore: %v", err)
	}
	store.SetTabulaHome(dir)

	if err := store.ReloadTenant("alpha"); err != nil {
		t.Fatalf("ReloadTenant: %v", err)
	}

	caps := store.CapabilitiesForTenant("alpha")
	if len(caps) != 1 || caps[0].Target.ID != "fs" || len(caps[0].Tools) != 1 || caps[0].Tools[0].Name != "alpha_read" {
		t.Fatalf("tenant capabilities = %#v", caps)
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

[worker]
command = ["python3", "run.py"]
mode = "warm"
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

[worker]
command = ["python3", "run.py"]
mode = "warm"

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

[worker]
command = ["python3", "run.py"]
mode = "warm"

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

[worker]
command = ["python3", "run.py"]
mode = "warm"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=not-a-version"
`,
		},
		{
			name: "invalid tool schema json",
			body: `id = "fs"
name = "Filesystem"
version = "0.1.0"

[worker]
command = ["python3", "run.py"]
mode = "warm"

[[tools]]
name = "fs_read"
schema_json = '{not-json}'

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
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

func TestLoadDirsDiscoversSkillsAndReportsMetadata(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "skills", "timer", "SKILL.md"), `---
name: timer
description: "Short timers"
---
`)

	idx, err := LoadDirs([]string{dir})
	if err != nil {
		t.Fatalf("LoadDirs: %v", err)
	}
	skill, ok := idx.GetSkill("skill:timer")
	if !ok {
		t.Fatal("expected timer skill")
	}
	if skill.Name != "timer" || skill.Description != "Short timers" {
		t.Fatalf("unexpected skill manifest: %#v", skill)
	}
	caps := idx.Capabilities()
	if len(caps) != 0 {
		t.Fatalf("skills must not publish executable capabilities: %#v", caps)
	}
}

func TestLoadSearchDirsReadsExplicitSkillDirs(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills")
	writeSkill(t, filepath.Join(skillDir, "cold", "SKILL.md"), `---
name: cold
description: "Cold skill"
---
`)

	idx, err := LoadSearchDirs(nil, []string{skillDir})
	if err != nil {
		t.Fatalf("LoadSearchDirs: %v", err)
	}
	if _, ok := idx.GetSkill("skill:cold"); !ok {
		t.Fatal("expected explicit skill dir to load skill")
	}
	caps := idx.Capabilities()
	if len(caps) != 0 {
		t.Fatalf("skills must not publish executable capabilities: %#v", caps)
	}
}

func TestLoadSearchDirsFollowsSymlinkedSkillDirectories(t *testing.T) {
	source := t.TempDir()
	writeSkill(t, filepath.Join(source, "cold", "SKILL.md"), `---
name: cold
description: "Cold skill"
---
`)
	installed := t.TempDir()
	installedSkills := filepath.Join(installed, "skills")
	if err := os.MkdirAll(installedSkills, 0o755); err != nil {
		t.Fatalf("mkdir installed skills: %v", err)
	}
	if err := os.Symlink(filepath.Join(source, "cold"), filepath.Join(installedSkills, "cold")); err != nil {
		t.Fatalf("symlink skill: %v", err)
	}

	idx, err := LoadSearchDirs(nil, []string{installedSkills})
	if err != nil {
		t.Fatalf("LoadSearchDirs: %v", err)
	}
	if _, ok := idx.GetSkill("skill:cold"); !ok {
		t.Fatal("expected symlinked skill dir to load skill")
	}
}

func TestLoadDirsSkipsMalformedSkillsAndKeepsOthers(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "skills", "good", "SKILL.md"), `---
name: good
description: "Good skill"
---
`)
	writeSkill(t, filepath.Join(dir, "skills", "bad", "SKILL.md"), `---
name bad
tools:
  - nope
---
`)

	idx, err := LoadDirs([]string{dir})
	if err != nil {
		t.Fatalf("LoadDirs: %v", err)
	}
	if _, ok := idx.GetSkill("skill:good"); !ok {
		t.Fatal("expected good skill to remain loaded")
	}
	if _, ok := idx.GetSkill("skill:bad"); ok {
		t.Fatal("malformed skill should be skipped")
	}
	if len(idx.Capabilities()) != 0 {
		t.Fatalf("skills must not publish executable capabilities after malformed skip: %#v", idx.Capabilities())
	}
}

func TestLoadSkillAllowsZeroToolSkills(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skills", "guide", "SKILL.md")
	writeSkill(t, path, `---
name: guide
description: >
  Routing skill with no tools.
---
`)
	skill, err := LoadSkill(path)
	if err != nil {
		t.Fatalf("LoadSkill: %v", err)
	}
	if skill.Name != "guide" || skill.TargetID() != "skill:guide" {
		t.Fatalf("unexpected skill: %#v", skill)
	}
}

func TestLoadSkillIgnoresLegacyToolFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skills", "legacy", "SKILL.md")
	writeSkill(t, path, `---
name: legacy
description: "Legacy tool metadata should be ignored"
tools:
  - name: old_tool
    exec: "python3 run.py"
---
`)
	skill, err := LoadSkill(path)
	if err != nil {
		t.Fatalf("LoadSkill: %v", err)
	}
	if skill.Name != "legacy" || skill.Description != "Legacy tool metadata should be ignored" {
		t.Fatalf("unexpected skill: %#v", skill)
	}
}

func TestLoadSkillParsesAllBundleSkills(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "tabula-bundles")
	paths, err := skillManifestPaths(root)
	if err != nil {
		t.Fatalf("skillManifestPaths: %v", err)
	}
	if len(paths) == 0 {
		t.Skip("tabula-bundles sibling repo not present")
	}
	for _, path := range paths {
		if _, err := LoadSkill(path); err != nil {
			t.Fatalf("LoadSkill(%s): %v", path, err)
		}
	}
}

func TestStoreReloadPicksUpAddedAndRemovedSkills(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "skills", "one", "SKILL.md"), `---
name: one
description: "One"
---
`)
	store, err := NewStore([]string{dir})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, ok := store.GetSkill("skill:one"); !ok {
		t.Fatal("expected initial skill")
	}
	if err := os.Remove(filepath.Join(dir, "skills", "one", "SKILL.md")); err != nil {
		t.Fatalf("remove skill: %v", err)
	}
	writeSkill(t, filepath.Join(dir, "skills", "two", "SKILL.md"), `---
name: two
description: "Two"
---
`)
	if err := store.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if _, ok := store.GetSkill("skill:one"); ok {
		t.Fatal("removed skill should disappear after reload")
	}
	if _, ok := store.GetSkill("skill:two"); !ok {
		t.Fatal("new skill should appear after reload")
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

func pluginManifest(id, tool string) string {
	return `id = "` + id + `"
name = "Plugin"
version = "0.1.0"

[worker]
command = ["python3", "run.py"]
mode = "warm"

[[tools]]
name = "` + tool + `"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`
}

func writeSkill(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}

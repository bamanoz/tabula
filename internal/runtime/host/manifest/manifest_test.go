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
runtime = "python"
entry = "run.py"

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
	if plugin.RootDir != filepath.Join(dir, "fs") || plugin.Runtime != "python" || plugin.Entry != "run.py" {
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
runtime = "python"
entry = "run.py"
worker_mode = "cold"

[[tools]]
name = "question"

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
	if plugin.Capability().WorkerMode != wire.WorkerModeCold {
		t.Fatalf("capability worker mode = %q, want cold", plugin.Capability().WorkerMode)
	}
}

func TestLoadRejectsColdWorkerModeForHookPlugin(t *testing.T) {
	dir := t.TempDir()
	body := `id = "fs"
name = "Filesystem"
version = "0.1.0"
runtime = "python"
entry = "run.py"
worker_mode = "cold"

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

func TestLoadDirsDiscoversSkillsAndReportsMetadata(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, filepath.Join(dir, "skills", "timer", "SKILL.md"), `---
name: timer
description: "Short timers"
tools:
  - name: timer_start
    description: "Start a timer"
    params:
      after: { type: string }
      message: { type: string }
    required: [after, message]
    exec: "python3 skills/timer/scripts/run.py tool timer_start"
  - name: timer_list
    description: "List timers"
    params: {}
    required: []
    exec: "python3 skills/timer/scripts/run.py tool timer_list"
  - name: timer_cancel
    description: "Cancel timer"
    params:
      id: { type: string }
    required: [id]
    exec: "python3 skills/timer/scripts/run.py tool timer_cancel"
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
	if skill.WorkerMode != wire.WorkerModeCold || skill.HarnessKind != wire.HarnessKindPython || len(skill.Tools) != 3 {
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
tools:
  - name: cold_tool
    description: "Cold"
    params: {}
    required: []
    exec: "python3 ${SKILL_DIR}/scripts/run.py tool cold_tool"
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
tools:
  - name: cold_tool
    description: "Cold"
    params: {}
    required: []
    exec: "python3 ${SKILL_DIR}/scripts/run.py tool cold_tool"
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
tools:
  - name: good_tool
    description: "Good"
    params: {}
    required: []
    exec: "python3 skills/good/scripts/run.py tool good_tool"
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
	if len(skill.Tools) != 0 {
		t.Fatalf("expected zero tools: %#v", skill)
	}
	cap := skill.Capability()
	if cap.Target.ID != "skill:guide" || len(cap.Tools) != 0 || cap.WorkerMode != wire.WorkerModeCold {
		t.Fatalf("unexpected zero-tool capability: %#v", cap)
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
tools:
  - name: one_tool
    description: "One"
    params: {}
    required: []
    exec: "python3 skills/one/scripts/run.py tool one_tool"
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
tools:
  - name: two_tool
    description: "Two"
    params: {}
    required: []
    exec: "python3 skills/two/scripts/run.py tool two_tool"
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
runtime = "python"
entry = "run.py"

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

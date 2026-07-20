package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadRuntimeConfigUsesTokenFileAndExpandsEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TABULA_HOME", dir)
	path := filepath.Join(dir, "runtime.toml")
	writeFile(t, path, `[[kernel]]
id = "local"
url = "unix://${TABULA_HOME}/run/runtime.sock"
token_file = "${TABULA_HOME}/run/runtime-token"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	k, err := cfg.SingleKernel()
	if err != nil {
		t.Fatalf("SingleKernel: %v", err)
	}
	if k.URL != "unix://"+filepath.Join(dir, "run", "runtime.sock") {
		t.Fatalf("expanded url = %q", k.URL)
	}
	if k.TokenFile != filepath.Join(dir, "run", "runtime-token") {
		t.Fatalf("expanded token_file = %q", k.TokenFile)
	}
	if len(k.Tenants) != 1 || k.Tenants[0] != "*" {
		t.Fatalf("default tenants = %#v", k.Tenants)
	}
	if len(cfg.PluginDirs) != 1 || cfg.PluginDirs[0] != filepath.Join(dir, "plugins") {
		t.Fatalf("default plugin dirs = %#v", cfg.PluginDirs)
	}
	if cfg.Pool.ColdWorkersPerTenantMax != 16 {
		t.Fatalf("default cold worker limit = %d", cfg.Pool.ColdWorkersPerTenantMax)
	}
}

func TestLoadRuntimeConfigAcceptsWSSAndTLSFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TABULA_HOME", dir)
	path := filepath.Join(dir, "runtime.toml")
	writeFile(t, path, `[[kernel]]
id = "remote"
url = "wss://kernel.example.com/runtime"
token_file = "/tmp/runtime-token"
ca_file = "${TABULA_HOME}/server.ca"
cert_file = "${TABULA_HOME}/runtime.crt"
key_file = "${TABULA_HOME}/runtime.key"
tls_insecure_skip_verify = true
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	k, err := cfg.SingleKernel()
	if err != nil {
		t.Fatalf("SingleKernel: %v", err)
	}
	if k.URL != "wss://kernel.example.com/runtime" {
		t.Fatalf("url = %q", k.URL)
	}
	if !k.TLSInsecureSkipVerify {
		t.Fatal("expected tls_insecure_skip_verify to be preserved")
	}
	if k.CAFile != filepath.Join(dir, "server.ca") || k.CertFile != filepath.Join(dir, "runtime.crt") || k.KeyFile != filepath.Join(dir, "runtime.key") {
		t.Fatalf("unexpected tls paths: %#v", k)
	}
}

func TestLoadRuntimeConfigRejectsPartialClientCertificate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.toml")
	writeFile(t, path, `[[kernel]]
id = "remote"
url = "wss://kernel.example.com/runtime"
token_file = "/tmp/runtime-token"
cert_file = "/tmp/runtime.crt"
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "cert_file and key_file") {
		t.Fatalf("expected cert/key validation error, got %v", err)
	}
}

func TestLoadRuntimeConfigParsesTenantAllowlistAndOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime.toml")
	writeFile(t, path, `plugin_dirs = ["/tmp/plugins"]

[pool]
cold_workers_per_tenant_max = 4

[pool.tenants.alpha]
cold_workers_max = 2

[[kernel]]
id = "local"
url = "unix:///tmp/runtime.sock"
token_file = "/tmp/runtime-token"
tenants = ["alpha", "beta"]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	k, _ := cfg.SingleKernel()
	if strings.Join(k.Tenants, ",") != "alpha,beta" {
		t.Fatalf("tenants = %#v", k.Tenants)
	}
	if cfg.Pool.Tenants["alpha"].ColdWorkersMax != 2 {
		t.Fatalf("pool tenant override = %#v", cfg.Pool.Tenants)
	}
}

func TestLoadRuntimeConfigParsesPluginKindDependencies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.toml")
	writeFile(t, path, `plugin_dirs = ["/tmp/plugins"]

[plugin_kinds.gateway]
depends_on = ["driver"]

[[kernel]]
id = "local"
url = "unix:///tmp/runtime.sock"
token_file = "/tmp/runtime-token"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg.PluginKinds["gateway"].DependsOn, []string{"driver"}) {
		t.Fatalf("plugin kind deps = %#v", cfg.PluginKinds)
	}
}

func TestLoadRuntimeConfigParsesRuntimeCommands(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TABULA_PYTHON", filepath.Join(dir, "python"))
	path := filepath.Join(dir, "runtime.toml")
	writeFile(t, path, `[runtimes.python]
command = ["${TABULA_PYTHON}", "-I"]

[[kernel]]
id = "local"
url = "unix:///tmp/runtime.sock"
token_file = "/tmp/runtime-token"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{filepath.Join(dir, "python"), "-I"}
	if !reflect.DeepEqual(cfg.Runtimes["python"].Command, want) {
		t.Fatalf("python runtime command = %#v, want %#v", cfg.Runtimes["python"].Command, want)
	}
}

func TestLoadRuntimeConfigRejectsEmptyRuntimeCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.toml")
	writeFile(t, path, `[runtimes.python]
command = []

[[kernel]]
id = "local"
url = "unix:///tmp/runtime.sock"
token_file = "/tmp/runtime-token"
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "runtimes.python.command") {
		t.Fatalf("expected runtime command validation error, got %v", err)
	}
}

func TestLoadRuntimeConfigRejectsInvalidPluginKindDependencies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.toml")
	writeFile(t, path, `plugin_dirs = ["/tmp/plugins"]

[plugin_kinds.gateway]
depends_on = ["gateway"]

[[kernel]]
id = "local"
url = "unix:///tmp/runtime.sock"
token_file = "/tmp/runtime-token"
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "cannot depend on itself") {
		t.Fatalf("expected self dependency error, got %v", err)
	}
}

func TestLoadRuntimeConfigRejectsMixedWildcardTenantAllowlist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.toml")
	writeFile(t, path, `plugin_dirs = ["/tmp/plugins"]

[[kernel]]
id = "local"
url = "unix:///tmp/runtime.sock"
token_file = "/tmp/runtime-token"
tenants = ["*", "alpha"]
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "cannot mix") {
		t.Fatalf("expected wildcard mix error, got %v", err)
	}
}

func TestLoadRuntimeConfigExpandsPluginAndSkillDirs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TABULA_HOME", dir)
	path := filepath.Join(dir, "runtime.toml")
	writeFile(t, path, `plugin_dirs = ["${TABULA_HOME}/plugins", "${TABULA_HOME}/extra-plugins"]
skill_dirs = ["${TABULA_HOME}/skills"]

[[kernel]]
id = "local"
url = "unix:///tmp/runtime.sock"
token_file = "${TABULA_HOME}/run/runtime-token"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.PluginDirs) != 2 || cfg.PluginDirs[1] != filepath.Join(dir, "extra-plugins") {
		t.Fatalf("plugin dirs = %#v", cfg.PluginDirs)
	}
	if len(cfg.SkillDirs) != 1 || cfg.SkillDirs[0] != filepath.Join(dir, "skills") {
		t.Fatalf("skill dirs = %#v", cfg.SkillDirs)
	}
}

func TestLoadRuntimeConfigParsesTenantCatalogs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TABULA_HOME", dir)
	path := filepath.Join(dir, "runtime.toml")
	writeFile(t, path, `plugin_dirs = ["${TABULA_HOME}/plugins"]

[[tenant]]
id = "alpha"
plugin_dirs = ["${TABULA_HOME}/tenants/alpha/plugins"]
skill_dirs = ["${TABULA_HOME}/tenants/alpha/skills"]

[[tenant]]
id = "beta"
plugin_dirs = ["${TABULA_HOME}/tenants/beta/plugins"]

[[kernel]]
id = "local"
url = "unix:///tmp/runtime.sock"
token_file = "${TABULA_HOME}/run/runtime-token"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Tenants) != 2 || cfg.Tenants[0].ID != "alpha" || cfg.Tenants[1].ID != "beta" {
		t.Fatalf("tenant catalogs = %#v", cfg.Tenants)
	}
	if cfg.Tenants[0].PluginDirs[0] != filepath.Join(dir, "tenants", "alpha", "plugins") {
		t.Fatalf("alpha plugin dirs = %#v", cfg.Tenants[0].PluginDirs)
	}
	if cfg.Tenants[0].SkillDirs[0] != filepath.Join(dir, "tenants", "alpha", "skills") {
		t.Fatalf("alpha skill dirs = %#v", cfg.Tenants[0].SkillDirs)
	}
}

func TestLoadRuntimeConfigParsesTenantCatalogsBeforeKernel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TABULA_HOME", dir)
	path := filepath.Join(dir, "runtime.toml")
	writeFile(t, path, `plugin_dirs = []
skill_dirs = []

[[tenant]]
id = "alpha"
plugin_dirs = ["${TABULA_HOME}/tenants/alpha/plugins"]
skill_dirs = ["${TABULA_HOME}/tenants/alpha/skills"]

[[kernel]]
id = "local"
url = "unix:///tmp/runtime.sock"
token_file = "${TABULA_HOME}/run/runtime-token"
tenants = ["alpha"]
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Tenants) != 1 || cfg.Tenants[0].ID != "alpha" {
		t.Fatalf("tenant catalogs = %#v", cfg.Tenants)
	}
	if cfg.Tenants[0].PluginDirs[0] != filepath.Join(dir, "tenants", "alpha", "plugins") {
		t.Fatalf("alpha plugin dirs = %#v", cfg.Tenants[0].PluginDirs)
	}
}

func TestLoadRuntimeConfigRejectsStaleTokenAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.toml")
	writeFile(t, path, `[[kernel]]
id = "local"
url = "unix:///tmp/runtime.sock"
token = "/tmp/runtime-token"
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported field") || !strings.Contains(err.Error(), "kernel.token") {
		t.Fatalf("expected unsupported token field error, got %v", err)
	}
}

func TestLoadRuntimeConfigRejectsPluralKernelAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.toml")
	writeFile(t, path, `[[kernels]]
id = "local"
url = "unix:///tmp/runtime.sock"
token_file = "/tmp/runtime-token"
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported field") || !strings.Contains(err.Error(), "kernels") {
		t.Fatalf("expected unsupported plural kernels field error, got %v", err)
	}
}

func TestLoadRuntimeConfigRejectsKernelSideRuntimeRegistryShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.toml")
	writeFile(t, path, `[[runtime]]
id = "local"
backend = "local"
token_file = "/tmp/runtime-token"
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported field") || !strings.Contains(err.Error(), "runtime") {
		t.Fatalf("expected unsupported kernel-side runtime registry field error, got %v", err)
	}
}

func TestLoadRuntimeConfigRejectsUnknownTopLevelFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.toml")
	writeFile(t, path, `runtime_id = "local"

[[kernel]]
id = "main"
url = "unix:///tmp/runtime.sock"
token_file = "/tmp/runtime-token"
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported field") || !strings.Contains(err.Error(), "runtime_id") {
		t.Fatalf("expected unsupported runtime_id field error, got %v", err)
	}
}

func TestSingleKernelRejectsMultiKernelForM2(t *testing.T) {
	cfg := Config{Kernels: []Kernel{{ID: "a", URL: "unix:///a", TokenFile: "/a"}, {ID: "b", URL: "unix:///b", TokenFile: "/b"}}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	_, err := cfg.SingleKernel()
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected single-kernel error, got %v", err)
	}
}

func TestSaveRoundTripsRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config", "runtime.toml")
	cfg := Config{
		Kernels:    []Kernel{{ID: "main", URL: "unix:///tmp/runtime.sock", TokenFile: "/tmp/runtime-token", Tenants: []string{"alpha"}}},
		PluginDirs: []string{"/tmp/plugins", "/tmp/extra-plugins"},
		SkillDirs:  []string{"/tmp/skills"},
		Pool:       Pool{ColdWorkersPerTenantMax: 8, Tenants: map[string]TenantPool{"alpha": {ColdWorkersMax: 3}}},
		Runtimes:   map[string]Runtime{"python": {Command: []string{"/opt/tabula/python", "-I"}}},
		PluginKinds: map[string]PluginKind{
			"gateway": {DependsOn: []string{"driver"}},
		},
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(loaded.Kernels, cfg.Kernels) {
		t.Fatalf("kernels = %#v, want %#v", loaded.Kernels, cfg.Kernels)
	}
	if len(loaded.PluginDirs) != 2 || loaded.PluginDirs[0] != filepath.Clean("/tmp/plugins") || loaded.PluginDirs[1] != filepath.Clean("/tmp/extra-plugins") {
		t.Fatalf("plugin dirs = %#v", loaded.PluginDirs)
	}
	if len(loaded.SkillDirs) != 1 || loaded.SkillDirs[0] != filepath.Clean("/tmp/skills") {
		t.Fatalf("skill dirs = %#v", loaded.SkillDirs)
	}
	if loaded.Pool.ColdWorkersPerTenantMax != 8 {
		t.Fatalf("pool = %#v", loaded.Pool)
	}
	if loaded.Pool.Tenants["alpha"].ColdWorkersMax != 3 {
		t.Fatalf("pool tenant overrides = %#v", loaded.Pool.Tenants)
	}
	if !reflect.DeepEqual(loaded.PluginKinds, cfg.PluginKinds) {
		t.Fatalf("plugin kind policies = %#v, want %#v", loaded.PluginKinds, cfg.PluginKinds)
	}
	if !reflect.DeepEqual(loaded.Runtimes, cfg.Runtimes) {
		t.Fatalf("runtimes = %#v, want %#v", loaded.Runtimes, cfg.Runtimes)
	}
}

func TestLoadRuntimeConfigRejectsInvalidColdWorkerLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.toml")
	writeFile(t, path, `plugin_dirs = ["/tmp/plugins"]

[pool]
cold_workers_per_tenant_max = 0

[[kernel]]
id = "main"
url = "unix:///tmp/runtime.sock"
token_file = "/tmp/runtime-token"
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "cold_workers_per_tenant_max") {
		t.Fatalf("expected cold worker limit error, got %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

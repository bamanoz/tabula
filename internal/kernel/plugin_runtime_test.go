package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/kernel/plugin"
)

func TestRegisterPluginSpawnsAndRegistersHandle(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), map[string]string{"skill_tool": "echo ok"}, 3, 5, nil)
	fake := &fakePluginRuntime{}
	hub.pluginRuntime = fake
	manifest := &plugin.Manifest{
		ID:      "hello",
		Name:    "Hello",
		Version: "1.0.0",
		Runtime: "python",
		Entry:   "run.py",
		Config:  map[string]any{"default": "yes"},
	}

	if err := hub.RegisterPlugin(manifest, map[string]any{"override": true}); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	defer stopPluginRun(t, hub, "hello")

	if fake.spawned != 1 {
		t.Fatalf("spawn count: %d", fake.spawned)
	}
	if fake.lastOptions.MaxProtocolVersion != MaxPluginProtocolVersion {
		t.Fatalf("protocol max version: %d", fake.lastOptions.MaxProtocolVersion)
	}
	if fake.lastOptions.MinProtocolVersion != MinPluginProtocolVersion {
		t.Fatalf("protocol min version: %d", fake.lastOptions.MinProtocolVersion)
	}
	if fake.lastConfig["override"] != true {
		t.Fatalf("config not passed to runtime: %#v", fake.lastConfig)
	}
	registered := hub.plugins.Get("hello")
	if registered == nil || !registered.IsRegistered() {
		t.Fatalf("plugin not registered: %#v", registered)
	}
	entry, ok := hub.toolExec["hello_ping"]
	if !ok || entry.Source != toolSourcePlugin || entry.Plugin != registered {
		t.Fatalf("plugin tool dispatch not installed: ok=%v entry=%+v", ok, entry)
	}
	if entries := hub.hooks.entries("before_tool_call"); len(entries) != 1 || entries[0].sub.Name() != "hello" {
		t.Fatalf("plugin hook subscriber not indexed: %+v", entries)
	}
}

func TestNewHubDoesNotEagerlyCreateLegacyPluginRuntime(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	if hub.pluginRuntime != nil {
		t.Fatal("expected legacy plugin runtime to remain nil until explicitly used")
	}
}

func TestRegisterPluginFailureDoesNotMutateRegistry(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	hub.pluginRuntime = &fakePluginRuntime{err: plugin.NonRestartable(errors.New("register timeout"))}
	manifest := &plugin.Manifest{ID: "slow", Name: "Slow", Version: "1.0.0", Runtime: "python", Entry: "run.py"}

	err := hub.RegisterPlugin(manifest, nil)
	if err == nil || !strings.Contains(err.Error(), "register timeout") {
		t.Fatalf("expected register timeout error, got %v", err)
	}
	if hub.plugins.Len() != 0 {
		t.Fatalf("registry mutated after failed register: %d", hub.plugins.Len())
	}
	if len(hub.toolExec) != 0 {
		t.Fatalf("tool dispatch mutated after failed register: %#v", hub.toolExec)
	}
}

func TestRegisterPluginInvalidCatalogFromRuntimeDoesNotMutateRegistry(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	hub.pluginRuntime = fakeInvalidPluginRuntime{}
	manifest := &plugin.Manifest{ID: "bad", Name: "Bad", Version: "1.0.0", Runtime: "python", Entry: "run.py"}

	err := hub.RegisterPlugin(manifest, nil)
	if err == nil || !strings.Contains(err.Error(), "register rejected") {
		t.Fatalf("expected register rejected error, got %v", err)
	}
	if hub.plugins.Len() != 0 {
		t.Fatalf("registry mutated after invalid register: %d", hub.plugins.Len())
	}
	if len(hub.toolExec) != 0 {
		t.Fatalf("tool dispatch mutated after invalid register: %#v", hub.toolExec)
	}
}

func TestHandlePluginExitRemovesCurrentHandleOnly(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	old := registeredTestPlugin(t, "hello", "old_tool")
	newHandle := registeredTestPlugin(t, "hello", "new_tool")
	hub.registerPluginHandle(old)
	hub.registerPluginHandle(newHandle)

	hub.handlePluginExit(old, errors.New("old process exited late"))

	if got := hub.plugins.Get("hello"); got != newHandle {
		t.Fatalf("late exit removed replacement handle: got=%p want=%p", got, newHandle)
	}
	if _, ok := hub.toolExec["new_tool"]; !ok {
		t.Fatal("replacement plugin tool removed by stale exit")
	}
}

func TestLoadPluginsContinuesAfterManifestFailure(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	fake := &fakePluginRuntime{}
	hub.pluginRuntime = fake
	root := t.TempDir()
	validDir := filepath.Join(root, "valid")
	if err := os.Mkdir(validDir, 0o755); err != nil {
		t.Fatalf("mkdir valid plugin: %v", err)
	}
	writePluginToml(t, validDir, "good")

	err := hub.LoadPlugins([]plugin.BootEntry{
		{ManifestPath: filepath.Join(root, "missing")},
		{ManifestPath: validDir, Config: map[string]any{"enabled": true}},
	})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected joined error for missing manifest, got %v", err)
	}
	if fake.spawned != 1 {
		t.Fatalf("valid plugin should still be spawned, count=%d", fake.spawned)
	}
	if hub.plugins.Get("good") == nil {
		t.Fatal("valid plugin not registered after previous failure")
	}
}

type fakePluginRuntime struct {
	mu          sync.Mutex
	spawned     int
	err         error
	exitErrors  []error
	exitDelay   time.Duration
	lastConfig  map[string]any
	lastOptions plugin.SpawnOptions
}

type fakeInvalidPluginRuntime struct{}

func (fakeInvalidPluginRuntime) Spawn(ctx context.Context, manifest *plugin.Manifest, config map[string]any, opts plugin.SpawnOptions) (*plugin.Handle, error) {
	h := plugin.NewHandle(manifest.ID, nil)
	h.MarkAlive()
	if err := h.MarkRegistered(&plugin.RegisterParams{
		ProtocolVersion: opts.MaxProtocolVersion,
		PluginID:        manifest.ID,
		Tools:           []plugin.ToolSpec{{Name: "bad_tool"}},
		Subscriptions:   []plugin.SubscriptionSpec{{Event: "unsupported_event"}},
	}); err != nil {
		return nil, err
	}
	return h, nil
}

func (r *fakePluginRuntime) Spawn(ctx context.Context, manifest *plugin.Manifest, config map[string]any, opts plugin.SpawnOptions) (*plugin.Handle, error) {
	r.mu.Lock()
	idx := r.spawned
	r.spawned++
	r.lastConfig = config
	r.lastOptions = opts
	var exitErr error
	if idx < len(r.exitErrors) {
		exitErr = r.exitErrors[idx]
	}
	err := r.err
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	h := registeredTestPluginFromManifest(manifest, manifest.ID+"_ping")
	if exitErr != nil {
		delay := r.exitDelay
		go func() {
			if delay > 0 {
				time.Sleep(delay)
			}
			opts.OnExit(h, exitErr)
		}()
	} else {
		go func() {
			<-ctx.Done()
			if opts.OnExit != nil {
				opts.OnExit(h, ctx.Err())
			}
		}()
	}
	return h, nil
}

func TestRegisterPluginPassesProcessSupervisorCallback(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	fake := &fakePluginRuntime{}
	hub.pluginRuntime = fake
	manifest := &plugin.Manifest{ID: "diag", Name: "Diag", Version: "1.0.0", Runtime: "python", Entry: "run.py"}

	if err := hub.RegisterPlugin(manifest, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	defer stopPluginRun(t, hub, "diag")
	if fake.lastOptions.OnStartProcess == nil {
		t.Fatal("OnStartProcess callback not wired into runtime options")
	}
}

func TestHandlePluginProcessStartRegistersProcessSupervisor(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	handle := registeredTestPlugin(t, "diag", "diag_tool")
	cmd := &exec.Cmd{Process: &os.Process{Pid: 424242}}

	hub.handlePluginProcessStart(handle, cmd)

	proc, ok := hub.processes.ByPID(424242)
	if !ok {
		t.Fatal("plugin process not registered with ProcessSupervisor")
	}
	if proc.Command != "plugin:diag" || proc.Session != "" || !proc.Alive {
		t.Fatalf("unexpected process snapshot: %+v", proc)
	}
}

func TestHandlePluginExitMarksProcessSupervisorExited(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	handle := registeredTestPlugin(t, "diag", "diag_tool")
	handle.SetPID(434343)
	cmd := &exec.Cmd{Process: &os.Process{Pid: 434343}}
	hub.processes.Register(cmd, "plugin:diag", "")
	hub.registerPluginHandle(handle)

	hub.handlePluginExit(handle, errors.New("done"))

	proc, ok := hub.processes.ByPID(434343)
	if !ok {
		t.Fatal("process missing after exit mark")
	}
	if proc.Alive {
		t.Fatalf("plugin process still marked alive: %+v", proc)
	}
}

func (r *fakePluginRuntime) spawnCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.spawned
}

func registeredTestPlugin(t *testing.T, id, tool string) *plugin.Handle {
	t.Helper()
	return registeredTestPluginFromManifest(&plugin.Manifest{ID: id, Name: id, Version: "1.0.0", Runtime: "python", Entry: "run.py"}, tool)
}

func registeredTestPluginFromManifest(manifest *plugin.Manifest, tool string) *plugin.Handle {
	h := plugin.NewHandle(manifest.ID, nil)
	h.MarkAlive()
	_ = h.MarkRegistered(&plugin.RegisterParams{
		ProtocolVersion: MaxPluginProtocolVersion,
		PluginID:        manifest.ID,
		Tools:           []plugin.ToolSpec{{Name: tool}},
		Subscriptions:   []plugin.SubscriptionSpec{{Event: "before_tool_call", Priority: 10}},
	})
	return h
}

func writePluginToml(t *testing.T, dir, id string) {
	t.Helper()
	content := `id = "` + id + `"
name = "` + id + `"
version = "1.0.0"
runtime = "python"
entry = "run.py"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=0.1.0,<0.2.0"
`
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write plugin.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("write run.py: %v", err)
	}
}

func TestRegisterPluginSupervisorRestartsAndReinstallsHandle(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	fake := &fakePluginRuntime{exitErrors: []error{errors.New("crash")}, exitDelay: 10 * time.Millisecond}
	hub.pluginRuntime = fake
	hub.pluginSupervisorPolicy = plugin.SupervisorPolicy{
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		MaxRestarts:    2,
		RestartWindow:  time.Second,
		CleanRunReset:  time.Hour,
	}
	manifest := &plugin.Manifest{ID: "restarty", Name: "Restarty", Version: "1.0.0", Runtime: "python", Entry: "run.py"}

	if err := hub.RegisterPlugin(manifest, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	first := hub.plugins.Get("restarty")
	if first == nil {
		t.Fatal("first handle not installed")
	}

	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		if fake.spawnCount() >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if fake.spawnCount() < 2 {
		t.Fatalf("supervisor did not restart plugin, spawn count=%d", fake.spawnCount())
	}
	restarted := hub.plugins.Get("restarty")
	if restarted == nil || restarted == first {
		t.Fatalf("restart did not reinstall fresh handle: first=%p restarted=%p", first, restarted)
	}
	if entry := hub.toolExec["restarty_ping"]; entry.Plugin != restarted {
		t.Fatalf("tool dispatch not updated to restarted handle: %+v", entry)
	}

	stopPluginRun(t, hub, "restarty")
}

func stopPluginRun(t *testing.T, hub *Hub, id string) {
	t.Helper()
	hub.pluginRunsMu.Lock()
	run := hub.pluginRuns[id]
	hub.pluginRunsMu.Unlock()
	if run != nil {
		run.cancel()
		<-run.done
	}
}

func TestReloadPluginsStopsExistingAndStartsFresh(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	fake := &fakePluginRuntime{}
	hub.pluginRuntime = fake
	root := t.TempDir()
	pluginA := filepath.Join(root, "alpha")
	pluginB := filepath.Join(root, "beta")
	if err := os.Mkdir(pluginA, 0o755); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	if err := os.Mkdir(pluginB, 0o755); err != nil {
		t.Fatalf("mkdir beta: %v", err)
	}
	writePluginToml(t, pluginA, "alpha")
	writePluginToml(t, pluginB, "beta")

	if err := hub.LoadPlugins([]plugin.BootEntry{{ManifestPath: pluginA}}); err != nil {
		t.Fatalf("initial LoadPlugins: %v", err)
	}
	if hub.plugins.Get("alpha") == nil {
		t.Fatal("alpha not registered after initial load")
	}

	if err := hub.ReloadPlugins([]plugin.BootEntry{{ManifestPath: pluginB}}); err != nil {
		t.Fatalf("ReloadPlugins: %v", err)
	}
	defer stopPluginRun(t, hub, "beta")

	if hub.plugins.Get("alpha") != nil {
		t.Fatal("alpha should have been stopped during reload")
	}
	if hub.plugins.Get("beta") == nil {
		t.Fatal("beta not registered after reload")
	}
	hub.pluginRunsMu.Lock()
	if _, ok := hub.pluginRuns["alpha"]; ok {
		hub.pluginRunsMu.Unlock()
		t.Fatal("alpha lifecycle still tracked after reload")
	}
	hub.pluginRunsMu.Unlock()
	if got := fake.spawnCount(); got != 2 {
		t.Fatalf("expected 2 spawns (initial + reload), got %d", got)
	}
}

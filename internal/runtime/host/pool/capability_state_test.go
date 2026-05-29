package pool

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

func TestCapabilityStateSeedsManifestCapabilities(t *testing.T) {
	state, _ := testCapabilityState(t)

	caps := state.list()
	if len(caps) != 1 {
		t.Fatalf("capabilities length = %d, want 1: %#v", len(caps), caps)
	}
	if caps[0].Target.ID != "fs" || caps[0].State != wire.CapabilityStateManifestLoaded || caps[0].Source != wire.CapabilitySourceManifest {
		t.Fatalf("capability = %#v", caps[0])
	}
	if !state.hasTool("tenant-a", "fs", "echo") || state.hasTool("tenant-a", "fs", "missing") {
		t.Fatalf("unexpected tool lookup results")
	}
}

func TestCapabilityStateMarkReadyClonesWorkerMetadata(t *testing.T) {
	state, plugin := testCapabilityState(t)
	ack := workerwire.WorkerInitAck{
		Tools:         []wire.ToolSpec{{Name: "zeta"}, {Name: "alpha"}},
		Subscriptions: []wire.HookSpec{{Event: "before_tool_call", Priority: 100}},
	}

	capability := state.markReady("tenant-a", plugin, ack)
	ack.Tools[0].Name = "mutated"
	ack.Subscriptions[0].Event = "mutated"

	if capability.State != wire.CapabilityStateReady || capability.Source != wire.CapabilitySourceWorker || capability.Revision != 2 {
		t.Fatalf("ready capability metadata = %#v", capability)
	}
	if len(capability.Tools) != 2 || capability.Tools[0].Name != "alpha" || capability.Tools[1].Name != "zeta" {
		t.Fatalf("ready tools should be cloned and sorted: %#v", capability.Tools)
	}
	snapshot, ok := state.readySnapshot("tenant-a", "fs")
	if !ok || len(snapshot.Hooks) != 1 || snapshot.Hooks[0].Event != "before_tool_call" {
		t.Fatalf("ready snapshot = %#v ok=%v", snapshot, ok)
	}
}

func TestCapabilityStateApplyToolsUpdatedDeduplicatesSameRevision(t *testing.T) {
	state, plugin := testCapabilityState(t)
	state.markReady("tenant-a", plugin, workerwire.WorkerInitAck{Tools: []wire.ToolSpec{{Name: "echo"}}})

	capability, ok := state.applyToolsUpdated("tenant-a", plugin, workerwire.WorkerToolsUpdated{Revision: 7, Tools: []wire.ToolSpec{{Name: "dynamic"}}, Removed: []string{"echo"}})
	if !ok || capability.Revision != 7 || len(capability.Tools) != 1 || capability.Tools[0].Name != "dynamic" {
		t.Fatalf("first update = %#v ok=%v", capability, ok)
	}
	_, ok = state.applyToolsUpdated("tenant-a", plugin, workerwire.WorkerToolsUpdated{Revision: 7, Tools: []wire.ToolSpec{{Name: "dynamic"}}})
	if ok {
		t.Fatal("same revision and same tools should not publish a new capability")
	}
}

func TestCapabilityStateResetTenantTargetKeepsOtherTenant(t *testing.T) {
	alphaDir := t.TempDir()
	betaDir := t.TempDir()
	writePoolPlugin(t, filepath.Join(alphaDir, "fs", "plugin.toml"), "fs", "alpha_read")
	writePoolPlugin(t, filepath.Join(betaDir, "fs", "plugin.toml"), "fs", "beta_read")
	store, err := manifest.NewTenantStore(map[string]manifest.SearchDirs{
		"alpha": {PluginDirs: []string{alphaDir}},
		"beta":  {PluginDirs: []string{betaDir}},
	})
	if err != nil {
		t.Fatalf("NewTenantStore: %v", err)
	}
	state := newCapabilityState(store)
	alphaPlugin, ok := store.GetForTenant("alpha", "fs")
	if !ok {
		t.Fatal("alpha plugin missing")
	}
	state.markReady("alpha", alphaPlugin, workerwire.WorkerInitAck{Tools: []wire.ToolSpec{{Name: "runtime_alpha", Schema: json.RawMessage(`{"type":"object"}`)}}})

	state.reset(&wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, "beta")

	alphaSnapshot, ok := state.readySnapshot("alpha", "fs")
	if !ok || len(alphaSnapshot.Tools) != 1 || alphaSnapshot.Tools[0].Name != "runtime_alpha" {
		t.Fatalf("alpha ready snapshot should survive beta reset: %#v ok=%v", alphaSnapshot, ok)
	}
	if !state.hasTool("beta", "fs", "beta_read") {
		t.Fatalf("beta manifest tool should be restored after reset")
	}
}

func testCapabilityState(t *testing.T) (*capabilityState, manifest.Plugin) {
	t.Helper()
	store, err := manifest.NewStore([]string{testPluginDir(t)})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	plugin, ok := store.GetForTenant("tenant-a", "fs")
	if !ok {
		t.Fatal("plugin missing")
	}
	return newCapabilityState(store), plugin
}

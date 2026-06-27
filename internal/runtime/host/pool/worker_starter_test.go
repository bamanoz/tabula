package pool

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy"
	runtimewire "github.com/bamanoz/tabula/internal/runtime/wire"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

func TestWorkerStarterReturnsExistingLiveWorker(t *testing.T) {
	starter, fake, plugin := testWorkerStarter(t)
	existing := newFakeWorker()
	entry := &entry{worker: existing}

	worker, reused, err := starter.ensure(context.Background(), entry, plugin, "tenant-a")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if worker != existing || !reused {
		t.Fatalf("ensure returned worker=%#v reused=%v, want existing true", worker, reused)
	}
	if got := fake.spawnCount.Load(); got != 0 {
		t.Fatalf("spawn count = %d, want 0", got)
	}
}

func TestWorkerStarterSpawnsInitializesAndMarksReady(t *testing.T) {
	starter, fake, plugin := testWorkerStarter(t)
	registryEntry := &entry{}
	var initialized, ready, watched bool
	starter.onInitializing = func(tenantID string, got manifest.Plugin) {
		initialized = tenantID == "tenant-a" && got.ID == plugin.ID
	}
	starter.onReady = func(tenantID string, got manifest.Plugin, ack workerwire.WorkerInitAck) {
		ready = tenantID == "tenant-a" && got.ID == plugin.ID && len(ack.Tools) == 2
	}
	starter.onWatch = func(tenantID string, got manifest.Plugin, e *entry, worker policy.Worker) {
		watched = tenantID == "tenant-a" && got.ID == plugin.ID && e == registryEntry && worker != nil
	}

	worker, reused, err := starter.ensure(context.Background(), registryEntry, plugin, "tenant-a")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if worker == nil || registryEntry.worker != worker || reused {
		t.Fatalf("worker=%#v entry.worker=%#v reused=%v", worker, registryEntry.worker, reused)
	}
	if !initialized || !ready || !watched {
		t.Fatalf("callbacks initialized=%v ready=%v watched=%v", initialized, ready, watched)
	}
	reqs := fake.spawnRequests()
	if len(reqs) != 1 || reqs[0].Mode != policy.SpawnModeWarm || reqs[0].Env["TABULA_TENANT_DIR"] != filepath.Join("/tmp/tabula", "tenants", "tenant-a") {
		t.Fatalf("spawn request = %#v", reqs)
	}
	if len(reqs[0].Command) != 2 || reqs[0].Command[0] != "python3" || reqs[0].Command[1] != "run.py" {
		t.Fatalf("expected canonical python worker command: %#v", reqs[0])
	}
}

func TestWorkerStarterInitFailureClearsEntryAndShutsDownWorker(t *testing.T) {
	starter, _, plugin := testWorkerStarter(t)
	w := newFakeWorker()
	w.initErr = errors.New("init failed")
	starter.policy = &fakePolicy{spawned: make(chan *fakeWorker, 1), nextWorker: w}
	entry := &entry{}
	var failed bool
	starter.onFailed = func(tenantID string, got manifest.Plugin) {
		failed = tenantID == "tenant-a" && got.ID == plugin.ID
	}

	worker, reused, err := starter.ensure(context.Background(), entry, plugin, "tenant-a")
	if err == nil {
		t.Fatal("expected init error")
	}
	if worker != nil || reused || entry.worker != nil {
		t.Fatalf("worker=%#v reused=%v entry.worker=%#v", worker, reused, entry.worker)
	}
	if !failed || !w.shutdown.Load() {
		t.Fatalf("failed callback=%v shutdown=%v", failed, w.shutdown.Load())
	}
}

func TestWorkerStarterSpawnFailureMarksFailed(t *testing.T) {
	starter, _, plugin := testWorkerStarter(t)
	starter.policy = failingPolicy{err: errors.New("spawn failed")}
	var failed bool
	starter.onFailed = func(tenantID string, got manifest.Plugin) {
		failed = tenantID == "tenant-a" && got.ID == plugin.ID
	}

	_, _, err := starter.ensure(context.Background(), &entry{}, plugin, "tenant-a")
	if err == nil {
		t.Fatal("expected spawn error")
	}
	if !failed {
		t.Fatal("spawn failure should mark target failed")
	}
}

func TestWorkerStarterSpawnReqCarriesCanonicalWorkerCommand(t *testing.T) {
	starter := newWorkerStarter("kernel", failingPolicy{}, nil)
	plugin := manifest.Plugin{
		ID:          "testbed-cold-node",
		Name:        "Testbed Cold Node",
		Version:     "0.1.0",
		Description: "Node fixture",
		Worker:      &manifest.Worker{Command: []string{"node", "scripts/run.js"}, Mode: runtimewire.WorkerModeCold},
		WorkerMode:  runtimewire.WorkerModeCold,
		Tools:       []manifest.Tool{{Name: "testbed_cold_node"}},
		Requires:    &manifest.Requires{Kernel: ">=0.9.0,<1.0.0", ProtocolVersion: 1},
		RootDir:     "/tmp/plugin",
	}
	req := starter.spawnReq("tenant-a", plugin)
	if len(req.Command) != 2 || req.Command[0] != "node" || req.Command[1] != "scripts/run.js" {
		t.Fatalf("command = %#v", req.Command)
	}
	if req.HarnessKind != runtimewire.HarnessKindNode {
		t.Fatalf("harness kind = %q, want node", req.HarnessKind)
	}
	if req.Mode != policy.SpawnModeWarm {
		t.Fatalf("mode = %q, want warm", req.Mode)
	}
}

func testWorkerStarter(t *testing.T) (*workerStarter, *fakePolicy, manifest.Plugin) {
	t.Helper()
	store, err := manifest.NewStore([]string{testPluginDir(t)})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	plugin, ok := store.GetForTenant("tenant-a", "fs")
	if !ok {
		t.Fatal("plugin missing")
	}
	fake := &fakePolicy{spawned: make(chan *fakeWorker, 1), nextWorker: newFakeWorker()}
	starter := newWorkerStarter("kernel", fake, func(tenantID string) map[string]string {
		return map[string]string{"TABULA_TENANT_DIR": filepath.Join("/tmp/tabula", "tenants", tenantID)}
	})
	return starter, fake, plugin
}

type failingPolicy struct {
	err error
}

func (p failingPolicy) Spawn(context.Context, policy.SpawnReq) (policy.Worker, error) {
	return nil, p.err
}

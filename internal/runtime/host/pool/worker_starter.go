package pool

import (
	"context"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

type workerStarter struct {
	kernelID string
	policy   policy.PluginExecPolicy
	env      func(string) map[string]string

	onInitializing func(string, manifest.Plugin)
	onFailed       func(string, manifest.Plugin)
	onReady        func(string, manifest.Plugin, workerwire.WorkerInitAck)
	onWatch        func(string, manifest.Plugin, *entry, policy.Worker)
}

func newWorkerStarter(kernelID string, pol policy.PluginExecPolicy, env func(string) map[string]string) *workerStarter {
	return &workerStarter{kernelID: kernelID, policy: pol, env: env}
}

func (s *workerStarter) ensure(ctx context.Context, e *entry, plugin manifest.Plugin, tenantID string) (policy.Worker, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if e.worker != nil && e.worker.IsAlive() {
		return e.worker, true, nil
	}
	if e.worker != nil {
		e.worker = nil
		e.noteWarmWorkerFailureLocked(time.Now())
	}
	if err := e.warmWorkerBackoffErrLocked(time.Now()); err != nil {
		return nil, false, err
	}
	s.markInitializing(tenantID, plugin)
	worker, err := s.policy.Spawn(ctx, s.spawnReq(tenantID, plugin))
	if err != nil {
		e.noteWarmWorkerFailureLocked(time.Now())
		s.markFailed(tenantID, plugin)
		return nil, false, err
	}
	e.worker = worker
	s.watch(tenantID, plugin, e, worker)
	ack, err := worker.Init(ctx, workerwire.WorkerInit{KernelID: s.kernelID, TenantID: tenantID, TargetID: plugin.ID, Manifest: plugin.RawJSON()})
	if err != nil {
		if e.worker == worker {
			e.worker = nil
		}
		e.noteWarmWorkerFailureLocked(time.Now())
		s.markFailed(tenantID, plugin)
		_ = worker.Shutdown(context.Background())
		return nil, false, err
	}
	e.noteWarmWorkerStartedLocked()
	s.markReady(tenantID, plugin, ack)
	return worker, false, nil
}

func (s *workerStarter) spawnReq(tenantID string, plugin manifest.Plugin) policy.SpawnReq {
	req := policy.SpawnReq{
		KernelID:    s.kernelID,
		TenantID:    tenantID,
		TargetID:    plugin.ID,
		HarnessKind: plugin.Capability().HarnessKind,
		Command:     plugin.LaunchCommand(),
		Runtime:     plugin.Runtime,
		Entry:       plugin.Entry,
		Manifest:    plugin.RawJSON(),
		WorkingDir:  plugin.RootDir,
		Mode:        policy.SpawnModeWarm,
	}
	if s.env != nil {
		req.Env = s.env(tenantID)
	}
	return req
}

func (s *workerStarter) markInitializing(tenantID string, plugin manifest.Plugin) {
	if s != nil && s.onInitializing != nil {
		s.onInitializing(tenantID, plugin)
	}
}

func (s *workerStarter) markFailed(tenantID string, plugin manifest.Plugin) {
	if s != nil && s.onFailed != nil {
		s.onFailed(tenantID, plugin)
	}
}

func (s *workerStarter) markReady(tenantID string, plugin manifest.Plugin, ack workerwire.WorkerInitAck) {
	if s != nil && s.onReady != nil {
		s.onReady(tenantID, plugin, ack)
	}
}

func (s *workerStarter) watch(tenantID string, plugin manifest.Plugin, e *entry, worker policy.Worker) {
	if s != nil && s.onWatch != nil {
		s.onWatch(tenantID, plugin, e, worker)
	}
}

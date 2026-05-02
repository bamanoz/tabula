// Package pool manages warm runtime workers keyed by kernel, tenant, and target.
package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/bamanoz/tabula/cmd/tabula-runtime/manifest"
	"github.com/bamanoz/tabula/cmd/tabula-runtime/policy"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

// Pool lazily spawns and reuses warm workers.
type Pool struct {
	kernelID string
	store    *manifest.Store
	policy   policy.PluginExecPolicy

	mu      sync.Mutex
	entries map[key]*entry
}

type key struct {
	kernelID string
	tenantID string
	targetID string
}

type entry struct {
	mu     sync.Mutex
	worker policy.Worker
}

// New creates a worker pool.
func New(kernelID string, store *manifest.Store, pol policy.PluginExecPolicy) *Pool {
	return &Pool{kernelID: kernelID, store: store, policy: pol, entries: map[key]*entry{}}
}

// Invoke routes one Runtime API invoke to the correct warm worker.
func (p *Pool) Invoke(ctx context.Context, in wire.Invoke) (wire.InvokeResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil || p.policy == nil || p.store == nil {
		return failed(in.CallID, wire.ErrorInternal, "worker pool is not configured"), nil
	}
	if in.Target.Kind != wire.TargetKindPlugin {
		return failed(in.CallID, wire.ErrorTargetUnknown, fmt.Sprintf("target %s/%s is not hosted by M2 plugin runtime", in.Target.Kind, in.Target.ID)), nil
	}
	plugin, ok := p.store.Get(in.Target.ID)
	if !ok {
		return failed(in.CallID, wire.ErrorTargetUnknown, fmt.Sprintf("target %q not found", in.Target.ID)), nil
	}
	if !plugin.HasTool(in.Tool) {
		return failed(in.CallID, wire.ErrorToolNotFound, fmt.Sprintf("tool %q not found on target %q", in.Tool, in.Target.ID)), nil
	}

	resultCh := make(chan wire.InvokeResult, 1)
	go func() {
		resultCh <- p.invokeLocked(plugin, in)
	}()

	select {
	case result := <-resultCh:
		return result, nil
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return failed(in.CallID, wire.ErrorTimeout, "invoke timed out"), nil
		}
		return failed(in.CallID, wire.ErrorCancelled, "invoke cancelled"), nil
	}
}

func (p *Pool) invokeLocked(plugin manifest.Plugin, in wire.Invoke) wire.InvokeResult {
	k := key{kernelID: p.kernelID, tenantID: in.TenantID, targetID: in.Target.ID}
	e := p.entryFor(k)
	e.mu.Lock()
	defer e.mu.Unlock()

	worker, err := p.ensureWorker(e, plugin, in.TenantID)
	if err != nil {
		return failed(in.CallID, wire.ErrorInternal, err.Error())
	}
	result, err := worker.Call(context.Background(), workerwire.WorkerCall{CallID: in.CallID, Tool: in.Tool, Args: in.Args})
	if err != nil {
		e.worker = nil
		_ = worker.Shutdown(context.Background())
		return failed(in.CallID, wire.ErrorInternal, fmt.Sprintf("worker call failed: %v", err))
	}
	if !result.OK {
		message := "worker call failed"
		if result.Error != nil && result.Error.Message != "" {
			message = result.Error.Message
		}
		return failed(in.CallID, wire.ErrorInternal, message)
	}
	return wire.InvokeResult{Op: wire.OpInvokeResult, CallID: in.CallID, OK: true, Data: result.Data}
}

func (p *Pool) ensureWorker(e *entry, plugin manifest.Plugin, tenantID string) (policy.Worker, error) {
	if e.worker != nil && e.worker.IsAlive() {
		return e.worker, nil
	}
	req := policy.SpawnReq{
		KernelID:   p.kernelID,
		TenantID:   tenantID,
		TargetID:   plugin.ID,
		Runtime:    plugin.Runtime,
		Entry:      plugin.Entry,
		Manifest:   plugin.RawJSON(),
		WorkingDir: plugin.RootDir,
		Mode:       policy.SpawnModeWarm,
	}
	worker, err := p.policy.Spawn(context.Background(), req)
	if err != nil {
		return nil, err
	}
	if err := worker.Init(context.Background(), workerwire.WorkerInit{KernelID: p.kernelID, TenantID: tenantID, TargetID: plugin.ID, Manifest: plugin.RawJSON()}); err != nil {
		_ = worker.Shutdown(context.Background())
		return nil, err
	}
	e.worker = worker
	return worker, nil
}

func (p *Pool) entryFor(k key) *entry {
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing := p.entries[k]; existing != nil {
		return existing
	}
	e := &entry{}
	p.entries[k] = e
	return e
}

// Reload evicts matching workers and returns their Runtime API targets.
func (p *Pool) Reload(target *wire.Target) []wire.Target {
	if p == nil {
		return nil
	}
	var evicted []struct {
		target wire.Target
		entry  *entry
	}
	p.mu.Lock()
	for k, e := range p.entries {
		if target != nil && (target.Kind != wire.TargetKindPlugin || target.ID != k.targetID) {
			continue
		}
		delete(p.entries, k)
		evicted = append(evicted, struct {
			target wire.Target
			entry  *entry
		}{target: wire.Target{Kind: wire.TargetKindPlugin, ID: k.targetID}, entry: e})
	}
	p.mu.Unlock()
	out := make([]wire.Target, 0, len(evicted))
	for _, item := range evicted {
		item.entry.mu.Lock()
		if item.entry.worker != nil {
			_ = item.entry.worker.Shutdown(context.Background())
		}
		item.entry.mu.Unlock()
		out = append(out, item.target)
	}
	return out
}

// WorkerCount returns the number of live workers currently tracked.
func (p *Pool) WorkerCount() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	entries := make([]*entry, 0, len(p.entries))
	for _, e := range p.entries {
		entries = append(entries, e)
	}
	p.mu.Unlock()
	count := 0
	for _, e := range entries {
		e.mu.Lock()
		if e.worker != nil && e.worker.IsAlive() {
			count++
		}
		e.mu.Unlock()
	}
	return count
}

// Close shuts down every tracked worker.
func (p *Pool) Close() {
	_ = p.Reload(nil)
}

func failed(callID string, code wire.ErrorCode, message string) wire.InvokeResult {
	return wire.InvokeResult{Op: wire.OpInvokeResult, CallID: callID, OK: false, Error: &wire.Error{Code: code, Retryable: code == wire.ErrorRuntimeBusy || code == wire.ErrorRuntimeUnavailable, Message: message}}
}

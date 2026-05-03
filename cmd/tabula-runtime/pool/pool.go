// Package pool manages warm runtime workers keyed by kernel, tenant, and target.
package pool

import (
	"context"
	"errors"
	"fmt"
	"sort"
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

	targetMu sync.RWMutex
	targets  map[string]wire.Capability
	async    chan any
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
	p := &Pool{kernelID: kernelID, store: store, policy: pol, entries: map[key]*entry{}, targets: map[string]wire.Capability{}, async: make(chan any, 128)}
	p.resetTargets(nil)
	return p
}

// AsyncFrames returns runtime-originated async frames synthesized from worker
// events and target-state transitions.
func (p *Pool) AsyncFrames() <-chan any {
	if p == nil {
		return nil
	}
	return p.async
}

// PrimeTargets proactively initializes manifest-backed targets so runtime
// attachments can publish authoritative catalog/hook state before first invoke.
// Re-priming an already-ready live worker republishes its current catalog state
// so a newly attached kernel can rebuild runtime-backed dispatch without
// waiting for a later mutation.
func (p *Pool) PrimeTargets(ctx context.Context, target *wire.Target) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil || p.store == nil {
		return
	}
	for _, capability := range p.store.Capabilities() {
		if target != nil {
			if target.Kind != wire.TargetKindPlugin || target.ID != capability.Target.ID {
				continue
			}
		}
		plugin, ok := p.store.Get(capability.Target.ID)
		if !ok {
			continue
		}
		p.primeTarget(ctx, plugin)
	}
}

// Capabilities returns the current runtime capability view in stable target-id
// order. Manifest metadata seeds the initial state; worker init acknowledgements
// upgrade targets to authoritative ready metadata.
func (p *Pool) Capabilities() []wire.Capability {
	if p == nil {
		return nil
	}
	p.targetMu.RLock()
	defer p.targetMu.RUnlock()
	ids := make([]string, 0, len(p.targets))
	for id := range p.targets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]wire.Capability, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneCapability(p.targets[id]))
	}
	return out
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

// HookEvent routes one Runtime API hook event to the correct warm worker.
// A nil reply means the event was fire-and-forget and did not expect a reply.
func (p *Pool) HookEvent(ctx context.Context, in wire.HookEvent) (*wire.HookEventReply, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil || p.policy == nil || p.store == nil {
		return nil, fmt.Errorf("worker pool is not configured")
	}
	if in.Target.Kind != wire.TargetKindPlugin {
		return nil, fmt.Errorf("target %s/%s is not hosted by M2 plugin runtime", in.Target.Kind, in.Target.ID)
	}
	plugin, ok := p.store.Get(in.Target.ID)
	if !ok {
		return nil, fmt.Errorf("target %q not found", in.Target.ID)
	}
	k := key{kernelID: p.kernelID, tenantID: "default", targetID: in.Target.ID}
	e := p.entryFor(k)
	e.mu.Lock()
	defer e.mu.Unlock()
	worker, _, err := p.ensureWorker(ctx, e, plugin, "default")
	if err != nil {
		return nil, err
	}
	reply, err := worker.HookEvent(ctx, workerwire.WorkerEvent{
		CallID:    in.CallID,
		Event:     in.Event,
		ReplyMode: workerwire.ReplyMode(in.ReplyMode),
		SessionID: in.SessionID,
		Data:      in.Data,
	})
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, nil
	}
	return &wire.HookEventReply{Op: wire.OpHookEventReply, CallID: reply.CallID, Action: reply.Action, Data: reply.Data, Reason: reply.Reason}, nil
}

func (p *Pool) invokeLocked(plugin manifest.Plugin, in wire.Invoke) wire.InvokeResult {
	k := key{kernelID: p.kernelID, tenantID: in.TenantID, targetID: in.Target.ID}
	e := p.entryFor(k)
	e.mu.Lock()
	defer e.mu.Unlock()

	worker, _, err := p.ensureWorker(context.Background(), e, plugin, in.TenantID)
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

func (p *Pool) ensureWorker(ctx context.Context, e *entry, plugin manifest.Plugin, tenantID string) (policy.Worker, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if e.worker != nil && e.worker.IsAlive() {
		return e.worker, true, nil
	}
	p.markTargetInitializing(plugin)
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
	worker, err := p.policy.Spawn(ctx, req)
	if err != nil {
		p.markTargetFailed(plugin)
		return nil, false, err
	}
	e.worker = worker
	p.watchWorker(plugin, e, worker)
	ack, err := worker.Init(ctx, workerwire.WorkerInit{KernelID: p.kernelID, TenantID: tenantID, TargetID: plugin.ID, Manifest: plugin.RawJSON()})
	if err != nil {
		if e.worker == worker {
			e.worker = nil
		}
		p.markTargetFailed(plugin)
		_ = worker.Shutdown(context.Background())
		return nil, false, err
	}
	p.markTargetReady(plugin, ack)
	return worker, false, nil
}

func (p *Pool) primeTarget(ctx context.Context, plugin manifest.Plugin) {
	k := key{kernelID: p.kernelID, tenantID: "default", targetID: plugin.ID}
	e := p.entryFor(k)
	e.mu.Lock()
	_, existing, err := p.ensureWorker(ctx, e, plugin, "default")
	e.mu.Unlock()
	if err != nil || !existing {
		return
	}
	p.publishTargetSnapshot(plugin.ID)
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
	seenTargets := make(map[string]struct{}, len(evicted))
	for _, item := range evicted {
		item.entry.mu.Lock()
		if item.entry.worker != nil {
			worker := item.entry.worker
			item.entry.worker = nil
			item.entry.mu.Unlock()
			if _, seen := seenTargets[item.target.ID]; !seen {
				p.publishCriticalFrame(wire.LifecycleNotice{Op: wire.OpLifecycleNotice, Target: item.target, State: wire.LifecycleStateStopping})
			}
			_ = worker.Shutdown(context.Background())
		} else {
			item.entry.mu.Unlock()
		}
		if _, seen := seenTargets[item.target.ID]; seen {
			continue
		}
		seenTargets[item.target.ID] = struct{}{}
		out = append(out, item.target)
	}
	p.resetTargets(target)
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

func (p *Pool) watchWorker(plugin manifest.Plugin, entry *entry, worker policy.Worker) {
	if p == nil || entry == nil || worker == nil {
		return
	}
	go func() {
		for event := range worker.Events() {
			if !p.isCurrentWorker(entry, worker) {
				continue
			}
			if event.Err != nil {
				p.clearCurrentWorker(entry, worker)
				p.markTargetCrashed(plugin, event.Err)
				continue
			}
			switch frame := event.Frame.(type) {
			case *workerwire.WorkerToolsUpdated:
				p.applyToolsUpdated(plugin, *frame)
			case *workerwire.WorkerEventReply:
				p.publishCriticalFrame(wire.HookEventReply{Op: wire.OpHookEventReply, CallID: frame.CallID, Action: frame.Action, Data: frame.Data, Reason: frame.Reason})
			case *workerwire.WorkerSend:
				p.publishCriticalFrame(wire.PluginSend{Op: wire.OpPluginSend, Target: wire.Target{Kind: wire.TargetKindPlugin, ID: plugin.ID}, Channel: frame.Channel, Type: frame.Type, Payload: frame.Payload, SessionID: frame.SessionID})
			case *workerwire.WorkerLog:
				p.publishBestEffortFrame(wire.PluginLog{Op: wire.OpPluginLog, Target: wire.Target{Kind: wire.TargetKindPlugin, ID: plugin.ID}, Level: frame.Level, Message: frame.Message, Fields: frame.Fields})
			case *workerwire.WorkerError:
				p.clearCurrentWorker(entry, worker)
				message := frame.Error.Code
				if frame.Error.Message != "" {
					message = frame.Error.Message
				}
				p.markTargetCrashed(plugin, errors.New(message))
			}
		}
	}()
}

func (p *Pool) isCurrentWorker(entry *entry, worker policy.Worker) bool {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.worker == worker
}

func (p *Pool) applyToolsUpdated(plugin manifest.Plugin, update workerwire.WorkerToolsUpdated) {
	p.targetMu.Lock()
	defer p.targetMu.Unlock()
	capability, ok := p.targets[plugin.ID]
	if !ok {
		capability = plugin.Capability()
	}
	if update.Revision <= capability.Revision {
		return
	}
	capability.Target = wire.Target{Kind: wire.TargetKindPlugin, ID: plugin.ID}
	capability.Tools = cloneToolSpecs(update.Tools)
	capability.State = wire.CapabilityStateReady
	capability.Source = wire.CapabilitySourceWorker
	capability.Revision = update.Revision
	capability = cloneCapability(capability)
	p.targets[plugin.ID] = capability
	p.publishCriticalFrame(wire.CatalogUpdate{Op: wire.OpCatalogUpdate, Target: capability.Target, Tools: cloneToolSpecs(capability.Tools), Hooks: cloneHookSpecs(capability.Hooks), Removed: append([]string(nil), update.Removed...), Revision: capability.Revision, State: capability.State, Source: capability.Source})
}

func (p *Pool) resetTargets(target *wire.Target) {
	if p == nil {
		return
	}
	p.targetMu.Lock()
	defer p.targetMu.Unlock()
	if target == nil {
		p.targets = map[string]wire.Capability{}
		if p.store == nil {
			return
		}
		for _, capability := range p.store.Capabilities() {
			p.targets[capability.Target.ID] = cloneCapability(capability)
		}
		return
	}
	if target.Kind != wire.TargetKindPlugin {
		delete(p.targets, target.ID)
		return
	}
	if p.store == nil {
		delete(p.targets, target.ID)
		return
	}
	if plugin, ok := p.store.Get(target.ID); ok {
		p.targets[target.ID] = cloneCapability(plugin.Capability())
		return
	}
	delete(p.targets, target.ID)
}

func (p *Pool) markTargetInitializing(plugin manifest.Plugin) {
	p.setTargetState(plugin, wire.CapabilityStateInitializing)
	p.publishCriticalFrame(wire.LifecycleNotice{Op: wire.OpLifecycleNotice, Target: wire.Target{Kind: wire.TargetKindPlugin, ID: plugin.ID}, State: wire.LifecycleStateStarting})
}

func (p *Pool) markTargetFailed(plugin manifest.Plugin) {
	p.setTargetState(plugin, wire.CapabilityStateFailed)
}

func (p *Pool) markTargetCrashed(plugin manifest.Plugin, err error) {
	p.markTargetFailed(plugin)
	message := "worker failed"
	if err != nil && err.Error() != "" {
		message = err.Error()
	}
	p.publishCriticalFrame(wire.LifecycleNotice{Op: wire.OpLifecycleNotice, Target: wire.Target{Kind: wire.TargetKindPlugin, ID: plugin.ID}, State: wire.LifecycleStateCrashed, Message: message})
}

func (p *Pool) setTargetState(plugin manifest.Plugin, state wire.CapabilityState) {
	p.targetMu.Lock()
	defer p.targetMu.Unlock()
	capability, ok := p.targets[plugin.ID]
	if !ok {
		capability = plugin.Capability()
	}
	capability.State = state
	if state == wire.CapabilityStateManifestLoaded {
		capability.Source = wire.CapabilitySourceManifest
	}
	p.targets[plugin.ID] = cloneCapability(capability)
}

func (p *Pool) publishTargetSnapshot(targetID string) {
	p.targetMu.RLock()
	capability, ok := p.targets[targetID]
	p.targetMu.RUnlock()
	if !ok || capability.State != wire.CapabilityStateReady {
		return
	}
	capability = cloneCapability(capability)
	p.publishCriticalFrame(wire.CatalogUpdate{Op: wire.OpCatalogUpdate, Target: capability.Target, Tools: cloneToolSpecs(capability.Tools), Hooks: cloneHookSpecs(capability.Hooks), Revision: capability.Revision, State: capability.State, Source: capability.Source})
	p.publishCriticalFrame(wire.LifecycleNotice{Op: wire.OpLifecycleNotice, Target: capability.Target, State: wire.LifecycleStateReady})
}

func (p *Pool) markTargetReady(plugin manifest.Plugin, ack workerwire.WorkerInitAck) {
	p.targetMu.Lock()
	defer p.targetMu.Unlock()
	capability, ok := p.targets[plugin.ID]
	if !ok {
		capability = plugin.Capability()
	}
	capability.Target = wire.Target{Kind: wire.TargetKindPlugin, ID: plugin.ID}
	capability.Tools = cloneToolSpecs(ack.Tools)
	capability.Hooks = cloneHookSpecs(ack.Subscriptions)
	capability.State = wire.CapabilityStateReady
	capability.Source = wire.CapabilitySourceWorker
	if capability.Revision < 1 {
		capability.Revision = 1
	}
	if capability.Revision == 1 {
		capability.Revision = 2
	}
	capability = cloneCapability(capability)
	p.targets[plugin.ID] = capability
	p.publishCriticalFrame(wire.CatalogUpdate{Op: wire.OpCatalogUpdate, Target: capability.Target, Tools: cloneToolSpecs(capability.Tools), Hooks: cloneHookSpecs(capability.Hooks), Revision: capability.Revision, State: capability.State, Source: capability.Source, Diagnostic: "worker ready"})
	p.publishCriticalFrame(wire.LifecycleNotice{Op: wire.OpLifecycleNotice, Target: capability.Target, State: wire.LifecycleStateReady})
}

func (p *Pool) publishCriticalFrame(frame any) {
	if p == nil || p.async == nil || frame == nil {
		return
	}
	p.async <- frame
}

func (p *Pool) publishBestEffortFrame(frame any) {
	if p == nil || p.async == nil || frame == nil {
		return
	}
	select {
	case p.async <- frame:
	default:
	}
}

func (p *Pool) clearCurrentWorker(entry *entry, worker policy.Worker) {
	if entry == nil {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.worker == worker {
		entry.worker = nil
	}
}

func cloneCapability(in wire.Capability) wire.Capability {
	out := in
	out.Tools = cloneToolSpecs(in.Tools)
	out.Hooks = cloneHookSpecs(in.Hooks)
	return out
}

func cloneToolSpecs(in []wire.ToolSpec) []wire.ToolSpec {
	if in == nil {
		return nil
	}
	out := make([]wire.ToolSpec, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func cloneHookSpecs(in []wire.HookSpec) []wire.HookSpec {
	if in == nil {
		return nil
	}
	out := make([]wire.HookSpec, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Event == out[j].Event {
			return out[i].Priority < out[j].Priority
		}
		return out[i].Event < out[j].Event
	})
	return out
}

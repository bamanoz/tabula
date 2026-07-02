// Package pool manages warm runtime workers keyed by kernel, tenant, and target.
package pool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

const defaultColdWorkersPerTenantMax = 16
const defaultColdAcquireTimeout = 30 * time.Second

var errColdWorkerBusy = errors.New("cold worker pool is busy")

// ErrTargetNotFound reports that a hook/invoke target is not present in the
// runtime's current manifest catalog.
var ErrTargetNotFound = errors.New("target not found")

type Options struct {
	ColdWorkersPerTenantMax int
	ColdAcquireTimeout      time.Duration
	ColdWorkersByTenant     map[string]int
	AllowedTenants          []string
	TabulaHome              string
	KernelURL               string
	PluginKindDependsOn     map[string][]string
}

// Pool lazily spawns and reuses warm workers.
type Pool struct {
	kernelID string
	store    *manifest.Store
	policy   policy.PluginExecPolicy
	opts     Options

	registry *workerRegistry
	cold     *coldWorkerLimiter
	starter  *workerStarter

	capabilities *capabilityState
	publisher    *asyncPublisher
	logger       *slog.Logger
}

// New creates a worker pool.
func New(kernelID string, store *manifest.Store, pol policy.PluginExecPolicy, opts ...Options) *Pool {
	resolved := Options{ColdWorkersPerTenantMax: defaultColdWorkersPerTenantMax, ColdAcquireTimeout: defaultColdAcquireTimeout}
	if len(opts) > 0 {
		if opts[0].ColdWorkersPerTenantMax > 0 {
			resolved.ColdWorkersPerTenantMax = opts[0].ColdWorkersPerTenantMax
		}
		if opts[0].ColdAcquireTimeout > 0 {
			resolved.ColdAcquireTimeout = opts[0].ColdAcquireTimeout
		}
		resolved.ColdWorkersByTenant = cloneTenantLimits(opts[0].ColdWorkersByTenant)
		resolved.AllowedTenants = append([]string(nil), opts[0].AllowedTenants...)
		resolved.TabulaHome = strings.TrimSpace(opts[0].TabulaHome)
		resolved.KernelURL = strings.TrimSpace(opts[0].KernelURL)
		resolved.PluginKindDependsOn = cloneKindDependencies(opts[0].PluginKindDependsOn)
	}
	p := &Pool{kernelID: kernelID, store: store, policy: pol, opts: resolved, registry: newWorkerRegistry(), cold: newColdWorkerLimiter(resolved.ColdWorkersPerTenantMax, resolved.ColdAcquireTimeout, resolved.ColdWorkersByTenant), capabilities: newCapabilityState(store), publisher: newAsyncPublisher(), logger: slog.Default()}
	p.starter = newWorkerStarter(kernelID, pol, p.spawnEnv)
	p.starter.onInitializing = p.markTargetInitializing
	p.starter.onFailed = p.markTargetFailed
	p.starter.onReady = p.markTargetReady
	p.starter.onWatch = p.watchWorker
	return p
}

// SetLogger configures diagnostic logging for background worker priming.
func (p *Pool) SetLogger(logger *slog.Logger) {
	if p == nil || logger == nil {
		return
	}
	p.logger = logger
}

// AsyncFrames returns runtime-originated async frames synthesized from worker
// events and target-state transitions.
func (p *Pool) AsyncFrames() <-chan any {
	if p == nil {
		return nil
	}
	return p.publisher.Frames()
}

// PrimeTargets proactively initializes manifest-backed targets so runtime
// attachments can publish authoritative catalog/hook state before first invoke.
// Re-priming an already-ready live worker republishes its current catalog state
// so a newly attached kernel can rebuild runtime-backed dispatch without
// waiting for a later mutation.
func (p *Pool) PrimeTargets(ctx context.Context, target *wire.Target) {
	p.primeTargets(ctx, target, false)
}

// PrimeRuntimeTargets proactively initializes runtime-scoped manifest-backed
// targets. It is safe at runtime attach because these targets explicitly opted
// into one shared worker per runtime instead of tenant fan-out.
func (p *Pool) PrimeRuntimeTargets(ctx context.Context, target *wire.Target) {
	p.primeTargets(ctx, target, true)
}

func (p *Pool) primeTargets(ctx context.Context, target *wire.Target, runtimeScopeOnly bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil || p.store == nil {
		return
	}
	for _, tenantID := range p.capabilities.catalogTenants() {
		plugins := p.orderedPrimePlugins(tenantID, target)
		readyKinds := map[string]bool{}
		for _, plugin := range plugins {
			if plugin.WorkerMode == wire.WorkerModeCold {
				continue
			}
			if runtimeScopeOnly && plugin.WorkerScope != wire.WorkerScopeRuntime {
				continue
			}
			if !p.kindDependenciesReady(plugin, readyKinds) {
				p.logPrimeDependencyBlocked(plugin)
				continue
			}
			p.primeTarget(ctx, tenantID, plugin)
			if p.targetReady(tenantID, plugin.ID) {
				if kind := pluginKindName(plugin); kind != "" {
					readyKinds[kind] = true
				}
			}
		}
	}
}

func (p *Pool) orderedPrimePlugins(tenantID string, target *wire.Target) []manifest.Plugin {
	plugins := p.store.PluginsForTenant(tenantID)
	if target != nil && target.Kind != wire.TargetKindPlugin {
		return nil
	}
	if target != nil {
		plugins = p.filterPrimeClosure(plugins, target.ID)
	}
	return orderPluginsByKindDependencies(plugins, p.opts.PluginKindDependsOn)
}

func (p *Pool) filterPrimeClosure(plugins []manifest.Plugin, targetID string) []manifest.Plugin {
	if targetID == "" {
		return nil
	}
	byID := make(map[string]manifest.Plugin, len(plugins))
	byKind := map[string][]manifest.Plugin{}
	for _, plugin := range plugins {
		byID[plugin.ID] = plugin
		if kind := pluginKindName(plugin); kind != "" {
			byKind[kind] = append(byKind[kind], plugin)
		}
	}
	target, ok := byID[targetID]
	if !ok {
		return nil
	}
	selected := map[string]manifest.Plugin{target.ID: target}
	var addDeps func(manifest.Plugin)
	addDeps = func(plugin manifest.Plugin) {
		for _, depKind := range p.opts.PluginKindDependsOn[pluginKindName(plugin)] {
			for _, dep := range byKind[depKind] {
				if _, ok := selected[dep.ID]; ok {
					continue
				}
				selected[dep.ID] = dep
				addDeps(dep)
			}
		}
	}
	addDeps(target)
	out := make([]manifest.Plugin, 0, len(selected))
	for _, plugin := range selected {
		out = append(out, plugin)
	}
	return out
}

func (p *Pool) kindDependenciesReady(plugin manifest.Plugin, readyKinds map[string]bool) bool {
	for _, depKind := range p.opts.PluginKindDependsOn[pluginKindName(plugin)] {
		if !readyKinds[depKind] {
			return false
		}
	}
	return true
}

func (p *Pool) targetReady(tenantID, targetID string) bool {
	_, ok := p.capabilities.readySnapshot(tenantID, targetID)
	return ok
}

// Capabilities returns the current runtime capability view in stable target-id
// order. Manifest metadata seeds the initial state; worker init acknowledgements
// upgrade targets to authoritative ready metadata.
func (p *Pool) Capabilities() []wire.Capability {
	if p == nil {
		return nil
	}
	return p.capabilities.list()
}

// Invoke routes one Runtime API invoke to the correct warm worker.
func (p *Pool) Invoke(ctx context.Context, in wire.Invoke) (result wire.InvokeResult, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p == nil || p.policy == nil || p.store == nil {
		return failed(in.CallID, wire.ErrorInternal, "worker pool is not configured"), nil
	}
	defer func(start time.Time) {
		p.logInvokeOutcome(in, result, err, time.Since(start))
	}(time.Now())
	switch in.Target.Kind {
	case wire.TargetKindPlugin:
		if !p.tenantAllowed(in.TenantID) {
			p.logTenantForbidden(in)
			return failed(in.CallID, wire.ErrorTenantForbidden, fmt.Sprintf("tenant %q is not served by runtime kernel %q", in.TenantID, p.kernelID)), nil
		}
		plugin, ok := p.store.GetForTenant(in.TenantID, in.Target.ID)
		if !ok {
			return failed(in.CallID, wire.ErrorTargetUnknown, fmt.Sprintf("target %q not found", in.Target.ID)), nil
		}
		if plugin.WorkerMode == wire.WorkerModeCold {
			return p.invokeColdPlugin(ctx, plugin, in), nil
		}
		resultCh := make(chan wire.InvokeResult, 1)
		go func() {
			resultCh <- p.invokeWarm(ctx, plugin, in)
		}()

		select {
		case result := <-resultCh:
			return result, nil
		case <-ctx.Done():
			select {
			case result := <-resultCh:
				return result, nil
			default:
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return failed(in.CallID, wire.ErrorTimeout, "invoke timed out"), nil
			}
			return failed(in.CallID, wire.ErrorCancelled, "invoke cancelled"), nil
		}
	default:
		return failed(in.CallID, wire.ErrorTargetForbidden, fmt.Sprintf("target %s/%s is not an invokable plugin target", in.Target.Kind, in.Target.ID)), nil
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
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" && len(p.opts.AllowedTenants) == 1 && p.opts.AllowedTenants[0] != "*" {
		tenantID = p.opts.AllowedTenants[0]
	}
	if tenantID == "" {
		var payload struct {
			TenantID string `json:"tenant_id"`
		}
		_ = json.Unmarshal(in.Data, &payload)
		tenantID = strings.TrimSpace(payload.TenantID)
	}
	if tenantID == "" {
		tenantID = "default"
	}
	plugin, ok := p.store.GetForTenant(tenantID, in.Target.ID)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrTargetNotFound, in.Target.ID)
	}
	e := p.registry.entryFor(p.workerKey(tenantID, plugin))
	e.mu.Lock()
	defer e.mu.Unlock()
	worker, _, err := p.ensureWorker(ctx, e, plugin, tenantID)
	if err != nil {
		return nil, errors.New(safeWorkerErrorMessage("worker initialization failed", err))
	}
	reply, err := worker.HookEvent(ctx, workerwire.WorkerEvent{
		CallID:            in.CallID,
		TenantID:          tenantID,
		Event:             in.Event,
		ReplyMode:         workerwire.ReplyMode(in.ReplyMode),
		SessionID:         in.SessionID,
		TurnCorrelationID: in.TurnCorrelationID,
		Data:              in.Data,
	})
	if err != nil {
		return nil, errors.New(safeWorkerErrorMessage("worker hook event failed", err))
	}
	if reply == nil {
		return nil, nil
	}
	return &wire.HookEventReply{Op: wire.OpHookEventReply, CallID: reply.CallID, Action: reply.Action, Data: reply.Data, Reason: reply.Reason}, nil
}

func (p *Pool) invokeWarm(ctx context.Context, plugin manifest.Plugin, in wire.Invoke) wire.InvokeResult {
	if ctx == nil {
		ctx = context.Background()
	}
	tool, ok := p.pluginTool(in.TenantID, plugin, in.Tool)
	if !ok {
		return failed(in.CallID, wire.ErrorToolNotFound, fmt.Sprintf("tool %q not found on target %q", in.Tool, in.Target.ID))
	}
	e := p.registry.entryFor(p.workerKey(in.TenantID, plugin))
	worker, err := p.acquireWarmToolSlot(ctx, e, plugin, in.TenantID, tool)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return failed(in.CallID, wire.ErrorTimeout, "invoke timed out")
		}
		if errors.Is(err, context.Canceled) {
			return failed(in.CallID, wire.ErrorCancelled, "invoke cancelled")
		}
		if errors.Is(err, errWarmWorkerBackoff) {
			return failed(in.CallID, wire.ErrorRuntimeBusy, "worker initialization delayed; "+err.Error())
		}
		return failed(in.CallID, wire.ErrorInternal, safeWorkerErrorMessage("worker initialization failed", err))
	}
	defer p.releaseWarmToolSlot(e, tool)
	result, err := worker.Call(ctx, workerwire.WorkerCall{CallID: in.CallID, TenantID: in.TenantID, Tool: in.Tool, Args: in.Args, SessionID: in.SessionID, TurnCorrelationID: in.TurnCorrelationID})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			if p.shouldResetWarmWorkerAfterCallError(e, worker) {
				p.resetWarmWorker(e, worker)
			}
			return failed(in.CallID, wire.ErrorTimeout, "invoke timed out")
		}
		if errors.Is(err, context.Canceled) {
			if p.shouldResetWarmWorkerAfterCallError(e, worker) {
				p.resetWarmWorker(e, worker)
			}
			return failed(in.CallID, wire.ErrorCancelled, "invoke cancelled")
		}
		p.resetWarmWorkerAfterFailure(e, worker)
		return failed(in.CallID, wire.ErrorInternal, safeWorkerErrorMessage("worker call failed", err))
	}
	if !result.OK {
		message := "worker call failed"
		if result.Error != nil && result.Error.Message != "" {
			message = result.Error.Message
		}
		return failed(in.CallID, wire.ErrorInternal, message)
	}
	return wire.InvokeResult{Op: wire.OpInvokeResult, CallID: in.CallID, OK: true, Data: append(json.RawMessage(nil), result.Data...)}
}

func (p *Pool) acquireWarmToolSlot(ctx context.Context, e *entry, plugin manifest.Plugin, tenantID string, tool manifest.Tool) (policy.Worker, error) {
	for {
		e.mu.Lock()
		worker, _, err := p.ensureWorker(ctx, e, plugin, tenantID)
		if err != nil {
			e.mu.Unlock()
			return nil, err
		}
		if e.canRunLocked(tool) {
			e.startToolLocked(tool)
			e.mu.Unlock()
			return worker, nil
		}
		waitCh := e.waitCh
		e.mu.Unlock()
		select {
		case <-waitCh:
			continue
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (p *Pool) releaseWarmToolSlot(e *entry, tool manifest.Tool) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.finishToolLocked(tool)
	e.mu.Unlock()
}

func (p *Pool) resetWarmWorker(e *entry, worker policy.Worker) {
	if e == nil || worker == nil {
		return
	}
	e.mu.Lock()
	if e.worker == worker {
		e.worker = nil
	}
	e.notifyWaitersLocked()
	e.mu.Unlock()
	_ = worker.Shutdown(context.Background())
}

func (p *Pool) resetWarmWorkerAfterFailure(e *entry, worker policy.Worker) {
	if e == nil || worker == nil {
		return
	}
	e.mu.Lock()
	if e.worker == worker {
		e.worker = nil
		e.noteWarmWorkerFailureLocked(time.Now())
	} else {
		e.notifyWaitersLocked()
	}
	e.mu.Unlock()
	_ = worker.Shutdown(context.Background())
}

func (p *Pool) shouldResetWarmWorkerAfterCallError(e *entry, worker policy.Worker) bool {
	if e == nil || worker == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.worker == worker && e.activeToolCallsLocked() <= 1
}

func pluginTool(plugin manifest.Plugin, name string) (manifest.Tool, bool) {
	for _, tool := range plugin.Tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return manifest.Tool{}, false
}

func (p *Pool) pluginTool(tenantID string, plugin manifest.Plugin, name string) (manifest.Tool, bool) {
	if tool, ok := pluginTool(plugin, name); ok {
		return tool, true
	}
	if p == nil || p.capabilities == nil {
		return manifest.Tool{}, false
	}
	spec, ok := p.capabilities.toolSpec(tenantID, plugin.ID, name)
	if !ok {
		return manifest.Tool{}, false
	}
	return manifest.Tool{
		Name:                spec.Name,
		Description:         spec.Description,
		Schema:              append(json.RawMessage(nil), spec.Schema...),
		DeadlineMS:          int(spec.DeadlineMS),
		Concurrency:         spec.Concurrency,
		ExecutionGroup:      spec.ExecutionGroup,
		ConflictsWithGroups: append([]string(nil), spec.ConflictsWithGroups...),
	}, true
}

func (p *Pool) invokeColdPlugin(ctx context.Context, plugin manifest.Plugin, in wire.Invoke) wire.InvokeResult {
	if !p.targetHasTool(in.TenantID, in.Target.ID, in.Tool) {
		return failed(in.CallID, wire.ErrorToolNotFound, fmt.Sprintf("tool %q not found on target %q", in.Tool, in.Target.ID))
	}
	release, err := p.acquireColdWorkerSlot(ctx, in.TenantID)
	if err != nil {
		if errors.Is(err, errColdWorkerBusy) {
			return failed(in.CallID, wire.ErrorRuntimeBusy, fmt.Sprintf("runtime busy: tenant %q reached %d concurrent cold workers", in.TenantID, p.coldWorkerLimit(in.TenantID)))
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return failed(in.CallID, wire.ErrorTimeout, "invoke timed out")
		}
		if errors.Is(err, context.Canceled) {
			return failed(in.CallID, wire.ErrorCancelled, "invoke cancelled")
		}
		return failed(in.CallID, wire.ErrorInternal, err.Error())
	}
	defer release()
	worker, err := p.policy.Spawn(ctx, policy.SpawnReq{
		KernelID:    p.kernelID,
		TenantID:    in.TenantID,
		TargetID:    in.Target.ID,
		TargetKind:  wire.TargetKindPlugin,
		HarnessKind: plugin.Capability().HarnessKind,
		Command:     plugin.LaunchCommand(),
		Runtime:     plugin.Runtime,
		Entry:       plugin.Entry,
		Manifest:    plugin.RawJSON(),
		Env:         p.spawnEnv(in.TenantID),
		WorkingDir:  plugin.RootDir,
		Mode:        policy.SpawnModeCold,
	})
	if err != nil {
		return failed(in.CallID, wire.ErrorInternal, err.Error())
	}
	defer func() {
		_ = worker.Shutdown(context.Background())
		_, _ = worker.Wait()
	}()
	if _, err := worker.Init(ctx, workerwire.WorkerInit{Op: workerwire.OpInit, KernelID: p.kernelID, TenantID: in.TenantID, TargetID: in.Target.ID, Manifest: plugin.RawJSON()}); err != nil {
		return failed(in.CallID, wire.ErrorInternal, err.Error())
	}
	result, err := worker.Call(ctx, workerwire.WorkerCall{Op: workerwire.OpCall, CallID: in.CallID, TenantID: in.TenantID, Tool: in.Tool, Args: in.Args, SessionID: in.SessionID, TurnCorrelationID: in.TurnCorrelationID})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return failed(in.CallID, wire.ErrorTimeout, "invoke timed out")
		}
		if errors.Is(err, context.Canceled) {
			return failed(in.CallID, wire.ErrorCancelled, "invoke cancelled")
		}
		return failed(in.CallID, wire.ErrorInternal, err.Error())
	}
	if !result.OK {
		code := wire.ErrorInternal
		message := "plugin call failed"
		if result.Error != nil {
			message = result.Error.Message
			if wire.IsErrorCode(wire.ErrorCode(result.Error.Code)) {
				code = wire.ErrorCode(result.Error.Code)
			}
		}
		return failed(in.CallID, code, message)
	}
	return wire.InvokeResult{Op: wire.OpInvokeResult, CallID: in.CallID, OK: true, Data: result.Data}
}

func (p *Pool) acquireColdWorkerSlot(ctx context.Context, tenantID string) (func(), error) {
	return p.cold.acquire(ctx, tenantID)
}

func (p *Pool) tenantAllowed(tenantID string) bool {
	if p != nil && p.store != nil {
		for _, catalogTenantID := range p.store.TenantIDs() {
			if catalogTenantID == tenantID {
				return true
			}
		}
	}
	allowed := p.opts.AllowedTenants
	if len(allowed) == 0 {
		return true
	}
	for _, item := range allowed {
		if item == "*" || item == tenantID {
			return true
		}
	}
	return false
}

func (p *Pool) coldWorkerLimit(tenantID string) int {
	return p.cold.limit(tenantID)
}

func (p *Pool) spawnEnv(tenantID string) map[string]string {
	if strings.TrimSpace(p.opts.TabulaHome) == "" {
		return nil
	}
	return map[string]string{
		"TABULA_HOME":       p.opts.TabulaHome,
		"TABULA_TENANT_DIR": filepath.Join(p.opts.TabulaHome, "tenants", tenantID),
		"TABULA_URL":        p.opts.KernelURL,
	}
}

func (p *Pool) workerKey(tenantID string, plugin manifest.Plugin) key {
	if plugin.WorkerScope == wire.WorkerScopeRuntime {
		tenantID = ""
	}
	return key{kernelID: p.kernelID, tenantID: tenantID, targetID: plugin.ID}
}

func (p *Pool) primeTenantID() string {
	if len(p.opts.AllowedTenants) == 0 {
		return "default"
	}
	for _, tenantID := range p.opts.AllowedTenants {
		if tenantID != "*" {
			return tenantID
		}
	}
	return "default"
}

func (p *Pool) logTenantForbidden(in wire.Invoke) {
	logger := p.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("runtime tenant forbidden", "kernel_id", p.kernelID, "tenant_id", in.TenantID, "target", in.Target.ID, "tool", in.Tool, "call_id", in.CallID, "turn_correlation_id", in.TurnCorrelationID)
}

func (p *Pool) logInvokeOutcome(in wire.Invoke, result wire.InvokeResult, err error, elapsed time.Duration) {
	logger := p.logger
	if logger == nil {
		logger = slog.Default()
	}
	outcome := "ok"
	if err != nil {
		outcome = "transport_error"
	} else if result.Error != nil {
		outcome = string(result.Error.Code)
	} else if !result.OK {
		outcome = "failed"
	}
	logger.Info("runtime invoke handled", "kernel_id", p.kernelID, "tenant_id", in.TenantID, "target", in.Target.ID, "tool", in.Tool, "call_id", in.CallID, "turn_correlation_id", in.TurnCorrelationID, "outcome", outcome, "duration_ms", elapsed.Milliseconds())
}

func (p *Pool) coldTenantCounts(tenantID string) (active, queued int) {
	return p.cold.countsFor(tenantID)
}

func (p *Pool) coldCounts() (active, queued int) {
	return p.cold.counts()
}

func (p *Pool) targetHasTool(tenantID, targetID, toolName string) bool {
	return p.capabilities.hasTool(tenantID, targetID, toolName)
}

func (p *Pool) ensureWorker(ctx context.Context, e *entry, plugin manifest.Plugin, tenantID string) (policy.Worker, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return p.starter.ensure(ctx, e, plugin, tenantID)
}

func (p *Pool) primeTarget(ctx context.Context, tenantID string, plugin manifest.Plugin) {
	e := p.registry.entryFor(p.workerKey(tenantID, plugin))
	e.mu.Lock()
	_, existing, err := p.ensureWorker(ctx, e, plugin, tenantID)
	e.mu.Unlock()
	if err != nil {
		p.logPrimeFailure(plugin, err)
		return
	}
	if !existing {
		return
	}
	p.publishTargetSnapshot(tenantID, plugin.ID)
}

func (p *Pool) logPrimeFailure(plugin manifest.Plugin, err error) {
	if p == nil || err == nil {
		return
	}
	logger := p.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn(
		"runtime target priming failed",
		"target", plugin.ID,
		"runtime", pluginRuntime(plugin),
		"entry_path", pluginEntryPath(plugin),
		"command", strings.Join(plugin.LaunchCommand(), " "),
		"diagnostic", safeWorkerErrorMessage("worker initialization failed", err),
	)
}

func (p *Pool) logPrimeDependencyBlocked(plugin manifest.Plugin) {
	if p == nil {
		return
	}
	logger := p.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("runtime target priming blocked by plugin kind dependency", "target", plugin.ID, "kind", pluginKindName(plugin), "depends_on", p.opts.PluginKindDependsOn[pluginKindName(plugin)])
}

func orderPluginsByKindDependencies(plugins []manifest.Plugin, deps map[string][]string) []manifest.Plugin {
	if len(plugins) <= 1 || len(deps) == 0 {
		out := append([]manifest.Plugin(nil), plugins...)
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out
	}
	byID := make(map[string]manifest.Plugin, len(plugins))
	byKind := map[string][]string{}
	for _, plugin := range plugins {
		byID[plugin.ID] = plugin
		if kind := pluginKindName(plugin); kind != "" {
			byKind[kind] = append(byKind[kind], plugin.ID)
		}
	}
	for kind := range byKind {
		sort.Strings(byKind[kind])
	}
	ids := make([]string, 0, len(plugins))
	for _, plugin := range plugins {
		ids = append(ids, plugin.ID)
	}
	sort.Strings(ids)
	visited := map[string]bool{}
	visiting := map[string]bool{}
	orderedIDs := make([]string, 0, len(ids))
	var visit func(string)
	visit = func(id string) {
		if visited[id] || visiting[id] {
			return
		}
		visiting[id] = true
		plugin := byID[id]
		for _, depKind := range deps[pluginKindName(plugin)] {
			for _, depID := range byKind[depKind] {
				visit(depID)
			}
		}
		visiting[id] = false
		visited[id] = true
		orderedIDs = append(orderedIDs, id)
	}
	for _, id := range ids {
		visit(id)
	}
	out := make([]manifest.Plugin, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		out = append(out, byID[id])
	}
	return out
}

func pluginKindName(plugin manifest.Plugin) string {
	if plugin.Kind == nil {
		return ""
	}
	return strings.TrimSpace(plugin.Kind.Name)
}

func pluginRuntime(plugin manifest.Plugin) string {
	if runtime := strings.TrimSpace(plugin.Runtime); runtime != "" {
		return runtime
	}
	if kind := plugin.Capability().HarnessKind; kind != wire.HarnessKindUnknown {
		return string(kind)
	}
	return ""
}

func cloneKindDependencies(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for kind, deps := range in {
		out[kind] = append([]string(nil), deps...)
	}
	return out
}

func cloneTenantLimits(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func safeWorkerErrorMessage(prefix string, err error) string {
	message := prefix
	if strings.TrimSpace(message) == "" {
		message = "worker operation failed"
	}
	if err == nil {
		return message
	}
	errText := err.Error()
	if stderr := stderrCapturedSummary(errText); stderr != "" {
		message += "; " + stderr
	} else if detail := safeWorkerInfrastructureDetail(errText); detail != "" {
		message += "; " + detail
	}
	if strings.Contains(errText, "likely legacy register_request/stdio plugin SDK") {
		message += "; hint: likely legacy register_request/stdio plugin SDK, not the M2 worker protocol"
	}
	return message
}

func safeWorkerInfrastructureDetail(message string) string {
	message = strings.TrimSpace(message)
	for _, prefix := range []string{
		"bare policy: no command configured for runtime",
		"bare policy: runtime is required",
		"bare policy: entry is required",
		"bare policy: working dir is required",
		"bare policy: unsupported spawn mode",
	} {
		if strings.HasPrefix(message, prefix) {
			return message
		}
	}
	return ""
}

func stderrCapturedSummary(message string) string {
	needle := "worker stderr captured:"
	start := strings.Index(message, needle)
	if start < 0 {
		return ""
	}
	start += len(needle)
	end := strings.Index(message[start:], ")")
	if end < 0 {
		end = len(message) - start
	}
	diagnostic := strings.TrimSpace(message[start : start+end])
	parts := strings.Split(diagnostic, ";")
	stats := strings.TrimSpace(parts[0])
	if stats == "" {
		return "worker stderr captured"
	}
	safeParts := []string{"worker stderr captured: " + stats}
	for _, raw := range parts[1:] {
		part := strings.TrimSpace(raw)
		if part == "details truncated" || strings.HasPrefix(part, "stderr sample:") || strings.HasPrefix(part, "hint:") {
			safeParts = append(safeParts, part)
		}
	}
	return strings.Join(safeParts, "; ")
}

func pluginEntryPath(plugin manifest.Plugin) string {
	return plugin.LaunchPath()
}

// Reload evicts matching workers and returns their Runtime API targets.
func (p *Pool) Reload(target *wire.Target, tenants ...string) []wire.Target {
	if p == nil {
		return nil
	}
	evicted := p.registry.evict(target, tenants...)
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
	p.resetTargets(target, tenants...)
	return out
}

func tenantSet(tenants []string) map[string]bool {
	out := map[string]bool{}
	for _, tenantID := range tenants {
		tenantID = strings.TrimSpace(tenantID)
		if tenantID == "" || tenantID == "*" {
			return nil
		}
		out[tenantID] = true
	}
	return out
}

// WorkerCount returns the number of live workers currently tracked.
func (p *Pool) WorkerCount() int {
	if p == nil {
		return 0
	}
	entries := p.registry.entriesSnapshot()
	count := 0
	for _, e := range entries {
		e.mu.Lock()
		if e.worker != nil && e.worker.IsAlive() {
			count++
		}
		e.mu.Unlock()
	}
	activeCold, _ := p.coldCounts()
	return count + activeCold
}

// Close shuts down every tracked worker.
func (p *Pool) Close() {
	_ = p.Reload(nil)
}

func failed(callID string, code wire.ErrorCode, message string) wire.InvokeResult {
	return wire.InvokeResult{Op: wire.OpInvokeResult, CallID: callID, OK: false, Error: &wire.Error{Code: code, Retryable: code == wire.ErrorRuntimeBusy || code == wire.ErrorRuntimeUnavailable, Message: message}}
}

func (p *Pool) watchWorker(tenantID string, plugin manifest.Plugin, entry *entry, worker policy.Worker) {
	if p == nil || entry == nil || worker == nil {
		return
	}
	go func() {
		for event := range worker.Events() {
			if !entryHasWorker(entry, worker) {
				continue
			}
			if event.Err != nil {
				if clearEntryWorkerAfterFailure(entry, worker) {
					p.markTargetCrashed(tenantID, plugin, event.Err)
					_ = worker.Shutdown(context.Background())
				}
				continue
			}
			switch frame := event.Frame.(type) {
			case *workerwire.WorkerToolsUpdated:
				p.applyToolsUpdated(tenantID, plugin, *frame)
			case *workerwire.WorkerEventReply:
				p.publishCriticalFrame(wire.HookEventReply{Op: wire.OpHookEventReply, CallID: frame.CallID, Action: frame.Action, Data: frame.Data, Reason: frame.Reason})
			case *workerwire.WorkerSend:
				p.publishCriticalFrame(wire.PluginSend{Op: wire.OpPluginSend, Target: wire.Target{Kind: wire.TargetKindPlugin, ID: plugin.ID}, Channel: frame.Channel, Type: frame.Type, Payload: frame.Payload, SessionID: frame.SessionID})
			case *workerwire.WorkerLog:
				p.publishBestEffortFrame(wire.PluginLog{Op: wire.OpPluginLog, Target: wire.Target{Kind: wire.TargetKindPlugin, ID: plugin.ID}, Level: frame.Level, Message: frame.Message, Fields: frame.Fields})
			case *workerwire.WorkerError:
				if clearEntryWorkerAfterFailure(entry, worker) {
					message := frame.Error.Code
					if frame.Error.Message != "" {
						message = frame.Error.Message
					}
					p.markTargetCrashed(tenantID, plugin, errors.New(message))
					_ = worker.Shutdown(context.Background())
				}
			}
		}
	}()
}

func (p *Pool) applyToolsUpdated(tenantID string, plugin manifest.Plugin, update workerwire.WorkerToolsUpdated) {
	capabilities, ok := p.capabilities.applyToolsUpdated(tenantID, plugin, update)
	if !ok {
		return
	}
	for _, capability := range capabilities {
		p.publishCriticalFrame(wire.CatalogUpdate{Op: wire.OpCatalogUpdate, Target: capability.Target, Tenants: append([]string(nil), capability.Tenants...), Tools: cloneToolSpecs(capability.Tools), Hooks: cloneHookSpecs(capability.Hooks), Removed: append([]string(nil), update.Removed...), Revision: capability.Revision, State: capability.State, Source: capability.Source})
	}
}

func (p *Pool) resetTargets(target *wire.Target, tenants ...string) {
	p.capabilities.reset(target, tenants...)
}

func (p *Pool) markTargetInitializing(tenantID string, plugin manifest.Plugin) {
	p.setTargetState(tenantID, plugin, wire.CapabilityStateInitializing)
	p.publishCriticalFrame(wire.LifecycleNotice{Op: wire.OpLifecycleNotice, Target: wire.Target{Kind: wire.TargetKindPlugin, ID: plugin.ID}, State: wire.LifecycleStateStarting})
}

func (p *Pool) markTargetFailed(tenantID string, plugin manifest.Plugin) {
	p.setTargetState(tenantID, plugin, wire.CapabilityStateFailed)
}

func (p *Pool) markTargetCrashed(tenantID string, plugin manifest.Plugin, err error) {
	p.markTargetFailed(tenantID, plugin)
	message := "worker failed"
	if err != nil && err.Error() != "" {
		message = safeWorkerErrorMessage("worker failed", err)
	}
	p.publishCriticalFrame(wire.LifecycleNotice{Op: wire.OpLifecycleNotice, Target: wire.Target{Kind: wire.TargetKindPlugin, ID: plugin.ID}, State: wire.LifecycleStateCrashed, Message: message})
}

func (p *Pool) setTargetState(tenantID string, plugin manifest.Plugin, state wire.CapabilityState) {
	p.capabilities.setState(tenantID, plugin, state)
}

func (p *Pool) publishTargetSnapshot(tenantID, targetID string) {
	capability, ok := p.capabilities.readySnapshot(tenantID, targetID)
	if !ok {
		return
	}
	p.publishCriticalFrame(wire.CatalogUpdate{Op: wire.OpCatalogUpdate, Target: capability.Target, Tenants: append([]string(nil), capability.Tenants...), Tools: cloneToolSpecs(capability.Tools), Hooks: cloneHookSpecs(capability.Hooks), Revision: capability.Revision, State: capability.State, Source: capability.Source})
	p.publishCriticalFrame(wire.LifecycleNotice{Op: wire.OpLifecycleNotice, Target: capability.Target, State: wire.LifecycleStateReady})
}

func (p *Pool) markTargetReady(tenantID string, plugin manifest.Plugin, ack workerwire.WorkerInitAck) {
	for _, capability := range p.capabilities.markReady(tenantID, plugin, ack) {
		p.publishCriticalFrame(wire.CatalogUpdate{Op: wire.OpCatalogUpdate, Target: capability.Target, Tenants: append([]string(nil), capability.Tenants...), Tools: cloneToolSpecs(capability.Tools), Hooks: cloneHookSpecs(capability.Hooks), Revision: capability.Revision, State: capability.State, Source: capability.Source, Diagnostic: "worker ready"})
	}
	p.publishCriticalFrame(wire.LifecycleNotice{Op: wire.OpLifecycleNotice, Target: wire.Target{Kind: wire.TargetKindPlugin, ID: plugin.ID}, State: wire.LifecycleStateReady})
}

func (p *Pool) publishCriticalFrame(frame any) {
	if p == nil {
		return
	}
	p.publisher.PublishCritical(frame)
}

func (p *Pool) publishBestEffortFrame(frame any) {
	if p == nil {
		return
	}
	p.publisher.PublishBestEffort(frame)
}

func cloneCapability(in wire.Capability) wire.Capability {
	out := in
	out.Tenants = append([]string(nil), in.Tenants...)
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

func sameToolSpecs(a, b []wire.ToolSpec) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || string(a[i].Schema) != string(b[i].Schema) || a[i].Description != b[i].Description || a[i].DeadlineMS != b[i].DeadlineMS {
			return false
		}
	}
	return true
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

package kernel

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

// RuntimeRegistry tracks Runtime API connections attached to this kernel.
//
// It is intentionally transport-agnostic. M2 registers only the authenticated
// local runtime attachment; later M4/M6 registry and backend work can reuse this
// read model without coupling the kernel to a concrete local/remote backend.
type RuntimeRegistry struct {
	mu             sync.RWMutex
	runtimes       map[string]*RuntimeAttachment
	defined        map[string]RuntimeDefinition
	tenantBindings map[string]TenantRuntimeBinding
}

// RuntimeAttachment is the read-model record for one runtime.
type RuntimeAttachment struct {
	ID            string
	Attached      bool
	PID           int
	TenantsServed []string
	Capabilities  []runtimeapi.Capability
	Health        runtimeapi.HealthResp
	LastError     string
	ConnectedAt   time.Time
	conn          runtimeapi.RuntimeConn
	done          <-chan struct{}
	targetStatus  map[string]runtimeTargetStatus
}

type runtimeTargetStatus struct {
	PID          int
	Lifecycle    wire.LifecycleState
	Message      string
	LastRevision int64
}

type runtimeHookTarget struct {
	RuntimeID  string
	Capability runtimeapi.Capability
	Conn       runtimeapi.RuntimeConn
	Done       <-chan struct{}
}

// NewRuntimeRegistry creates an empty runtime attachment registry.
func NewRuntimeRegistry() *RuntimeRegistry {
	return &RuntimeRegistry{runtimes: make(map[string]*RuntimeAttachment), defined: map[string]RuntimeDefinition{defaultRuntimeID: {ID: defaultRuntimeID, Backend: "local"}}, tenantBindings: map[string]TenantRuntimeBinding{}}
}

func (r *RuntimeRegistry) Configure(definitions []RuntimeDefinition, bindings map[string]TenantRuntimeBinding) error {
	if r == nil {
		return fmt.Errorf("runtime registry is nil")
	}
	if len(definitions) == 0 {
		definitions = []RuntimeDefinition{{ID: defaultRuntimeID, Backend: "local"}}
	}
	defined := make(map[string]RuntimeDefinition, len(definitions))
	for _, definition := range definitions {
		if err := wire.ValidateRuntimeID(definition.ID); err != nil {
			return err
		}
		if _, ok := defined[definition.ID]; ok {
			return fmt.Errorf("duplicate runtime id %q", definition.ID)
		}
		defined[definition.ID] = definition
	}
	copyBindings := make(map[string]TenantRuntimeBinding, len(bindings))
	for tenantID, binding := range bindings {
		copyBindings[tenantID] = TenantRuntimeBinding{AllowedRuntimes: append([]string(nil), binding.AllowedRuntimes...), DefaultRuntime: binding.DefaultRuntime}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defined = defined
	r.tenantBindings = copyBindings
	return nil
}

// RegisterHello records one accepted Hello as an attached runtime. M2 does not
// yet route kernel dispatch through this record; M2-07 replaces the empty local
// read model with the real RuntimeConn cutover.
func (r *RuntimeRegistry) RegisterHello(runtimeID string, conn runtimeapi.RuntimeConn, capabilities []runtimeapi.Capability, pid int, tenantsServed ...[]string) error {
	if r == nil {
		return fmt.Errorf("runtime registry is nil")
	}
	if err := wire.ValidateRuntimeID(runtimeID); err != nil {
		return err
	}
	tenants := []string{"*"}
	if len(tenantsServed) > 0 && len(tenantsServed[0]) > 0 {
		tenants = append([]string(nil), tenantsServed[0]...)
	}
	attachment := &RuntimeAttachment{ID: runtimeID, Attached: true, PID: pid, ConnectedAt: time.Now().UTC(), TenantsServed: tenants, Capabilities: append([]runtimeapi.Capability(nil), capabilities...), conn: conn, targetStatus: make(map[string]runtimeTargetStatus)}
	if closer, ok := conn.(interface{ Done() <-chan struct{} }); ok {
		attachment.done = closer.Done()
	}
	r.mu.Lock()
	r.runtimes[runtimeID] = attachment
	r.mu.Unlock()
	return nil
}

// MarkDetached updates a runtime as detached and records a sanitized reason.
func (r *RuntimeRegistry) MarkDetached(runtimeID string, err error) {
	if r == nil || strings.TrimSpace(runtimeID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	attachment := r.runtimes[runtimeID]
	if attachment == nil {
		attachment = &RuntimeAttachment{ID: runtimeID}
		r.runtimes[runtimeID] = attachment
	}
	attachment.Attached = false
	attachment.LastError = sanitizeRuntimeAttachmentError(err)
	attachment.conn = nil
	attachment.done = nil
}

func (r *RuntimeRegistry) RuntimeConn(runtimeID string) runtimeapi.RuntimeConn {
	if r == nil || strings.TrimSpace(runtimeID) == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	attachment := r.runtimes[runtimeID]
	if attachment == nil || !attachment.Attached {
		return nil
	}
	return attachment.conn
}

func (r *RuntimeRegistry) TenantsServed(runtimeID string) []string {
	if r == nil || strings.TrimSpace(runtimeID) == "" {
		return []string{"*"}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	attachment := r.runtimes[runtimeID]
	if attachment == nil || len(attachment.TenantsServed) == 0 {
		return []string{"*"}
	}
	return append([]string(nil), attachment.TenantsServed...)
}

func (r *RuntimeRegistry) Pick(tenantID string) (runtimeapi.RuntimeConn, string, wire.ErrorCode, error) {
	if r == nil {
		return nil, "", wire.ErrorRuntimeUnavailable, fmt.Errorf("runtime registry is nil")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	binding := r.bindingForTenantLocked(tenantID)
	runtimeID := strings.TrimSpace(binding.DefaultRuntime)
	if runtimeID == "" {
		runtimeID = defaultRuntimeID
	}
	conn, code, err := r.runtimeForTenantLocked(tenantID, binding, runtimeID)
	return conn, runtimeID, code, err
}

func (r *RuntimeRegistry) RuntimeForTenant(tenantID, runtimeID string) (runtimeapi.RuntimeConn, wire.ErrorCode, error) {
	if r == nil {
		return nil, wire.ErrorRuntimeUnavailable, fmt.Errorf("runtime registry is nil")
	}
	runtimeID = strings.TrimSpace(runtimeID)
	if runtimeID == "" {
		return nil, wire.ErrorRuntimeUnavailable, fmt.Errorf("runtime id is required")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.runtimeForTenantLocked(tenantID, r.bindingForTenantLocked(tenantID), runtimeID)
}

func (r *RuntimeRegistry) RuntimeAllowedForTenant(tenantID, runtimeID string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	binding := r.bindingForTenantLocked(tenantID)
	attachment := r.runtimes[runtimeID]
	return runtimeAllowed(binding.AllowedRuntimes, runtimeID) && attachment != nil && runtimeServesTenant(attachment.TenantsServed, tenantID)
}

func (r *RuntimeRegistry) bindingForTenantLocked(tenantID string) TenantRuntimeBinding {
	binding, ok := r.tenantBindings[tenantID]
	if ok {
		return binding
	}
	binding = TenantRuntimeBinding{AllowedRuntimes: []string{"*"}, DefaultRuntime: defaultRuntimeID}
	if len(r.defined) == 1 {
		for runtimeID := range r.defined {
			binding.DefaultRuntime = runtimeID
		}
	}
	return binding
}

func (r *RuntimeRegistry) runtimeForTenantLocked(tenantID string, binding TenantRuntimeBinding, runtimeID string) (runtimeapi.RuntimeConn, wire.ErrorCode, error) {
	if !runtimeAllowed(binding.AllowedRuntimes, runtimeID) {
		return nil, wire.ErrorTenantForbidden, fmt.Errorf("tenant %q cannot use runtime %q", tenantID, runtimeID)
	}
	if _, ok := r.defined[runtimeID]; !ok {
		return nil, wire.ErrorRuntimeUnavailable, fmt.Errorf("runtime %q is not configured", runtimeID)
	}
	attachment := r.runtimes[runtimeID]
	if attachment == nil || !attachment.Attached || attachment.conn == nil {
		return nil, wire.ErrorRuntimeUnavailable, fmt.Errorf("runtime %q is unavailable", runtimeID)
	}
	if !runtimeServesTenant(attachment.TenantsServed, tenantID) {
		return nil, wire.ErrorTenantForbidden, fmt.Errorf("runtime %q does not serve tenant %q", runtimeID, tenantID)
	}
	return attachment.conn, "", nil
}

func runtimeAllowed(allowed []string, runtimeID string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, item := range allowed {
		if item == "*" || item == runtimeID {
			return true
		}
	}
	return false
}

func (r *RuntimeRegistry) Attached(runtimeID string) bool {
	if r == nil || strings.TrimSpace(runtimeID) == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	attachment := r.runtimes[runtimeID]
	return attachment != nil && attachment.Attached
}

func (r *RuntimeRegistry) Defined(runtimeID string) bool {
	if r == nil || strings.TrimSpace(runtimeID) == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.defined[runtimeID]
	return ok
}

func (r *RuntimeRegistry) SetPID(runtimeID string, pid int) {
	if r == nil || strings.TrimSpace(runtimeID) == "" || pid <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	attachment := r.runtimes[runtimeID]
	if attachment == nil || !attachment.Attached {
		return
	}
	attachment.PID = pid
}

func (r *RuntimeRegistry) ApplyCatalogUpdate(runtimeID string, update wire.CatalogUpdate) (runtimeapi.Capability, bool, error) {
	if r == nil {
		return runtimeapi.Capability{}, false, fmt.Errorf("runtime registry is nil")
	}
	if err := validateCatalogUpdate(update); err != nil {
		return runtimeapi.Capability{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	attachment := r.runtimes[runtimeID]
	if attachment == nil || !attachment.Attached {
		return runtimeapi.Capability{}, false, fmt.Errorf("runtime %q is not attached", runtimeID)
	}
	attachment.TenantsServed = mergeRuntimeTenants(attachment.TenantsServed, update.Tenants)
	capability := runtimeapi.Capability{
		Target:   update.Target,
		Tenants:  append([]string(nil), update.Tenants...),
		Tools:    append([]wire.ToolSpec(nil), update.Tools...),
		Hooks:    append([]wire.HookSpec(nil), update.Hooks...),
		Revision: update.Revision,
		State:    update.State,
		Source:   update.Source,
	}
	if err := capability.Validate(); err != nil {
		return runtimeapi.Capability{}, false, err
	}
	replaced := false
	for i := range attachment.Capabilities {
		if sameRuntimeCapability(attachment.Capabilities[i], update.Target, update.Tenants) {
			attachment.Capabilities[i] = capability
			replaced = true
			break
		}
	}
	if !replaced {
		attachment.Capabilities = append(attachment.Capabilities, capability)
	}
	status := attachment.ensureTargetStatus(update.Target)
	status.LastRevision = update.Revision
	status.Message = runtimeCapabilityDiagnostic(update.State)
	attachment.targetStatus[runtimeTargetKey(update.Target)] = status
	return capability, true, nil
}

func (r *RuntimeRegistry) ApplyLifecycleNotice(runtimeID string, notice wire.LifecycleNotice) (runtimeapi.Capability, bool, error) {
	if r == nil {
		return runtimeapi.Capability{}, false, fmt.Errorf("runtime registry is nil")
	}
	if err := validateLifecycleNotice(notice); err != nil {
		return runtimeapi.Capability{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	attachment := r.runtimes[runtimeID]
	if attachment == nil || !attachment.Attached {
		return runtimeapi.Capability{}, false, fmt.Errorf("runtime %q is not attached", runtimeID)
	}
	status := attachment.ensureTargetStatus(notice.Target)
	status.PID = notice.PID
	status.Lifecycle = notice.State
	status.Message = runtimeLifecycleDiagnostic(notice.State)
	attachment.targetStatus[runtimeTargetKey(notice.Target)] = status
	for i := range attachment.Capabilities {
		if !sameRuntimeTarget(attachment.Capabilities[i].Target, notice.Target) {
			continue
		}
		switch notice.State {
		case wire.LifecycleStateStarting:
			attachment.Capabilities[i].State = wire.CapabilityStateInitializing
		case wire.LifecycleStateStopping, wire.LifecycleStateExited:
			attachment.Capabilities[i].State = wire.CapabilityStateStale
		case wire.LifecycleStateCrashed:
			attachment.Capabilities[i].State = wire.CapabilityStateFailed
		case wire.LifecycleStateReady:
			if attachment.Capabilities[i].Revision > 0 || len(attachment.Capabilities[i].Tools) > 0 || len(attachment.Capabilities[i].Hooks) > 0 {
				attachment.Capabilities[i].State = wire.CapabilityStateReady
			}
		}
		return attachment.Capabilities[i], true, nil
	}
	return runtimeapi.Capability{}, false, nil
}

func (r *RuntimeRegistry) HookTargets() []runtimeHookTarget {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []runtimeHookTarget
	for runtimeID, attachment := range r.runtimes {
		if attachment == nil || !attachment.Attached || attachment.conn == nil {
			continue
		}
		for _, capability := range attachment.Capabilities {
			out = append(out, runtimeHookTarget{
				RuntimeID:  runtimeID,
				Capability: capability,
				Conn:       attachment.conn,
				Done:       attachment.done,
			})
		}
	}
	return out
}

// Snapshot returns a stable copy of the runtime read model.
func (r *RuntimeRegistry) Snapshot() []RuntimeAttachment {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RuntimeAttachment, 0, len(r.runtimes))
	for _, attachment := range r.runtimes {
		if attachment == nil {
			continue
		}
		copyAttachment := *attachment
		copyAttachment.TenantsServed = append([]string(nil), attachment.TenantsServed...)
		copyAttachment.Capabilities = append([]runtimeapi.Capability(nil), attachment.Capabilities...)
		if attachment.targetStatus != nil {
			copyAttachment.targetStatus = make(map[string]runtimeTargetStatus, len(attachment.targetStatus))
			for key, status := range attachment.targetStatus {
				copyAttachment.targetStatus[key] = status
			}
		}
		copyAttachment.conn = nil
		copyAttachment.done = nil
		out = append(out, copyAttachment)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sanitizeRuntimeAttachmentError(err error) string {
	if err == nil {
		return ""
	}
	// Runtime connection errors can wrap protocol frames and invoke arguments in
	// future transports; keep the M2 read model intentionally coarse so bearer
	// tokens or user payloads are never exposed through logs/status.
	return "runtime connection failed"
}

func (a *RuntimeAttachment) ensureTargetStatus(target wire.Target) runtimeTargetStatus {
	if a.targetStatus == nil {
		a.targetStatus = make(map[string]runtimeTargetStatus)
	}
	return a.targetStatus[runtimeTargetKey(target)]
}

func runtimeTargetKey(target wire.Target) string {
	return string(target.Kind) + ":" + target.ID
}

func sameRuntimeTarget(left, right wire.Target) bool {
	return left.Kind == right.Kind && left.ID == right.ID
}

func sameRuntimeCapability(left runtimeapi.Capability, target wire.Target, tenants []string) bool {
	return sameRuntimeTarget(left.Target, target) && sameTenantSet(left.Tenants, tenants)
}

func sameTenantSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]int, len(left))
	for _, item := range left {
		seen[item]++
	}
	for _, item := range right {
		seen[item]--
		if seen[item] < 0 {
			return false
		}
	}
	return true
}

func mergeRuntimeTenants(existing, added []string) []string {
	if len(added) == 0 || servesAllTenants(existing) {
		return append([]string(nil), existing...)
	}
	if servesAllTenants(added) {
		return []string{"*"}
	}
	merged := append([]string(nil), existing...)
	seen := make(map[string]bool, len(merged)+len(added))
	for _, tenantID := range merged {
		seen[tenantID] = true
	}
	for _, tenantID := range added {
		tenantID = strings.TrimSpace(tenantID)
		if tenantID == "" || seen[tenantID] {
			continue
		}
		merged = append(merged, tenantID)
		seen[tenantID] = true
	}
	return merged
}

func servesAllTenants(tenants []string) bool {
	for _, tenantID := range tenants {
		if tenantID == "*" {
			return true
		}
	}
	return false
}

func validateCatalogUpdate(update wire.CatalogUpdate) error {
	if err := update.Target.Validate(); err != nil {
		return err
	}
	if update.Revision <= 0 {
		return fmt.Errorf("catalog_update revision must be positive")
	}
	capability := runtimeapi.Capability{Target: update.Target, Tenants: update.Tenants, Tools: update.Tools, Hooks: update.Hooks, Revision: update.Revision, State: update.State, Source: update.Source}
	return capability.Validate()
}

func validateLifecycleNotice(notice wire.LifecycleNotice) error {
	if err := notice.Target.Validate(); err != nil {
		return err
	}
	switch notice.State {
	case wire.LifecycleStateStarting, wire.LifecycleStateReady, wire.LifecycleStateStopping, wire.LifecycleStateExited, wire.LifecycleStateCrashed:
		return nil
	default:
		return fmt.Errorf("unknown lifecycle state %q", notice.State)
	}
}

func runtimeCapabilityDiagnostic(state wire.CapabilityState) string {
	switch state {
	case wire.CapabilityStateManifestLoaded, wire.CapabilityStateInitializing, wire.CapabilityStateReady, wire.CapabilityStateFailed, wire.CapabilityStateStale:
		return string(state)
	default:
		return ""
	}
}

func runtimeLifecycleDiagnostic(state wire.LifecycleState) string {
	switch state {
	case wire.LifecycleStateStarting, wire.LifecycleStateReady, wire.LifecycleStateStopping, wire.LifecycleStateExited, wire.LifecycleStateCrashed:
		return string(state)
	default:
		return ""
	}
}

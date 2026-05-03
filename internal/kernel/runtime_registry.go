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
	mu       sync.RWMutex
	runtimes map[string]*RuntimeAttachment
}

// RuntimeAttachment is the read-model record for one runtime.
type RuntimeAttachment struct {
	ID           string
	Attached     bool
	PID          int
	Capabilities []runtimeapi.Capability
	Health       runtimeapi.HealthResp
	LastError    string
	ConnectedAt  time.Time
	conn         runtimeapi.RuntimeConn
	done         <-chan struct{}
	targetStatus map[string]runtimeTargetStatus
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
	return &RuntimeRegistry{runtimes: make(map[string]*RuntimeAttachment)}
}

// RegisterHello records one accepted Hello as an attached runtime. M2 does not
// yet route kernel dispatch through this record; M2-07 replaces the empty local
// read model with the real RuntimeConn cutover.
func (r *RuntimeRegistry) RegisterHello(runtimeID string, conn runtimeapi.RuntimeConn, capabilities []runtimeapi.Capability, pid int) error {
	if r == nil {
		return fmt.Errorf("runtime registry is nil")
	}
	if err := wire.ValidateRuntimeID(runtimeID); err != nil {
		return err
	}
	attachment := &RuntimeAttachment{ID: runtimeID, Attached: true, PID: pid, ConnectedAt: time.Now().UTC(), Capabilities: append([]runtimeapi.Capability(nil), capabilities...), conn: conn, targetStatus: make(map[string]runtimeTargetStatus)}
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

func (r *RuntimeRegistry) Attached(runtimeID string) bool {
	if r == nil || strings.TrimSpace(runtimeID) == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	attachment := r.runtimes[runtimeID]
	return attachment != nil && attachment.Attached
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
	capability := runtimeapi.Capability{
		Target:   update.Target,
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
		if sameRuntimeTarget(attachment.Capabilities[i].Target, update.Target) {
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

func validateCatalogUpdate(update wire.CatalogUpdate) error {
	if err := update.Target.Validate(); err != nil {
		return err
	}
	if update.Revision <= 0 {
		return fmt.Errorf("catalog_update revision must be positive")
	}
	capability := runtimeapi.Capability{Target: update.Target, Tools: update.Tools, Hooks: update.Hooks, Revision: update.Revision, State: update.State, Source: update.Source}
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

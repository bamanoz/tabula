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
}

// NewRuntimeRegistry creates an empty runtime attachment registry.
func NewRuntimeRegistry() *RuntimeRegistry {
	return &RuntimeRegistry{runtimes: make(map[string]*RuntimeAttachment)}
}

// RegisterHello records one accepted Hello as an attached runtime. M2 does not
// yet route kernel dispatch through this record; M2-07 replaces the empty local
// read model with the real RuntimeConn cutover.
func (r *RuntimeRegistry) RegisterHello(runtimeID string, capabilities []runtimeapi.Capability, pid int) error {
	if r == nil {
		return fmt.Errorf("runtime registry is nil")
	}
	if err := wire.ValidateRuntimeID(runtimeID); err != nil {
		return err
	}
	attachment := &RuntimeAttachment{ID: runtimeID, Attached: true, PID: pid, ConnectedAt: time.Now().UTC(), Capabilities: append([]runtimeapi.Capability(nil), capabilities...)}
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

package pool

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

const defaultWarmWorkerBackoffBase = time.Second
const defaultWarmWorkerBackoffMax = 30 * time.Second
const defaultWarmWorkerBackoffStable = time.Minute

var errWarmWorkerBackoff = errors.New("warm worker restart backoff active")

var warmWorkerBackoffBase = defaultWarmWorkerBackoffBase
var warmWorkerBackoffMax = defaultWarmWorkerBackoffMax
var warmWorkerBackoffStable = defaultWarmWorkerBackoffStable

type workerRegistry struct {
	mu      sync.Mutex
	entries map[key]*entry
}

type key struct {
	kernelID string
	tenantID string
	targetID string
}

type entry struct {
	mu                  sync.Mutex
	worker              policy.Worker
	activeGroups        map[string]int
	waitCh              chan struct{}
	consecutiveFailures int
	backoffUntil        time.Time
	workerStartedAt     time.Time
}

type warmWorkerBackoffError struct {
	remaining time.Duration
}

func (e warmWorkerBackoffError) Error() string {
	remaining := e.remaining.Round(time.Millisecond)
	if remaining < 0 {
		remaining = 0
	}
	return fmt.Sprintf("warm worker restart backoff active; retry after %s", remaining)
}

func (e warmWorkerBackoffError) Is(target error) bool {
	return target == errWarmWorkerBackoff
}

type evictedEntry struct {
	target wire.Target
	entry  *entry
}

func newWorkerRegistry() *workerRegistry {
	return &workerRegistry{entries: map[key]*entry{}}
}

func (r *workerRegistry) entryFor(k key) *entry {
	if r == nil {
		return newEntry()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing := r.entries[k]; existing != nil {
		return existing
	}
	e := newEntry()
	r.entries[k] = e
	return e
}

func newEntry() *entry {
	return &entry{activeGroups: map[string]int{}, waitCh: make(chan struct{})}
}

type operationSpec struct {
	ExecutionGroup      string
	ConflictsWithGroups []string
}

func toolOperation(tool manifest.Tool) operationSpec {
	return operationSpec{ExecutionGroup: tool.ExecutionGroup, ConflictsWithGroups: append([]string(nil), tool.ConflictsWithGroups...)}
}

func hookOperation(hook manifest.Hook) operationSpec {
	return operationSpec{ExecutionGroup: hook.ExecutionGroup, ConflictsWithGroups: append([]string(nil), hook.ConflictsWithGroups...)}
}

func (e *entry) canRunLocked(op operationSpec) bool {
	for _, group := range op.ConflictsWithGroups {
		if e.activeGroups[group] > 0 {
			return false
		}
	}
	return true
}

func (e *entry) startOperationLocked(op operationSpec) {
	if op.ExecutionGroup == "" {
		return
	}
	e.activeGroups[op.ExecutionGroup]++
}

func (e *entry) finishOperationLocked(op operationSpec) {
	if op.ExecutionGroup == "" {
		e.notifyWaitersLocked()
		return
	}
	if count := e.activeGroups[op.ExecutionGroup]; count <= 1 {
		delete(e.activeGroups, op.ExecutionGroup)
	} else {
		e.activeGroups[op.ExecutionGroup] = count - 1
	}
	e.notifyWaitersLocked()
}

func (e *entry) notifyWaitersLocked() {
	if e.waitCh == nil {
		e.waitCh = make(chan struct{})
		return
	}
	close(e.waitCh)
	e.waitCh = make(chan struct{})
}

func (e *entry) activeToolCallsLocked() int {
	total := 0
	for _, count := range e.activeGroups {
		total += count
	}
	return total
}

func (e *entry) warmWorkerBackoffErrLocked(now time.Time) error {
	if e == nil || e.backoffUntil.IsZero() || !e.backoffUntil.After(now) {
		return nil
	}
	return warmWorkerBackoffError{remaining: e.backoffUntil.Sub(now)}
}

func (e *entry) noteWarmWorkerFailureLocked(now time.Time) {
	if e == nil {
		return
	}
	if !e.workerStartedAt.IsZero() && now.Sub(e.workerStartedAt) >= warmWorkerStableDuration() {
		e.consecutiveFailures = 0
	}
	e.workerStartedAt = time.Time{}
	e.consecutiveFailures++
	e.backoffUntil = now.Add(warmWorkerBackoffDelay(e.consecutiveFailures))
	e.notifyWaitersLocked()
}

func (e *entry) noteWarmWorkerStartedLocked() {
	if e == nil {
		return
	}
	e.workerStartedAt = time.Now()
	e.backoffUntil = time.Time{}
	e.notifyWaitersLocked()
}

func warmWorkerBackoffDelay(failures int) time.Duration {
	if failures <= 1 {
		return 0
	}
	base := warmWorkerBackoffBase
	if base <= 0 {
		base = defaultWarmWorkerBackoffBase
	}
	maxDelay := warmWorkerBackoffMax
	if maxDelay <= 0 {
		maxDelay = defaultWarmWorkerBackoffMax
	}
	delay := base
	for i := 2; i < failures; i++ {
		if delay >= maxDelay/2 {
			delay = maxDelay
			break
		}
		delay *= 2
	}
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

func warmWorkerStableDuration() time.Duration {
	if warmWorkerBackoffStable <= 0 {
		return defaultWarmWorkerBackoffStable
	}
	return warmWorkerBackoffStable
}

func (r *workerRegistry) evict(target *wire.Target, tenants ...string) []evictedEntry {
	if r == nil {
		return nil
	}
	requestedTenants := tenantSet(tenants)
	r.mu.Lock()
	defer r.mu.Unlock()
	evicted := make([]evictedEntry, 0)
	for k, e := range r.entries {
		if target != nil && (target.Kind != wire.TargetKindPlugin || target.ID != k.targetID) {
			continue
		}
		if len(requestedTenants) > 0 {
			if k.tenantID == "" || !requestedTenants[k.tenantID] {
				continue
			}
		}
		delete(r.entries, k)
		evicted = append(evicted, evictedEntry{target: wire.Target{Kind: wire.TargetKindPlugin, ID: k.targetID}, entry: e})
	}
	return evicted
}

func (r *workerRegistry) entriesSnapshot() []*entry {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := make([]*entry, 0, len(r.entries))
	for _, e := range r.entries {
		entries = append(entries, e)
	}
	return entries
}

func entryHasWorker(entry *entry, worker policy.Worker) bool {
	if entry == nil {
		return false
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.worker == worker
}

func clearEntryWorker(entry *entry, worker policy.Worker) {
	if entry == nil {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.worker == worker {
		entry.worker = nil
	}
}

func clearEntryWorkerAfterFailure(entry *entry, worker policy.Worker) bool {
	if entry == nil {
		return false
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.worker != worker {
		return false
	}
	entry.worker = nil
	entry.noteWarmWorkerFailureLocked(time.Now())
	return true
}

package kernel

import (
	"sync"

	"github.com/bamanoz/tabula/internal/runtime/wire"
)

type runtimeBusyState struct {
	count int
	done  chan struct{}
}

var closedRuntimeTargetBusyDone = func() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}()

func (h *Hub) markRuntimeTargetBusy(runtimeID string, target wire.Target) func() {
	if h == nil || runtimeID == "" {
		return func() {}
	}
	key := runtimeTargetBusyKey(runtimeID, target)
	h.runtimeBusyMu.Lock()
	state := h.runtimeBusy[key]
	if state == nil {
		state = &runtimeBusyState{done: make(chan struct{})}
		h.runtimeBusy[key] = state
	}
	state.count++
	h.runtimeBusyMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() { h.releaseRuntimeTargetBusy(key, state) })
	}
}

func (h *Hub) tryMarkRuntimeTargetBusy(runtimeID string, target wire.Target) (func(), bool) {
	if h == nil || runtimeID == "" {
		return func() {}, true
	}
	key := runtimeTargetBusyKey(runtimeID, target)
	h.runtimeBusyMu.Lock()
	defer h.runtimeBusyMu.Unlock()
	if h.runtimeBusy[key] != nil {
		return nil, false
	}
	state := &runtimeBusyState{count: 1, done: make(chan struct{})}
	h.runtimeBusy[key] = state
	var once sync.Once
	return func() {
		once.Do(func() { h.releaseRuntimeTargetBusy(key, state) })
	}, true
}

func (h *Hub) releaseRuntimeTargetBusy(key string, state *runtimeBusyState) {
	h.runtimeBusyMu.Lock()
	defer h.runtimeBusyMu.Unlock()
	if h.runtimeBusy[key] != state {
		return
	}
	state.count--
	if state.count > 0 {
		return
	}
	delete(h.runtimeBusy, key)
	close(state.done)
}

func (h *Hub) isRuntimeTargetBusy(runtimeID string, target wire.Target) bool {
	if h == nil || runtimeID == "" {
		return false
	}
	h.runtimeBusyMu.RLock()
	defer h.runtimeBusyMu.RUnlock()
	return h.runtimeBusy[runtimeTargetBusyKey(runtimeID, target)] != nil
}

func (h *Hub) runtimeTargetBusyDone(runtimeID string, target wire.Target) <-chan struct{} {
	if h == nil || runtimeID == "" {
		return nil
	}
	h.runtimeBusyMu.RLock()
	defer h.runtimeBusyMu.RUnlock()
	state := h.runtimeBusy[runtimeTargetBusyKey(runtimeID, target)]
	if state == nil {
		return closedRuntimeTargetBusyDone
	}
	return state.done
}

func runtimeTargetBusyKey(runtimeID string, target wire.Target) string {
	return runtimeID + "\x00" + string(target.Kind) + "\x00" + target.ID
}

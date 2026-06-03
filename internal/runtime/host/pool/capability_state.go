package pool

import (
	"sort"
	"sync"

	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

type capabilityState struct {
	mu      sync.RWMutex
	store   *manifest.Store
	targets map[string]wire.Capability
}

func newCapabilityState(store *manifest.Store) *capabilityState {
	state := &capabilityState{store: store, targets: map[string]wire.Capability{}}
	state.reset(nil)
	return state
}

func (s *capabilityState) list() []wire.Capability {
	if s == nil {
		return nil
	}
	byTarget := map[string]wire.Capability{}
	for _, tenantID := range s.catalogTenants() {
		for _, capability := range s.store.CapabilitiesForTenant(tenantID) {
			byTarget[capabilityKey(tenantID, capability.Target.ID)] = capability
		}
	}
	s.mu.RLock()
	for key, capability := range s.targets {
		byTarget[key] = cloneCapability(capability)
	}
	s.mu.RUnlock()
	ids := make([]string, 0, len(byTarget))
	for id := range byTarget {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]wire.Capability, 0, len(ids))
	for _, id := range ids {
		out = append(out, byTarget[id])
	}
	return out
}

func (s *capabilityState) hasTool(tenantID, targetID, toolName string) bool {
	if s == nil || toolName == "" {
		return false
	}
	s.mu.RLock()
	capability, ok := s.targets[capabilityKey(s.capabilityTenant(tenantID), targetID)]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	for _, tool := range capability.Tools {
		if tool.Name == toolName {
			return true
		}
	}
	if s.store != nil {
		if plugin, ok := s.store.GetForTenant(tenantID, targetID); ok {
			for _, tool := range plugin.Tools {
				if tool.Name == toolName {
					return true
				}
			}
		}
	}
	return false
}

func (s *capabilityState) toolSpec(tenantID, targetID, toolName string) (wire.ToolSpec, bool) {
	if s == nil || toolName == "" {
		return wire.ToolSpec{}, false
	}
	s.mu.RLock()
	capability, ok := s.targets[capabilityKey(s.capabilityTenant(tenantID), targetID)]
	s.mu.RUnlock()
	if !ok {
		return wire.ToolSpec{}, false
	}
	for _, tool := range capability.Tools {
		if tool.Name == toolName {
			return cloneToolSpecs([]wire.ToolSpec{tool})[0], true
		}
	}
	return wire.ToolSpec{}, false
}

func (s *capabilityState) applyToolsUpdated(tenantID string, plugin manifest.Plugin, update workerwire.WorkerToolsUpdated) (wire.Capability, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	capabilityTenant := s.capabilityTenant(tenantID)
	key := capabilityKey(capabilityTenant, plugin.ID)
	capability, ok := s.targets[key]
	if !ok {
		capability = plugin.Capability()
	}
	if update.Revision <= capability.Revision && sameToolSpecs(capability.Tools, update.Tools) {
		return wire.Capability{}, false
	}
	capability.Target = wire.Target{Kind: wire.TargetKindPlugin, ID: plugin.ID}
	capability.Tenants = []string{capabilityTenant}
	capability.Tools = cloneToolSpecs(update.Tools)
	capability.State = wire.CapabilityStateReady
	capability.Source = wire.CapabilitySourceWorker
	capability.Revision = update.Revision
	capability = cloneCapability(capability)
	s.targets[key] = capability
	return capability, true
}

func (s *capabilityState) reset(target *wire.Target, tenants ...string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	requestedTenants := tenantSet(tenants)
	if target == nil && len(requestedTenants) == 0 {
		s.targets = map[string]wire.Capability{}
		if s.store == nil {
			return
		}
		for _, tenantID := range s.catalogTenants() {
			for _, capability := range s.store.CapabilitiesForTenant(tenantID) {
				s.targets[capabilityKey(tenantID, capability.Target.ID)] = cloneCapability(capability)
			}
		}
		return
	}
	if target == nil {
		for key, capability := range s.targets {
			if len(capability.Tenants) == 0 {
				continue
			}
			if requestedTenants[capability.Tenants[0]] {
				delete(s.targets, key)
			}
		}
		if s.store == nil {
			return
		}
		for _, tenantID := range s.catalogTenants() {
			if !requestedTenants[tenantID] {
				continue
			}
			for _, capability := range s.store.CapabilitiesForTenant(tenantID) {
				s.targets[capabilityKey(tenantID, capability.Target.ID)] = cloneCapability(capability)
			}
		}
		return
	}
	if target.Kind != wire.TargetKindPlugin {
		s.deleteTargetCapabilitiesLocked(target.ID)
		return
	}
	if s.store == nil {
		s.deleteTargetCapabilitiesLocked(target.ID)
		return
	}
	if len(requestedTenants) == 0 {
		s.deleteTargetCapabilitiesLocked(target.ID)
	} else {
		s.deleteTenantTargetCapabilitiesLocked(requestedTenants, target.ID)
	}
	for _, tenantID := range s.catalogTenants() {
		if len(requestedTenants) > 0 && !requestedTenants[tenantID] {
			continue
		}
		if plugin, ok := s.store.GetForTenant(tenantID, target.ID); ok {
			capability := plugin.Capability()
			capability.Tenants = []string{tenantID}
			s.targets[capabilityKey(tenantID, target.ID)] = cloneCapability(capability)
		}
	}
}

func (s *capabilityState) setState(tenantID string, plugin manifest.Plugin, state wire.CapabilityState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	capabilityTenant := s.capabilityTenant(tenantID)
	key := capabilityKey(capabilityTenant, plugin.ID)
	capability, ok := s.targets[key]
	if !ok {
		capability = plugin.Capability()
	}
	capability.Tenants = []string{capabilityTenant}
	capability.State = state
	if state == wire.CapabilityStateManifestLoaded {
		capability.Source = wire.CapabilitySourceManifest
	}
	s.targets[key] = cloneCapability(capability)
}

func (s *capabilityState) readySnapshot(tenantID, targetID string) (wire.Capability, bool) {
	if s == nil {
		return wire.Capability{}, false
	}
	s.mu.RLock()
	capability, ok := s.targets[capabilityKey(s.capabilityTenant(tenantID), targetID)]
	s.mu.RUnlock()
	if !ok || capability.State != wire.CapabilityStateReady {
		return wire.Capability{}, false
	}
	return cloneCapability(capability), true
}

func (s *capabilityState) markReady(tenantID string, plugin manifest.Plugin, ack workerwire.WorkerInitAck) wire.Capability {
	s.mu.Lock()
	defer s.mu.Unlock()
	capabilityTenant := s.capabilityTenant(tenantID)
	key := capabilityKey(capabilityTenant, plugin.ID)
	capability, ok := s.targets[key]
	if !ok {
		capability = plugin.Capability()
	}
	capability.Target = wire.Target{Kind: wire.TargetKindPlugin, ID: plugin.ID}
	capability.Tenants = []string{capabilityTenant}
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
	s.targets[key] = capability
	return capability
}

func (s *capabilityState) catalogTenants() []string {
	if s == nil || s.store == nil {
		return nil
	}
	if tenants := s.store.TenantIDs(); len(tenants) > 0 {
		return tenants
	}
	return []string{"*"}
}

func (s *capabilityState) capabilityTenant(tenantID string) string {
	if s != nil && s.store != nil && len(s.store.TenantIDs()) > 0 {
		return tenantID
	}
	return "*"
}

func capabilityKey(tenantID, targetID string) string {
	if tenantID == "" || tenantID == "*" {
		return targetID
	}
	return tenantID + "\x00" + targetID
}

func (s *capabilityState) deleteTenantTargetCapabilitiesLocked(tenants map[string]bool, targetID string) {
	for key, capability := range s.targets {
		if capability.Target.ID != targetID || len(capability.Tenants) == 0 || !tenants[capability.Tenants[0]] {
			continue
		}
		delete(s.targets, key)
	}
}

func (s *capabilityState) deleteTargetCapabilitiesLocked(targetID string) {
	for key, capability := range s.targets {
		if capability.Target.ID == targetID {
			delete(s.targets, key)
		}
	}
}

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
	s.mu.RLock()
	ids := make([]string, 0, len(s.targets))
	for id := range s.targets {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	sort.Strings(ids)
	out := make([]wire.Capability, 0, len(ids))
	s.mu.RLock()
	for _, id := range ids {
		out = append(out, cloneCapability(s.targets[id]))
	}
	s.mu.RUnlock()
	return out
}

func (s *capabilityState) hasTool(tenantID, targetID, toolName string) bool {
	if s == nil || toolName == "" {
		return false
	}
	s.mu.RLock()
	capability, ok := s.targets[s.capabilityKeyForTarget(tenantID, targetID)]
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
	capability, ok := s.targets[s.capabilityKeyForTarget(tenantID, targetID)]
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

func (s *capabilityState) applyToolsUpdated(tenantID string, plugin manifest.Plugin, update workerwire.WorkerToolsUpdated) ([]wire.Capability, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	capability, ok := s.applyToolsUpdatedLocked(s.capabilityKeyForPlugin(tenantID, plugin), s.capabilityTenantsForPlugin(tenantID, plugin), plugin, update)
	if !ok {
		return nil, false
	}
	return []wire.Capability{capability}, true
}

func (s *capabilityState) applyToolsUpdatedLocked(key string, capabilityTenants []string, plugin manifest.Plugin, update workerwire.WorkerToolsUpdated) (wire.Capability, bool) {
	capability, ok := s.targets[key]
	if !ok {
		capability = plugin.Capability()
	}
	if update.Revision <= capability.Revision && sameToolSpecs(capability.Tools, update.Tools) {
		return wire.Capability{}, false
	}
	capability.Target = wire.Target{Kind: wire.TargetKindPlugin, ID: plugin.ID}
	capability.Tenants = append([]string(nil), capabilityTenants...)
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
		s.rebuildManifestCapabilitiesLocked()
		return
	}
	if target == nil {
		for key, capability := range s.targets {
			if capability.WorkerScope == wire.WorkerScopeRuntime || (len(capability.Tenants) > 0 && requestedTenants[capability.Tenants[0]]) {
				delete(s.targets, key)
			}
		}
		if s.store == nil {
			return
		}
		s.seedManifestCapabilitiesLocked(requestedTenants, false)
		s.seedManifestCapabilitiesLocked(nil, true)
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
	if s.targetIsRuntimeScopedLocked(target.ID) {
		s.deleteTargetCapabilitiesLocked(target.ID)
		s.seedManifestCapabilitiesForTargetLocked(target.ID, nil)
		return
	}
	if len(requestedTenants) == 0 {
		s.deleteTargetCapabilitiesLocked(target.ID)
	} else {
		s.deleteTenantTargetCapabilitiesLocked(requestedTenants, target.ID)
	}
	s.seedManifestCapabilitiesForTargetLocked(target.ID, requestedTenants)
}

func (s *capabilityState) setState(tenantID string, plugin manifest.Plugin, state wire.CapabilityState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setStateLocked(s.capabilityKeyForPlugin(tenantID, plugin), s.capabilityTenantsForPlugin(tenantID, plugin), plugin, state)
}

func (s *capabilityState) setStateLocked(key string, capabilityTenants []string, plugin manifest.Plugin, state wire.CapabilityState) {
	capability, ok := s.targets[key]
	if !ok {
		capability = plugin.Capability()
	}
	capability.Tenants = append([]string(nil), capabilityTenants...)
	capability.State = state
	if state == wire.CapabilityStateManifestLoaded {
		capability.Source = wire.CapabilitySourceManifest
	}
	s.targets[key] = cloneCapability(capability)
}

func (s *capabilityState) snapshot(tenantID, targetID string) (wire.Capability, bool) {
	if s == nil {
		return wire.Capability{}, false
	}
	s.mu.RLock()
	capability, ok := s.targets[s.capabilityKeyForTarget(tenantID, targetID)]
	s.mu.RUnlock()
	if !ok {
		return wire.Capability{}, false
	}
	return cloneCapability(capability), true
}

func (s *capabilityState) readySnapshot(tenantID, targetID string) (wire.Capability, bool) {
	capability, ok := s.snapshot(tenantID, targetID)
	if !ok || capability.State != wire.CapabilityStateReady {
		return wire.Capability{}, false
	}
	return capability, true
}

func (s *capabilityState) markReady(tenantID string, plugin manifest.Plugin, ack workerwire.WorkerInitAck) []wire.Capability {
	s.mu.Lock()
	defer s.mu.Unlock()
	return []wire.Capability{s.markReadyLocked(s.capabilityKeyForPlugin(tenantID, plugin), s.capabilityTenantsForPlugin(tenantID, plugin), plugin, ack)}
}

func (s *capabilityState) markReadyLocked(key string, capabilityTenants []string, plugin manifest.Plugin, ack workerwire.WorkerInitAck) wire.Capability {
	capability, ok := s.targets[key]
	if !ok {
		capability = plugin.Capability()
	}
	capability.Target = wire.Target{Kind: wire.TargetKindPlugin, ID: plugin.ID}
	capability.Tenants = append([]string(nil), capabilityTenants...)
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

func (s *capabilityState) capabilityKeyForTarget(tenantID, targetID string) string {
	if s != nil && s.store != nil {
		if plugin, ok := s.store.GetForTenant(tenantID, targetID); ok && plugin.WorkerScope == wire.WorkerScopeRuntime {
			return capabilityKey("*", targetID)
		}
	}
	return capabilityKey(s.capabilityTenant(tenantID), targetID)
}

func (s *capabilityState) capabilityKeyForPlugin(tenantID string, plugin manifest.Plugin) string {
	if plugin.WorkerScope == wire.WorkerScopeRuntime {
		return capabilityKey("*", plugin.ID)
	}
	return capabilityKey(s.capabilityTenant(tenantID), plugin.ID)
}

func (s *capabilityState) capabilityTenantsForPlugin(tenantID string, plugin manifest.Plugin) []string {
	if plugin.WorkerScope != wire.WorkerScopeRuntime {
		return []string{s.capabilityTenant(tenantID)}
	}
	if tenants := s.runtimeCapabilityTenants(plugin.ID); len(tenants) > 0 {
		return tenants
	}
	if tenant := s.capabilityTenant(tenantID); tenant != "" {
		return []string{tenant}
	}
	return []string{"*"}
}

func (s *capabilityState) runtimeCapabilityTenants(targetID string) []string {
	if s == nil || s.store == nil {
		return nil
	}
	tenants := make([]string, 0, len(s.catalogTenants()))
	for _, tenantID := range s.catalogTenants() {
		plugin, ok := s.store.GetForTenant(tenantID, targetID)
		if !ok || plugin.WorkerScope != wire.WorkerScopeRuntime {
			continue
		}
		tenants = append(tenants, tenantID)
	}
	return tenants
}

func (s *capabilityState) rebuildManifestCapabilitiesLocked() {
	s.targets = map[string]wire.Capability{}
	if s.store == nil {
		return
	}
	s.seedManifestCapabilitiesLocked(nil, false)
}

func (s *capabilityState) seedManifestCapabilitiesLocked(requestedTenants map[string]bool, runtimeOnly bool) {
	if s.store == nil {
		return
	}
	for _, tenantID := range s.catalogTenants() {
		if len(requestedTenants) > 0 && !requestedTenants[tenantID] && !runtimeOnly {
			continue
		}
		for _, capability := range s.store.CapabilitiesForTenant(tenantID) {
			if runtimeOnly && capability.WorkerScope != wire.WorkerScopeRuntime {
				continue
			}
			if !runtimeOnly && capability.WorkerScope == wire.WorkerScopeRuntime {
				continue
			}
			s.seedManifestCapabilityLocked(tenantID, capability)
		}
	}
}

func (s *capabilityState) seedManifestCapabilitiesForTargetLocked(targetID string, requestedTenants map[string]bool) {
	if s.store == nil {
		return
	}
	for _, tenantID := range s.catalogTenants() {
		if len(requestedTenants) > 0 && !requestedTenants[tenantID] {
			continue
		}
		if plugin, ok := s.store.GetForTenant(tenantID, targetID); ok {
			s.seedManifestCapabilityLocked(tenantID, plugin.Capability())
		}
	}
	if s.targetIsRuntimeScopedLocked(targetID) {
		for _, tenantID := range s.catalogTenants() {
			if plugin, ok := s.store.GetForTenant(tenantID, targetID); ok && plugin.WorkerScope == wire.WorkerScopeRuntime {
				s.seedManifestCapabilityLocked(tenantID, plugin.Capability())
			}
		}
	}
}

func (s *capabilityState) seedManifestCapabilityLocked(tenantID string, capability wire.Capability) {
	if capability.WorkerScope == wire.WorkerScopeRuntime {
		key := capabilityKey("*", capability.Target.ID)
		if existing, ok := s.targets[key]; ok {
			existing.Tenants = mergeCapabilityTenants(existing.Tenants, []string{tenantID})
			s.targets[key] = cloneCapability(existing)
			return
		}
		capability.Tenants = []string{tenantID}
		s.targets[key] = cloneCapability(capability)
		return
	}
	capability.Tenants = []string{tenantID}
	s.targets[capabilityKey(tenantID, capability.Target.ID)] = cloneCapability(capability)
}

func (s *capabilityState) targetIsRuntimeScopedLocked(targetID string) bool {
	if s == nil || s.store == nil {
		return false
	}
	for _, tenantID := range s.catalogTenants() {
		plugin, ok := s.store.GetForTenant(tenantID, targetID)
		if ok && plugin.WorkerScope == wire.WorkerScopeRuntime {
			return true
		}
	}
	return false
}

func mergeCapabilityTenants(existing, added []string) []string {
	if len(existing) == 0 {
		return append([]string(nil), added...)
	}
	merged := append([]string(nil), existing...)
	seen := make(map[string]bool, len(existing)+len(added))
	for _, tenantID := range merged {
		seen[tenantID] = true
	}
	for _, tenantID := range added {
		if tenantID == "" || seen[tenantID] {
			continue
		}
		merged = append(merged, tenantID)
		seen[tenantID] = true
	}
	return merged
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

package kernel

import (
	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func (h *Hub) syncRuntimeCapability(runtimeID string, capability runtimeapi.Capability) {
	if h == nil {
		return
	}
	if capability.Target.Kind != wire.TargetKindPlugin {
		return
	}
	h.toolExecMu.Lock()
	defer h.toolExecMu.Unlock()
	h.removeRuntimeTargetToolsLocked(runtimeID, capability.Target, capability.Tenants)
	advertise := capability.State == wire.CapabilityStateReady || capability.State == wire.CapabilityStateManifestLoaded || capability.State == wire.CapabilityStateInitializing
	if !advertise && len(capability.Tools) == 0 {
		return
	}
	tenants := capability.Tenants
	if len(tenants) == 0 {
		tenants = []string{"*"}
	}
	if h.runtimes != nil && len(capability.Tenants) == 0 {
		tenants = h.runtimes.TenantsServed(runtimeID)
	}
	for _, tool := range capability.Tools {
		if tool.Name == "" {
			continue
		}
		for _, tenantID := range tenants {
			key := toolExecKey(tenantID, tool.Name)
			if existing, ok := h.toolExec[key]; ok && (existing.RuntimeID != runtimeID || !sameRuntimeTarget(existing.Target, capability.Target)) {
				h.Logger.Warn("runtime tool shadows existing runtime tool", "tool", tool.Name, "runtime_id", runtimeID, "target", capability.Target.ID, "tenant_id", tenantID)
			}
			dispatch := runtimeDispatch(runtimeID, tenantID, capability.Target, tool.Schema, int(tool.DeadlineMS))
			dispatch.Advertise = advertise
			h.toolExec[key] = dispatch
		}
	}
}

func (h *Hub) broadcastRuntimeCatalogUpdate(capability runtimeapi.Capability) {
	if h == nil || h.sessions == nil || capability.Target.Kind != wire.TargetKindPlugin {
		return
	}
	// A tool-only update must preserve context supplied by already-active prompt
	// hooks. Checking only the capability being updated would send a base init
	// after a later fs/tool update and overwrite a driver's enriched prompt.
	rebuildContext := h.hasRuntimeHook("before_prompt_build")
	h.scheduleRuntimeCatalogRefreshForTenants(capability.Tenants, rebuildContext)
}

type catalogRefreshState struct {
	running        bool
	tenants        map[string]struct{}
	rebuildContext bool
}

// scheduleRuntimeCatalogRefreshForTenants coalesces capability bursts into
// sequential refreshes. A refresh added while one is running gets one follow-up
// pass against the newest runtime hook index instead of competing for hooks.
func (h *Hub) scheduleRuntimeCatalogRefreshForTenants(tenants []string, rebuildContext bool) {
	if h == nil || h.sessions == nil {
		return
	}
	h.catalogRefreshMu.Lock()
	if h.catalogRefresh.tenants == nil {
		h.catalogRefresh.tenants = make(map[string]struct{})
	}
	for _, tenantID := range tenants {
		h.catalogRefresh.tenants[tenantID] = struct{}{}
	}
	if len(tenants) == 0 {
		h.catalogRefresh.tenants["*"] = struct{}{}
	}
	h.catalogRefresh.rebuildContext = h.catalogRefresh.rebuildContext || rebuildContext
	if h.catalogRefresh.running {
		h.catalogRefreshMu.Unlock()
		return
	}
	h.catalogRefresh.running = true
	h.catalogRefreshMu.Unlock()
	go h.runRuntimeCatalogRefreshes()
}

func (h *Hub) runRuntimeCatalogRefreshes() {
	for {
		h.catalogRefreshMu.Lock()
		if len(h.catalogRefresh.tenants) == 0 {
			h.catalogRefresh.running = false
			h.catalogRefreshMu.Unlock()
			return
		}
		tenants := make([]string, 0, len(h.catalogRefresh.tenants))
		for tenantID := range h.catalogRefresh.tenants {
			tenants = append(tenants, tenantID)
		}
		rebuildContext := h.catalogRefresh.rebuildContext
		h.catalogRefresh.tenants = make(map[string]struct{})
		h.catalogRefresh.rebuildContext = false
		h.catalogRefreshMu.Unlock()

		h.broadcastRuntimeCatalogRefreshForTenants(tenants, rebuildContext)
	}
}

func (h *Hub) broadcastRuntimeCatalogRefreshForTenants(tenants []string, rebuildContext bool) {
	if h == nil || h.sessions == nil {
		return
	}
	sessionCount := 0
	clientCount := 0
	for _, sess := range h.sessions.All() {
		if sess == nil || sess.ID == "" || !runtimeServesTenant(tenants, sess.TenantID) {
			continue
		}
		sessionCount++
		tools := h.initToolsJSON(sess.TenantID)
		meta := h.initMetaJSON(sess.TenantID)
		for _, client := range h.sessionClients(sess.TenantID, sess.ID) {
			if client.canReceive(TopicSessionInit) {
				context := sess.GetInitContext()
				clientTools := tools
				if rebuildContext {
					context, clientTools = h.policy.BeforePromptBuild(sess.ID, sess.TenantID, client.Name(), context, tools, meta)
				}
				msg := h.initMessage(context, clientTools, meta)
				client.SendMsg(msg)
				clientCount++
			}
		}
	}
	if sessionCount > 0 || clientCount > 0 {
		h.Logger.Info("runtime catalog refresh broadcast", "tenants", tenants, "sessions", sessionCount, "apps", clientCount)
	}
}

func capabilityHasHook(capability runtimeapi.Capability, event string) bool {
	for _, hook := range capability.Hooks {
		if hook.Event == event {
			return true
		}
	}
	return false
}

func runtimeCapabilitiesHaveHook(capabilities []runtimeapi.Capability, event string) bool {
	for _, capability := range capabilities {
		if capabilityHasHook(capability, event) {
			return true
		}
	}
	return false
}

func (h *Hub) runtimeHasHook(runtimeID string, event string) bool {
	if h == nil || h.runtimes == nil || runtimeID == "" || event == "" {
		return false
	}
	for _, attachment := range h.runtimes.Snapshot() {
		if attachment.ID != runtimeID {
			continue
		}
		return runtimeCapabilitiesHaveHook(attachment.Capabilities, event)
	}
	return false
}

func (h *Hub) hasRuntimeHook(event string) bool {
	if h == nil || h.runtimes == nil || event == "" {
		return false
	}
	for _, attachment := range h.runtimes.Snapshot() {
		if runtimeCapabilitiesHaveHook(attachment.Capabilities, event) {
			return true
		}
	}
	return false
}

func (h *Hub) removeRuntimeTools(runtimeID string) int {
	h.toolExecMu.Lock()
	defer h.toolExecMu.Unlock()
	removed := 0
	for name, entry := range h.toolExec {
		if entry.Source == toolSourceRuntime && entry.RuntimeID == runtimeID {
			delete(h.toolExec, name)
			removed++
		}
	}
	return removed
}

func (h *Hub) removeRuntimeTargetToolsLocked(runtimeID string, target wire.Target, tenants []string) {
	for name, entry := range h.toolExec {
		if entry.Source == toolSourceRuntime && entry.RuntimeID == runtimeID && sameRuntimeTarget(entry.Target, target) && toolDispatchMatchesTenants(entry, tenants) {
			delete(h.toolExec, name)
		}
	}
}

func toolDispatchMatchesTenants(entry toolDispatch, tenants []string) bool {
	if len(tenants) == 0 {
		return true
	}
	if entry.TenantID == "" {
		return true
	}
	for _, tenantID := range tenants {
		if tenantID == "*" || tenantID == entry.TenantID {
			return true
		}
	}
	return false
}

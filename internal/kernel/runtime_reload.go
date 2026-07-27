package kernel

import (
	"context"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func (h *Hub) ReloadAttachedRuntime(ctx context.Context, runtimeID string, target *wire.Target, tenants ...string) (bool, error) {
	conn := h.runtimeConn(runtimeID)
	if conn == nil {
		return false, nil
	}
	previousTenants, replacedTenants := h.runtimes.ReplaceTenantsServed(runtimeID, tenants)
	if replacedTenants {
		h.rebuildHookIndex()
	}
	_, err := conn.Reload(ctx, runtimeapi.ReloadReq{Target: target, Tenants: tenants})
	if err != nil && replacedTenants {
		h.runtimes.ReplaceTenantsServed(runtimeID, previousTenants)
		h.rebuildHookIndex()
	}
	return true, err
}

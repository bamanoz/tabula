package tabula

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bamanoz/tabula/internal/kernel"
	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	runtimehostconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	"github.com/bamanoz/tabula/internal/tenant"
)

func watchReloadTrigger(tabulaHome string, hub *kernel.Hub, stop <-chan struct{}) {
	triggerPath := filepath.Join(tabulaHome, "run", "reload.touch")
	lastMtime := triggerMTime(triggerPath)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		mt := triggerMTime(triggerPath)
		if mt.IsZero() || mt.Equal(lastMtime) {
			continue
		}
		lastMtime = mt
		reloadTenant := reloadTriggerTenant(triggerPath)
		slog.Info("reload trigger fired", "path", triggerPath, "tenant_hint", reloadTenant)
		reloadCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := reloadRuntimeFromConfig(reloadCtx, tabulaHome, hub)
		cancel()
		if err != nil {
			slog.Error("runtime reload failed", "error", err)
			continue
		}
		slog.Info("runtime reload complete")
	}
}

func reloadRuntimeFromConfig(ctx context.Context, tabulaHome string, hub *kernel.Hub) error {
	tenantStore := tenant.NewFSStore(tabulaHome)
	hub.SetTenantStore(tenantStore)
	if err := configureKernelRuntimeRegistry(tabulaHome, hub, tenantStore); err != nil {
		return fmt.Errorf("reload runtime registry config: %w", err)
	}
	if err := configureTenantInitMeta(tabulaHome, hub); err != nil {
		return fmt.Errorf("reload tenant init meta: %w", err)
	}
	return reloadLocalRuntime(ctx, hub, reloadTenants(tabulaHome)...)
}

func watchRuntimeTokenRevocations(store *runtimeauth.FileStore, hub *kernel.Hub, stop <-chan struct{}) {
	if store == nil || hub == nil {
		return
	}
	seen := map[string]time.Time{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		for _, item := range store.List() {
			if item.RevokedAt.IsZero() {
				continue
			}
			if seen[item.RuntimeID].Equal(item.RevokedAt) {
				continue
			}
			seen[item.RuntimeID] = item.RevokedAt
			hub.DetachRuntimeForRevoke(item.RuntimeID)
		}
	}
}

type runtimeReloader interface {
	ReloadAttachedRuntime(context.Context, string, *wire.Target, ...string) (bool, error)
}

func reloadLocalRuntime(ctx context.Context, reloader runtimeReloader, tenants ...string) error {
	if reloader == nil {
		return fmt.Errorf("runtime reloader is required")
	}
	attempted, err := reloader.ReloadAttachedRuntime(ctx, runtimeauth.LocalRuntimeID, nil, tenants...)
	if err != nil {
		return err
	}
	if !attempted {
		return fmt.Errorf("local runtime is not attached")
	}
	return nil
}

func reloadTenants(tabulaHome string) []string {
	cfg, err := runtimehostconfig.Load(filepath.Join(tabulaHome, "config", "runtime.toml"))
	if err != nil || len(cfg.Kernels) != 1 {
		return nil
	}
	for _, tenantID := range cfg.Kernels[0].Tenants {
		if tenantID == "*" {
			return nil
		}
	}
	return append([]string(nil), cfg.Kernels[0].Tenants...)
}

func reloadTriggerTenants(triggerPath, tabulaHome string) []string {
	_ = triggerPath
	return reloadTenants(tabulaHome)
}

func reloadTriggerTenant(triggerPath string) string {
	data, err := os.ReadFile(triggerPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.TrimSpace(key) != "tenant" {
			continue
		}
		return strings.TrimSpace(value)
	}
	return ""
}

func triggerMTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

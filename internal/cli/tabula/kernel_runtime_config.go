package tabula

import (
	"fmt"

	"github.com/bamanoz/tabula/internal/kernel"
	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/registryconfig"
	"github.com/bamanoz/tabula/internal/tenant"
)

func configureKernelRuntimeRegistry(tabulaHome string, hub *kernel.Hub, store tenant.Store, managedLocal bool) error {
	definitions, bindings, err := loadKernelRuntimeRegistryConfig(tabulaHome, store, managedLocal)
	if err != nil {
		return err
	}
	return hub.ConfigureRuntimeRegistry(definitions, bindings)
}

func loadKernelRuntimeRegistryConfig(tabulaHome string, store tenant.Store, managedLocal bool) ([]runtimeconfig.Definition, map[string]runtimeconfig.Binding, error) {
	definitions, err := runtimeconfig.LoadDefinitions(tabulaHome)
	if err != nil {
		return nil, nil, err
	}
	runtimeIDs := make(map[string]struct{}, len(definitions)+1)
	for _, definition := range definitions {
		runtimeIDs[definition.ID] = struct{}{}
	}
	if managedLocal {
		if _, ok := runtimeIDs[runtimeauth.LocalRuntimeID]; !ok {
			definitions = append(definitions, runtimeconfig.Definition{ID: runtimeauth.LocalRuntimeID, Backend: "local"})
			runtimeIDs[runtimeauth.LocalRuntimeID] = struct{}{}
		}
	}
	bindings := map[string]runtimeconfig.Binding{}
	if store != nil {
		items, err := store.List()
		if err != nil {
			return nil, nil, fmt.Errorf("list tenants: %w", err)
		}
		for _, item := range items {
			binding, err := runtimeconfig.LoadBinding(tabulaHome, item.ID, runtimeIDs)
			if err != nil {
				return nil, nil, err
			}
			if managedLocal && binding.DefaultRuntime == "" {
				for _, allowed := range binding.AllowedRuntimes {
					if allowed == "*" || allowed == runtimeauth.LocalRuntimeID {
						binding.DefaultRuntime = runtimeauth.LocalRuntimeID
						break
					}
				}
			}
			bindings[item.ID] = binding
		}
	}
	return definitions, bindings, nil
}

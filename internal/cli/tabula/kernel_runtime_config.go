package tabula

import (
	"fmt"

	"github.com/bamanoz/tabula/internal/kernel"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/registryconfig"
	"github.com/bamanoz/tabula/internal/tenant"
)

func configureKernelRuntimeRegistry(tabulaHome string, hub *kernel.Hub, store tenant.Store) error {
	definitions, err := runtimeconfig.LoadDefinitions(tabulaHome)
	if err != nil {
		return err
	}
	runtimeIDs := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		runtimeIDs[definition.ID] = struct{}{}
	}
	bindings := map[string]runtimeconfig.Binding{}
	if store != nil {
		items, err := store.List()
		if err != nil {
			return fmt.Errorf("list tenants: %w", err)
		}
		for _, item := range items {
			binding, err := runtimeconfig.LoadBinding(tabulaHome, item.ID, runtimeIDs)
			if err != nil {
				return err
			}
			bindings[item.ID] = binding
		}
	}
	return hub.ConfigureRuntimeRegistry(definitions, bindings)
}

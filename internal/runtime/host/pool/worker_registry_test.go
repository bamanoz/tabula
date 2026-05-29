package pool

import (
	"testing"

	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestWorkerRegistryReusesEntryByKey(t *testing.T) {
	registry := newWorkerRegistry()
	k := key{kernelID: "kernel", tenantID: "tenant-a", targetID: "fs"}

	first := registry.entryFor(k)
	second := registry.entryFor(k)
	if first != second {
		t.Fatal("entryFor should return the existing entry for the same key")
	}
	if got := len(registry.entriesSnapshot()); got != 1 {
		t.Fatalf("entries length = %d, want 1", got)
	}
}

func TestWorkerRegistryEvictsByTargetAndTenant(t *testing.T) {
	registry := newWorkerRegistry()
	alphaFS := registry.entryFor(key{kernelID: "kernel", tenantID: "alpha", targetID: "fs"})
	betaFS := registry.entryFor(key{kernelID: "kernel", tenantID: "beta", targetID: "fs"})
	alphaSearch := registry.entryFor(key{kernelID: "kernel", tenantID: "alpha", targetID: "search"})

	evicted := registry.evict(&wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, "alpha")
	if len(evicted) != 1 || evicted[0].entry != alphaFS || evicted[0].target.ID != "fs" {
		t.Fatalf("evicted = %#v, want alpha fs only", evicted)
	}

	remaining := registry.entriesSnapshot()
	if len(remaining) != 2 {
		t.Fatalf("remaining entries = %d, want 2", len(remaining))
	}
	if registry.entryFor(key{kernelID: "kernel", tenantID: "beta", targetID: "fs"}) != betaFS {
		t.Fatal("beta fs entry should remain")
	}
	if registry.entryFor(key{kernelID: "kernel", tenantID: "alpha", targetID: "search"}) != alphaSearch {
		t.Fatal("alpha search entry should remain")
	}
}

func TestWorkerRegistryEvictsAllEntriesForWildcardTenant(t *testing.T) {
	registry := newWorkerRegistry()
	registry.entryFor(key{kernelID: "kernel", tenantID: "alpha", targetID: "fs"})
	registry.entryFor(key{kernelID: "kernel", tenantID: "beta", targetID: "search"})

	evicted := registry.evict(nil, "*")
	if len(evicted) != 2 {
		t.Fatalf("evicted length = %d, want 2", len(evicted))
	}
	if got := len(registry.entriesSnapshot()); got != 0 {
		t.Fatalf("remaining entries = %d, want 0", got)
	}
}

func TestWorkerRegistryClearsOnlyMatchingWorker(t *testing.T) {
	entry := &entry{}
	first := newFakeWorker()
	second := newFakeWorker()
	entry.worker = first

	if !entryHasWorker(entry, first) {
		t.Fatal("entry should report matching worker")
	}
	clearEntryWorker(entry, second)
	if !entryHasWorker(entry, first) {
		t.Fatal("clearing another worker should keep current worker")
	}
	clearEntryWorker(entry, first)
	if entryHasWorker(entry, first) || entry.worker != nil {
		t.Fatal("clearing current worker should remove it")
	}
}

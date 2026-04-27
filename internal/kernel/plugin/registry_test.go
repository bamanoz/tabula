package plugin

import "testing"

func TestRegistryAddGetRemove(t *testing.T) {
	r := NewRegistry()
	if r.Len() != 0 {
		t.Fatalf("empty Len: %d", r.Len())
	}

	a := NewHandle("a", nil)
	b := NewHandle("b", nil)
	if prior := r.Add(a); prior != nil {
		t.Fatalf("first Add must return nil prior, got %v", prior)
	}
	if prior := r.Add(b); prior != nil {
		t.Fatalf("second Add must return nil prior, got %v", prior)
	}
	if r.Len() != 2 {
		t.Fatalf("Len after two Adds: %d", r.Len())
	}
	if got := r.Get("a"); got != a {
		t.Fatalf("Get a: %v", got)
	}
	if got := r.Get("b"); got != b {
		t.Fatalf("Get b: %v", got)
	}
	if got := r.Get("missing"); got != nil {
		t.Fatalf("Get missing: %v", got)
	}

	all := r.All()
	if len(all) != 2 || all[0] != a || all[1] != b {
		t.Fatalf("All registration order: %+v", all)
	}

	if removed := r.Remove("a"); removed != a {
		t.Fatalf("Remove a: %v", removed)
	}
	if r.Len() != 1 {
		t.Fatalf("Len after remove: %d", r.Len())
	}
	if got := r.Remove("missing"); got != nil {
		t.Fatalf("Remove missing: %v", got)
	}
}

func TestRegistryAddReplacesByID(t *testing.T) {
	r := NewRegistry()
	a1 := NewHandle("a", nil)
	a2 := NewHandle("a", nil)

	if prior := r.Add(a1); prior != nil {
		t.Fatalf("first Add prior: %v", prior)
	}
	prior := r.Add(a2)
	if prior != a1 {
		t.Fatalf("second Add must return prior a1, got %v", prior)
	}
	if r.Len() != 1 {
		t.Fatalf("Len after replace: %d", r.Len())
	}
	if got := r.Get("a"); got != a2 {
		t.Fatalf("Get after replace: want a2, got %v", got)
	}
	all := r.All()
	if len(all) != 1 || all[0] != a2 {
		t.Fatalf("All after replace: %+v", all)
	}
}

func TestRegistryAddNilIsNoop(t *testing.T) {
	r := NewRegistry()
	if prior := r.Add(nil); prior != nil {
		t.Fatalf("Add(nil) prior: %v", prior)
	}
	if r.Len() != 0 {
		t.Fatalf("Len after Add(nil): %d", r.Len())
	}
}

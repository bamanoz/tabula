package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	runtimeinstance "github.com/bamanoz/tabula/internal/runtime/instance"
	"github.com/bamanoz/tabula/internal/runtime/paths"
)

func TestLoadLocalRuntimeIDReturnsEmptyWhenMetadataMissing(t *testing.T) {
	paths.SetForTests(t.TempDir())
	t.Cleanup(func() { paths.SetForTests("") })

	runtimeID, err := LoadLocalRuntimeID()
	if err != nil {
		t.Fatalf("LoadLocalRuntimeID: %v", err)
	}
	if runtimeID != "" {
		t.Fatalf("runtime id = %q, want empty", runtimeID)
	}
}

func TestLoadLocalRuntimeIDReadsRuntimeMetadata(t *testing.T) {
	home := t.TempDir()
	paths.SetForTests(home)
	t.Cleanup(func() { paths.SetForTests("") })

	meta, err := runtimeinstance.Ensure(paths.RuntimeInstanceFile(), time.Unix(123, 0).UTC())
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	runtimeID, err := LoadLocalRuntimeID()
	if err != nil {
		t.Fatalf("LoadLocalRuntimeID: %v", err)
	}
	if runtimeID != meta.RuntimeID {
		t.Fatalf("runtime id = %q, want %q", runtimeID, meta.RuntimeID)
	}
}

func TestWithRuntimeAffinityAddsRuntimeIDToMeta(t *testing.T) {
	home := t.TempDir()
	paths.SetForTests(home)
	t.Cleanup(func() { paths.SetForTests("") })

	meta, err := runtimeinstance.Ensure(paths.RuntimeInstanceFile(), time.Unix(123, 0).UTC())
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	raw, err := WithRuntimeAffinity(json.RawMessage(`{"tabula.client_role":"user"}`))
	if err != nil {
		t.Fatalf("WithRuntimeAffinity: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got[RuntimeIDMetaKey] != meta.RuntimeID {
		t.Fatalf("%s = %v, want %q", RuntimeIDMetaKey, got[RuntimeIDMetaKey], meta.RuntimeID)
	}
	if got["tabula.client_role"] != "user" {
		t.Fatalf("tabula.client_role = %v, want user", got["tabula.client_role"])
	}
}

func TestWithRuntimeAffinityLeavesMetaUnchangedWhenMissing(t *testing.T) {
	paths.SetForTests(t.TempDir())
	t.Cleanup(func() { paths.SetForTests("") })

	raw, err := WithRuntimeAffinity(json.RawMessage(`{"tabula.client_role":"user"}`))
	if err != nil {
		t.Fatalf("WithRuntimeAffinity: %v", err)
	}
	if string(raw) != `{"tabula.client_role":"user"}` {
		t.Fatalf("meta = %s", string(raw))
	}
}

func TestWithRuntimeAffinityRejectsInvalidHelloMeta(t *testing.T) {
	home := t.TempDir()
	paths.SetForTests(home)
	t.Cleanup(func() { paths.SetForTests("") })

	path := filepath.Join(home, "run", "runtime-instance.json")
	meta, err := json.Marshal(runtimeinstance.Metadata{Version: runtimeinstance.MetadataVersion, RuntimeID: "rt-1234567890abcdef1234", CreatedAt: time.Unix(123, 0).UTC()})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, meta, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := WithRuntimeAffinity(json.RawMessage(`[]`)); err == nil {
		t.Fatal("expected invalid hello meta error")
	}
}

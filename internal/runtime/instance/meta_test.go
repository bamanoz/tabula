package instance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestEnsureCreatesStableRuntimeMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "runtime-instance.json")
	now := time.Unix(123, 0).UTC()

	meta, err := Ensure(path, now)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if meta.Version != MetadataVersion {
		t.Fatalf("Version = %d", meta.Version)
	}
	if meta.CreatedAt != now {
		t.Fatalf("CreatedAt = %s, want %s", meta.CreatedAt, now)
	}
	if err := wire.ValidateRuntimeID(meta.RuntimeID); err != nil {
		t.Fatalf("RuntimeID %q invalid: %v", meta.RuntimeID, err)
	}
	if !strings.HasPrefix(meta.RuntimeID, "rt-") {
		t.Fatalf("RuntimeID = %q, want rt-*", meta.RuntimeID)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded != meta {
		t.Fatalf("Load = %#v, want %#v", loaded, meta)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("mode = %o, want 644", perm)
	}
}

func TestEnsureReusesExistingRuntimeMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime-instance.json")
	first, err := Ensure(path, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	second, err := Ensure(path, time.Unix(2, 0).UTC())
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if second != first {
		t.Fatalf("second Ensure = %#v, want %#v", second, first)
	}
}

func TestLoadRejectsInvalidRuntimeID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime-instance.json")
	data, err := json.Marshal(Metadata{Version: MetadataVersion, RuntimeID: "bad id", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "runtime_id") {
		t.Fatalf("expected runtime_id validation error, got %v", err)
	}
}

func TestLoadRejectsCorruptMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime-instance.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "parse runtime instance metadata") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestLoadRejectsUnsupportedVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime-instance.json")
	data, err := json.Marshal(Metadata{Version: 99, RuntimeID: "rt-1234567890abcdef1234", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("expected version error, got %v", err)
	}
}

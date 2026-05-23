package kernel

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	runtimemock "github.com/bamanoz/tabula/internal/runtime/mock"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestHubRuntimeAsyncSinkRedactsStructuredPluginLogFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	hub := NewHub(json.RawMessage(`[]`), 3, 5, logger)

	hub.runtimeAsyncSink().PluginLogged("local", wire.PluginLog{
		Op:      wire.OpPluginLog,
		Target:  wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"},
		Level:   "info",
		Message: "plugin ready",
		Fields:  json.RawMessage(`{"token":"super-secret","email":"alice@example.com"}`),
	})

	got := buf.String()
	if !strings.Contains(got, "plugin ready") {
		t.Fatalf("expected log output to keep message, got %q", got)
	}
	if !strings.Contains(got, "fields_redacted=true") {
		t.Fatalf("expected redaction marker in log output, got %q", got)
	}
	for _, forbidden := range []string{"super-secret", "alice@example.com", "fields="} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("log output leaked %q: %q", forbidden, got)
		}
	}
}

func TestBusMessagePreservesMessageEnvelopeFields(t *testing.T) {
	msg := busMessage(TopicMessageUser, "sess-1", json.RawMessage(`{"id":"msg-1","text":"hello","meta":{"source":"sessions"}}`))

	if msg.ID != "msg-1" {
		t.Fatalf("ID = %q, want msg-1", msg.ID)
	}
	if messageText(msg) != "hello" {
		t.Fatalf("Text = %q, want hello", messageText(msg))
	}
	var meta map[string]string
	if err := json.Unmarshal(msg.Meta, &meta); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	if meta["source"] != "sessions" {
		t.Fatalf("meta[source] = %q, want sessions", meta["source"])
	}
}

func TestSnapshotRuntimesSanitizesRuntimeDiagnostics(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	hub.runtimes = NewRuntimeRegistry()
	conn := runtimemock.New()
	if err := hub.runtimes.RegisterHello("local", conn, []wire.Capability{{
		Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"},
		Tools:  []wire.ToolSpec{{Name: "seed"}},
		State:  wire.CapabilityStateManifestLoaded,
		Source: wire.CapabilitySourceManifest,
	}}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}

	if _, _, err := hub.runtimes.ApplyCatalogUpdate("local", wire.CatalogUpdate{
		Op:         wire.OpCatalogUpdate,
		Target:     wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"},
		Tools:      []wire.ToolSpec{{Name: "echo"}},
		Revision:   1,
		State:      wire.CapabilityStateReady,
		Source:     wire.CapabilitySourceWorker,
		Diagnostic: "token=super-secret",
	}); err != nil {
		t.Fatalf("ApplyCatalogUpdate: %v", err)
	}
	assertSnapshotRuntimeDiagnostic(t, hub.SnapshotRuntimes(), "ready")

	if _, _, err := hub.runtimes.ApplyLifecycleNotice("local", wire.LifecycleNotice{
		Op:      wire.OpLifecycleNotice,
		Target:  wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"},
		State:   wire.LifecycleStateCrashed,
		Message: "password=hunter2",
	}); err != nil {
		t.Fatalf("ApplyLifecycleNotice: %v", err)
	}
	assertSnapshotRuntimeDiagnostic(t, hub.SnapshotRuntimes(), "crashed")
}

func assertSnapshotRuntimeDiagnostic(t *testing.T, raw []byte, want string) {
	t.Helper()
	var body struct {
		Runtimes []struct {
			Targets []struct {
				Diagnostic string `json:"diagnostic"`
			} `json:"targets"`
		} `json:"runtimes"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal runtime snapshot: %v", err)
	}
	if len(body.Runtimes) != 1 || len(body.Runtimes[0].Targets) != 1 {
		t.Fatalf("unexpected runtime snapshot: %s", string(raw))
	}
	if got := body.Runtimes[0].Targets[0].Diagnostic; got != want {
		t.Fatalf("diagnostic = %q, want %q (snapshot=%s)", got, want, string(raw))
	}
	for _, forbidden := range []string{"super-secret", "hunter2", "token=", "password="} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("snapshot leaked %q: %s", forbidden, string(raw))
		}
	}
}

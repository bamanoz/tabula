package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	runtimemock "github.com/bamanoz/tabula/internal/runtime/mock"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	"github.com/bamanoz/tabula/internal/tenant"
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

func TestPickRuntimeForSessionLogsPreferredRuntimeFallbackReason(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	hub := NewHub(json.RawMessage(`[]`), 3, 5, logger)
	hub.runtimes = NewRuntimeRegistry()
	if err := hub.runtimes.Configure(
		[]RuntimeDefinition{{ID: "local", Backend: "local"}, {ID: "remote", Backend: "attach"}},
		map[string]TenantRuntimeBinding{"alpha": {AllowedRuntimes: []string{"local", "remote"}, DefaultRuntime: "local"}},
	); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if err := hub.runtimes.RegisterHello("local", runtimemock.New(), nil, 0, []string{"alpha"}); err != nil {
		t.Fatalf("RegisterHello local: %v", err)
	}
	hub.sessions.GetOrCreate("s1", "alpha").BindPreferredRuntime("remote")

	conn, runtimeID, code, err := hub.pickRuntimeForSession("alpha", "s1")
	if err != nil {
		t.Fatalf("pickRuntimeForSession: %v", err)
	}
	if conn == nil || runtimeID != "local" || code != "" {
		t.Fatalf("unexpected runtime pick: conn=%v runtimeID=%q code=%q", conn != nil, runtimeID, code)
	}
	got := buf.String()
	for _, want := range []string{
		"preferred runtime fallback",
		"preferred_runtime_id=remote",
		"selected_runtime_id=local",
		"reason_code=runtime_unavailable",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected log output to contain %q, got %q", want, got)
		}
	}
	if !strings.Contains(got, `reason="runtime \"remote\" is unavailable"`) {
		t.Fatalf("expected fallback reason in log output, got %q", got)
	}
}

func TestRuntimeCatalogUpdateRefreshesToolsWithoutPromptHooks(t *testing.T) {
	env := newTestEnv(t)
	driver := env.connectAndJoin("driver", "s1", []string{}, []string{TopicSessionInit})
	_ = readMsg(t, driver) // initial init

	promptHook := env.connectHook("prompt", []HookSubscription{{Event: "before_prompt_build", Priority: 100}})

	go env.Hub.broadcastRuntimeCatalogUpdate(runtimeapi.Capability{
		Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"},
		Tools:  []wire.ToolSpec{{Name: "fs_read"}},
		State:  wire.CapabilityStateReady,
	})

	init := readMsg(t, driver)
	if !isSessionInit(&init) {
		t.Fatalf("expected session.init, got %+v", init)
	}
	if init.Context != "" {
		t.Fatalf("catalog refresh should not rebuild prompt context, got %q", init.Context)
	}
	if msg := readMsgTimeout(t, promptHook, 100*time.Millisecond); msg != nil {
		t.Fatalf("catalog refresh should not dispatch prompt hook, got %+v", msg)
	}
}

func TestRuntimePromptHookCatalogUpdateRefreshesPromptContext(t *testing.T) {
	env := newTestEnv(t)
	driver := env.connectAndJoin("driver", "s1", []string{}, []string{TopicSessionInit})
	initial := readMsg(t, driver)
	if !isSessionInit(&initial) || strings.Contains(initial.Context, "runtime prompt context") {
		t.Fatalf("unexpected initial session.init: %+v", initial)
	}

	runtimeConn := runtimemock.New()
	if err := env.Hub.runtimes.RegisterHello("local", runtimeConn, nil, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		event, ok := runtimeConn.WaitHookEvent(ctx)
		if !ok {
			return
		}
		payload, _ := json.Marshal(map[string]any{"context": "runtime prompt context"})
		_ = env.Hub.runtimeAsyncSink().HookEventReplied("local", wire.HookEventReply{
			Op:     wire.OpHookEventReply,
			CallID: event.CallID,
			Action: wire.HookActionRewrite,
			Data:   payload,
		})
	}()

	if err := env.Hub.runtimeAsyncSink().CatalogUpdated("local", wire.CatalogUpdate{
		Op:       wire.OpCatalogUpdate,
		Target:   wire.Target{Kind: wire.TargetKindPlugin, ID: "skills"},
		Hooks:    []wire.HookSpec{{Event: "before_prompt_build", Priority: 100}},
		Revision: 1,
		State:    wire.CapabilityStateReady,
		Source:   wire.CapabilitySourceWorker,
	}); err != nil {
		t.Fatalf("CatalogUpdated: %v", err)
	}

	refreshed := readMsg(t, driver)
	if !isSessionInit(&refreshed) {
		t.Fatalf("expected refreshed session.init, got %+v", refreshed)
	}
	if !strings.Contains(refreshed.Context, "runtime prompt context") {
		t.Fatalf("expected prompt hook context refresh, got %q", refreshed.Context)
	}
}

func TestRuntimeToolCatalogUpdatePreservesActivePromptHookContext(t *testing.T) {
	env := newTestEnv(t)
	driver := env.connectAndJoin("driver", "s1", []string{}, []string{TopicSessionInit})
	_ = readMsg(t, driver)

	runtimeConn := runtimemock.New()
	skills := runtimeapi.Capability{
		Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "skills"},
		Hooks:  []wire.HookSpec{{Event: "before_prompt_build", Priority: 100}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	if err := env.Hub.runtimes.RegisterHello("local", runtimeConn, []runtimeapi.Capability{skills}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	env.Hub.rebuildHookIndex()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		event, ok := runtimeConn.WaitHookEvent(ctx)
		if !ok {
			return
		}
		payload, _ := json.Marshal(map[string]any{"context": "## Agent Skills\n\nskills context"})
		_ = env.Hub.runtimeAsyncSink().HookEventReplied("local", wire.HookEventReply{
			Op:     wire.OpHookEventReply,
			CallID: event.CallID,
			Action: wire.HookActionRewrite,
			Data:   payload,
		})
	}()

	env.Hub.broadcastRuntimeCatalogUpdate(runtimeapi.Capability{
		Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"},
		Tools:  []wire.ToolSpec{{Name: "fs_read"}},
		State:  wire.CapabilityStateReady,
	})

	refreshed := readMsg(t, driver)
	if !isSessionInit(&refreshed) {
		t.Fatalf("expected refreshed session.init, got %+v", refreshed)
	}
	if !strings.Contains(refreshed.Context, "## Agent Skills") {
		t.Fatalf("tool-only update dropped active prompt hook context: %q", refreshed.Context)
	}
}

func TestRuntimeCatalogRefreshCoalescesConcurrentPromptBuilds(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	tenantID := tenant.DefaultID
	client := addTenantCaptureClient(t, hub, tenantID, "driver", "s1", []string{TopicSessionInit}, nil)
	timeout := int64(500)
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "skills"}
	capability := runtimeapi.Capability{
		Target: target,
		Hooks: []wire.HookSpec{{
			Event:     "before_prompt_build",
			Priority:  100,
			TimeoutMS: &timeout,
		}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	runtimeConn := runtimemock.New()
	if err := hub.runtimes.RegisterHello("local", runtimeConn, []runtimeapi.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.rebuildHookIndex()

	hub.scheduleRuntimeCatalogRefreshForTenants([]string{tenantID}, true)
	first := waitRuntimePromptHook(t, hub, runtimeConn)

	// These two updates arrive during the in-flight refresh and must share one
	// follow-up pass rather than each competing for the skills target.
	hub.scheduleRuntimeCatalogRefreshForTenants([]string{tenantID}, true)
	hub.scheduleRuntimeCatalogRefreshForTenants([]string{tenantID}, true)
	replyRuntimePromptHook(t, hub, first, "skills context")

	second := waitRuntimePromptHook(t, hub, runtimeConn)
	replyRuntimePromptHook(t, hub, second, "skills context")

	for range 2 {
		msg := waitForMessage(t, client.recvCh)
		if !isSessionInit(msg) || !strings.Contains(msg.Context, "skills context") {
			t.Fatalf("expected refreshed skills context, got %+v", msg)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok := runtimeConn.WaitHookEvent(ctx); ok {
		t.Fatal("concurrent refreshes were not coalesced")
	}
}

func waitRuntimePromptHook(t *testing.T, hub *Hub, runtimeConn *runtimemock.RuntimeConn) runtimeapi.HookEventReq {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, ok := runtimeConn.WaitHookEvent(ctx)
	if !ok {
		t.Fatal("timed out waiting for runtime prompt hook")
	}
	if event.Event != "before_prompt_build" {
		t.Fatalf("hook event = %q, want before_prompt_build", event.Event)
	}
	return event
}

func replyRuntimePromptHook(t *testing.T, hub *Hub, event runtimeapi.HookEventReq, context string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"context": context})
	if err != nil {
		t.Fatalf("marshal hook reply: %v", err)
	}
	if err := hub.runtimeAsyncSink().HookEventReplied("local", wire.HookEventReply{
		Op:     wire.OpHookEventReply,
		CallID: event.CallID,
		Action: wire.HookActionRewrite,
		Data:   payload,
	}); err != nil {
		t.Fatalf("HookEventReplied: %v", err)
	}
}

func TestRuntimeLifecycleNonReadyDoesNotBroadcastTrimmedCatalog(t *testing.T) {
	env := newTestEnv(t)
	driver := env.connectAndJoin("driver", "s1", []string{}, []string{TopicSessionInit})
	_ = readMsg(t, driver) // initial init

	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	if err := env.Hub.runtimes.RegisterHello("local", runtimemock.New(), []runtimeapi.Capability{{
		Target: target,
		Tools:  []wire.ToolSpec{{Name: "fs_read"}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	env.Hub.syncRuntimeCapability("local", runtimeapi.Capability{
		Target: target,
		Tools:  []wire.ToolSpec{{Name: "fs_read"}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	})
	env.Hub.rebuildHookIndex()
	env.Hub.broadcastRuntimeCatalogUpdate(runtimeapi.Capability{Target: target, Tenants: []string{"*"}, State: wire.CapabilityStateReady})
	ready := readMsg(t, driver)
	if !isSessionInit(&ready) || !strings.Contains(string(ready.Tools), `"fs_read"`) {
		t.Fatalf("expected refreshed fs_read init tools, got %+v", ready)
	}

	if err := env.Hub.runtimeAsyncSink().LifecycleNoticed("local", wire.LifecycleNotice{
		Op:      wire.OpLifecycleNotice,
		Target:  target,
		State:   wire.LifecycleStateCrashed,
		Message: "plugin crashed",
	}); err != nil {
		t.Fatalf("LifecycleNoticed: %v", err)
	}

	if msg := readMsgTimeout(t, driver, 100*time.Millisecond); msg != nil {
		t.Fatalf("unexpected session.init after non-ready lifecycle: %+v", msg)
	}
}

func TestSnapshotRuntimesSanitizesRuntimeDiagnostics(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	hub.runtimes = NewRuntimeRegistry()
	ensureRuntimeDefinitionsForTest(t, hub, RuntimeDefinition{ID: "local", Backend: "local"})
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

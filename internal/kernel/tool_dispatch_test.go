package kernel

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	runtimemock "github.com/bamanoz/tabula/internal/runtime/mock"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	"github.com/bamanoz/tabula/internal/tenant"
)

func TestNewHubStartsWithoutDynamicDispatch(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	if _, ok := toolDispatchEntry(hub, "echo"); ok {
		t.Fatal("expected no dynamic dispatch entries before runtime attach")
	}
}

func TestHandleDynamicTool_RuntimeSourceInvokesAttachedRuntime(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	rc := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	rc.OnInvoke("alpha", target, "mcp__echo").Return([]byte(`"ok"`))
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{{Target: target, Tools: []wire.ToolSpec{{Name: "mcp__echo"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", wire.Capability{Target: target, Tools: []wire.ToolSpec{{Name: "mcp__echo"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker})

	c := &Client{
		hub:      hub,
		name:     "test",
		session:  "s1",
		recvCh:   make(chan *Message, 4),
		receives: map[string]bool{TopicToolResult: true},
		sends:    map[string]bool{},
		state:    ClientJoined,
		done:     make(chan struct{}),
	}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.sessions.GetOrCreateTenant("s1", "alpha").AddClient(c.name)

	hub.tools.handleDynamicTool("s1", "tid-rt", "mcp__echo", json.RawMessage(`{}`))
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || msg.Output != "ok" {
		t.Fatalf("unexpected runtime tool result: %+v", msg)
	}
	recorded := rc.RecordedInvokes()
	if len(recorded) != 1 || recorded[0].TenantID != "alpha" || recorded[0].Tool != "mcp__echo" || !sameRuntimeTarget(recorded[0].Target, target) {
		t.Fatalf("unexpected runtime invoke record: %+v", recorded)
	}
}

func TestBusyRuntimeTargetIsSkippedForPromptBuildHooks(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "subagents"}
	capability := wire.Capability{
		Target: target,
		Tools:  []wire.ToolSpec{{Name: "subagent_spawn"}},
		Hooks:  []wire.HookSpec{{Event: "before_prompt_build", Priority: 100}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	rc := runtimemock.New().WithCapabilities(capability)
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)
	hub.rebuildHookIndex()
	if entries := hub.hooks.entries("before_prompt_build"); len(entries) != 1 {
		t.Fatalf("expected runtime hook subscriber, got %+v", entries)
	}

	release := hub.markRuntimeTargetBusy("local", target)
	defer release()
	started := time.Now()
	result, ok := hub.dispatchHook("before_prompt_build", json.RawMessage(`{"context":"base"}`), "s1")
	if !ok {
		t.Fatal("busy runtime target hook should be skipped fail-open")
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("busy hook target was not skipped quickly: %s", elapsed)
	}
	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["context"] != "base" || payload["session"] != "s1" || payload["tenant_id"] != tenant.DefaultID {
		t.Fatalf("unexpected payload: %s", string(result))
	}
}

func TestBusyRuntimeTargetIsNotSkippedForSecurityHooks(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	timeout := int64(20)
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "hook-permissions"}
	capability := wire.Capability{
		Target: target,
		Hooks:  []wire.HookSpec{{Event: "before_tool_call", Priority: 100, TimeoutMS: &timeout}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	}
	rc := runtimemock.New().WithCapabilities(capability)
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{capability}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", capability)
	hub.rebuildHookIndex()

	release := hub.markRuntimeTargetBusy("local", target)
	defer release()
	started := time.Now()
	_, ok := hub.dispatchHook("before_tool_call", json.RawMessage(`{"tool":"fs_read","input":{}}`), "s1")
	elapsed := time.Since(started)
	if ok {
		t.Fatal("busy security hook target should not be skipped fail-open")
	}
	if elapsed > time.Second {
		t.Fatalf("security hook should fail closed quickly in tests, elapsed=%s", elapsed)
	}
}

func TestRuntimeToolsAreScopedByTenant(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	hub.runtimes = NewRuntimeRegistry()
	if err := hub.runtimes.Configure(
		[]RuntimeDefinition{{ID: "alpha-runtime", Backend: "attach"}, {ID: "beta-runtime", Backend: "attach"}},
		map[string]TenantRuntimeBinding{
			"alpha": {AllowedRuntimes: []string{"alpha-runtime"}, DefaultRuntime: "alpha-runtime"},
			"beta":  {AllowedRuntimes: []string{"beta-runtime"}, DefaultRuntime: "beta-runtime"},
		},
	); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	hub.SetTenantStore(tenant.NewMemoryStore(
		tenant.Tenant{ID: "alpha", CreatedAt: time.Now()},
		tenant.Tenant{ID: "beta", CreatedAt: time.Now()},
	))
	alphaRuntime := runtimemock.New()
	betaRuntime := runtimemock.New()
	alphaTarget := wire.Target{Kind: wire.TargetKindPlugin, ID: "alpha-fs"}
	betaTarget := wire.Target{Kind: wire.TargetKindPlugin, ID: "beta-fs"}
	alphaCapability := wire.Capability{Target: alphaTarget, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}
	betaCapability := wire.Capability{Target: betaTarget, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}
	alphaRuntime.WithCapabilities(alphaCapability)
	betaRuntime.WithCapabilities(betaCapability)
	alphaRuntime.OnInvoke("alpha", alphaTarget, "fs_read").Return([]byte(`"alpha-ok"`))
	betaRuntime.OnInvoke("beta", betaTarget, "fs_read").Return([]byte(`"beta-ok"`))
	if err := hub.runtimes.RegisterHello("alpha-runtime", alphaRuntime, []wire.Capability{alphaCapability}, 0, []string{"alpha"}); err != nil {
		t.Fatalf("RegisterHello alpha: %v", err)
	}
	if err := hub.runtimes.RegisterHello("beta-runtime", betaRuntime, []wire.Capability{betaCapability}, 0, []string{"beta"}); err != nil {
		t.Fatalf("RegisterHello beta: %v", err)
	}
	hub.syncRuntimeCapability("alpha-runtime", alphaCapability)
	hub.syncRuntimeCapability("beta-runtime", betaCapability)
	if entry, ok := hub.toolExec[toolExecKey("alpha", "fs_read")]; !ok {
		t.Fatalf("missing alpha scoped tool dispatch: %#v", hub.toolExec)
	} else if !toolExecVisible(entry, "alpha") {
		t.Fatalf("alpha scoped tool dispatch not visible to alpha: %#v", entry)
	}

	alphaTools := string(hub.initToolsJSON("alpha"))
	betaTools := string(hub.initToolsJSON("beta"))
	if strings.Count(alphaTools, "fs_read") != 1 || strings.Count(betaTools, "fs_read") != 1 {
		t.Fatalf("expected tenant init tools to expose fs_read once, alpha=%s beta=%s dispatch=%#v", alphaTools, betaTools, hub.toolExec)
	}

	alphaClient := &Client{hub: hub, name: "alpha-client", session: "s-alpha", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicToolResult: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(alphaClient) {
		t.Fatal("add alpha client failed")
	}
	hub.sessions.GetOrCreateTenant("s-alpha", "alpha").AddClient(alphaClient.name)

	hub.tools.handleDynamicTool("s-alpha", "tid-alpha", "fs_read", json.RawMessage(`{}`))
	msg := waitForMessage(t, alphaClient.recvCh)
	if !isToolResult(msg) || msg.Output != "alpha-ok" {
		t.Fatalf("unexpected alpha result: %+v", msg)
	}
	if len(betaRuntime.RecordedInvokes()) != 0 {
		t.Fatalf("beta runtime should not be invoked for alpha tenant: %+v", betaRuntime.RecordedInvokes())
	}

}

func TestTenantScopedCapabilityUpdateDoesNotEvictSiblingTenantDispatch(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 1, 1, nil)
	hub.SetTenantStore(tenant.NewMemoryStore(
		tenant.Tenant{ID: "alpha", CreatedAt: time.Now()},
		tenant.Tenant{ID: "beta", CreatedAt: time.Now()},
	))
	if err := hub.runtimes.RegisterHello("local", runtimemock.New(), nil, 0, []string{"alpha", "beta"}); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	alpha := wire.Capability{Target: target, Tenants: []string{"alpha"}, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}
	beta := wire.Capability{Target: target, Tenants: []string{"beta"}, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}

	hub.syncRuntimeCapability("local", alpha)
	hub.syncRuntimeCapability("local", beta)

	if _, ok := hub.toolExec[toolExecKey("alpha", "fs_read")]; !ok {
		t.Fatalf("alpha dispatch was evicted: %#v", hub.toolExec)
	}
	if _, ok := hub.toolExec[toolExecKey("beta", "fs_read")]; !ok {
		t.Fatalf("beta dispatch missing: %#v", hub.toolExec)
	}
}

func TestTenantScopedCatalogUpdateExtendsRuntimeTenants(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 1, 1, nil)
	hub.SetTenantStore(tenant.NewMemoryStore(
		tenant.Tenant{ID: "bootstrap", CreatedAt: time.Now()},
		tenant.Tenant{ID: "alpha", CreatedAt: time.Now()},
	))
	if err := hub.runtimes.RegisterHello("local", runtimemock.New(), nil, 0, []string{"bootstrap"}); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	_, ok, err := hub.runtimes.ApplyCatalogUpdate("local", wire.CatalogUpdate{
		Target:   target,
		Tenants:  []string{"alpha"},
		Tools:    []wire.ToolSpec{{Name: "fs_read"}},
		Revision: 1,
		State:    wire.CapabilityStateReady,
		Source:   wire.CapabilitySourceWorker,
	})
	if err != nil {
		t.Fatalf("ApplyCatalogUpdate: %v", err)
	}
	if !ok {
		t.Fatal("expected catalog update to apply")
	}
	hub.syncRuntimeCapability("local", wire.Capability{Target: target, Tenants: []string{"alpha"}, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker})

	if !hub.runtimes.RuntimeAllowedForTenant("alpha", "local") {
		t.Fatalf("runtime did not learn late tenant: %#v", hub.runtimes.Snapshot())
	}
	alphaTools := string(hub.initToolsJSON("alpha"))
	if strings.Count(alphaTools, "fs_read") != 1 {
		t.Fatalf("expected alpha init tools to expose late runtime tool, tools=%s snapshot=%#v dispatch=%#v", alphaTools, hub.runtimes.Snapshot(), hub.toolExec)
	}
}

func TestInitRefreshesAttachedRuntimeCapabilitiesAfterLateTenantAppears(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 1, 1, nil)
	hub.SetTenantStore(tenant.NewMemoryStore(
		tenant.Tenant{ID: "bootstrap", CreatedAt: time.Now()},
		tenant.Tenant{ID: "alpha", CreatedAt: time.Now()},
	))
	bootstrapTarget := wire.Target{Kind: wire.TargetKindPlugin, ID: "bootstrap-fs"}
	alphaTarget := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	bootstrapCapability := wire.Capability{Target: bootstrapTarget, Tenants: []string{"bootstrap"}, Tools: []wire.ToolSpec{{Name: "bootstrap_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker, Revision: 1}
	conn := runtimemock.New().WithCapabilities(
		bootstrapCapability,
	)
	if err := hub.runtimes.RegisterHello("local", conn, []wire.Capability{bootstrapCapability}, 0, []string{"bootstrap"}); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", bootstrapCapability)

	conn.WithCapabilities(
		wire.Capability{Target: bootstrapTarget, Tenants: []string{"bootstrap"}, Tools: []wire.ToolSpec{{Name: "bootstrap_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker, Revision: 1},
		wire.Capability{Target: alphaTarget, Tenants: []string{"alpha"}, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker, Revision: 1},
	)

	alphaTools := string(hub.initToolsJSON("alpha"))
	if strings.Count(alphaTools, "fs_read") != 1 {
		t.Fatalf("expected init to refresh late tenant runtime capability, tools=%s snapshot=%#v dispatch=%#v", alphaTools, hub.runtimes.Snapshot(), hub.toolExec)
	}
}

func TestHandleDynamicTool_FallsBackToTenantDefaultRuntimeForGlobalDispatch(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	hub.runtimes = NewRuntimeRegistry()
	if err := hub.runtimes.Configure(
		[]RuntimeDefinition{{ID: "remote", Backend: "attach"}},
		map[string]TenantRuntimeBinding{"alpha": {AllowedRuntimes: []string{"remote"}, DefaultRuntime: "remote"}},
	); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	remote := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	remote.OnInvoke("alpha", target, "mcp__echo").Return([]byte(`"remote"`))
	if err := hub.runtimes.RegisterHello("remote", remote, nil, 0); err != nil {
		t.Fatalf("RegisterHello remote: %v", err)
	}
	hub.toolExec["mcp__echo"] = runtimeDispatch("", "*", target, nil, 0)

	c := &Client{hub: hub, name: "test", session: "s1", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicToolResult: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.sessions.GetOrCreateTenant("s1", "alpha").AddClient(c.name)

	hub.tools.handleDynamicTool("s1", "tid-rt", "mcp__echo", json.RawMessage(`{}`))
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || msg.Output != "remote" {
		t.Fatalf("unexpected runtime tool result: %+v", msg)
	}
	recorded := remote.RecordedInvokes()
	if len(recorded) != 1 || recorded[0].TenantID != "alpha" {
		t.Fatalf("unexpected remote runtime invokes: %+v", recorded)
	}
}

func TestHandleDynamicToolInvokesRuntimeOwningTool(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	hub.runtimes = NewRuntimeRegistry()
	if err := hub.runtimes.Configure(
		[]RuntimeDefinition{{ID: "local", Backend: "local"}, {ID: "remote", Backend: "attach"}},
		map[string]TenantRuntimeBinding{"alpha": {AllowedRuntimes: []string{"local", "remote"}, DefaultRuntime: "local"}},
	); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	local := runtimemock.New()
	remote := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	local.OnInvoke("alpha", target, "fs_read").Return([]byte(`"local"`))
	remote.OnInvoke("alpha", target, "fs_read").Return([]byte(`"remote"`))
	if err := hub.runtimes.RegisterHello("local", local, nil, 0, []string{"alpha"}); err != nil {
		t.Fatalf("RegisterHello local: %v", err)
	}
	if err := hub.runtimes.RegisterHello("remote", remote, nil, 0, []string{"alpha"}); err != nil {
		t.Fatalf("RegisterHello remote: %v", err)
	}
	hub.syncRuntimeCapability("remote", wire.Capability{Target: target, Tools: []wire.ToolSpec{{Name: "fs_read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker})

	c := &Client{hub: hub, name: "test", session: "s1", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicToolResult: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.sessions.GetOrCreateTenant("s1", "alpha").AddClient(c.name)

	hub.tools.handleDynamicTool("s1", "tid-rt", "fs_read", json.RawMessage(`{}`))
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || msg.Output != "remote" {
		t.Fatalf("unexpected runtime tool result: %+v", msg)
	}
	if len(local.RecordedInvokes()) != 0 {
		t.Fatalf("local default runtime should not be invoked: %+v", local.RecordedInvokes())
	}
	recorded := remote.RecordedInvokes()
	if len(recorded) != 1 || recorded[0].TenantID != "alpha" || !sameRuntimeTarget(recorded[0].Target, target) {
		t.Fatalf("unexpected remote runtime invokes: %+v", recorded)
	}
}

func TestHandleDynamicTool_ReturnsTenantForbiddenWhenDefaultRuntimeDisallowed(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	if err := hub.runtimes.Configure(
		[]RuntimeDefinition{{ID: "local", Backend: "local"}, {ID: "remote", Backend: "attach"}},
		map[string]TenantRuntimeBinding{"alpha": {AllowedRuntimes: []string{"local"}, DefaultRuntime: "remote"}},
	); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	hub.syncRuntimeCapability("remote", wire.Capability{Target: target, Tools: []wire.ToolSpec{{Name: "mcp__echo"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker})
	c := &Client{hub: hub, name: "test", session: "s1", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicToolResult: true}, sends: map[string]bool{}, state: ClientJoined, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.sessions.GetOrCreateTenant("s1", "alpha").AddClient(c.name)

	hub.tools.handleDynamicTool("s1", "tid-rt", "mcp__echo", json.RawMessage(`{}`))
	msg := waitForMessage(t, c.recvCh)
	if !isToolResult(msg) || !strings.Contains(msg.Output, "cannot use runtime") {
		t.Fatalf("unexpected runtime tool result: %+v", msg)
	}
}

func TestJoinBindsSessionTenantAndRejectsUnknownTenant(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	hub.SetTenantStore(tenant.NewMemoryStore(tenant.Tenant{ID: "alpha", CreatedAt: time.Now()}))
	c := &Client{hub: hub, name: "test", recvCh: make(chan *Message, 4), receives: map[string]bool{TopicSessionInit: true}, sends: map[string]bool{}, state: ClientProtocolReady, done: make(chan struct{})}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.handleJoin(c, &Message{Type: string(MsgJoin), Session: "s1", TenantID: "missing"})
	msg := waitForMessage(t, c.recvCh)
	if msg.Type != string(MsgError) || msg.Text != "tenant_unknown" {
		t.Fatalf("unexpected unknown tenant response: %+v", msg)
	}
	hub.handleJoin(c, &Message{Type: string(MsgJoin), Session: "s1", TenantID: "alpha"})
	msg = waitForMessage(t, c.recvCh)
	if msg.Type != string(MsgJoined) || msg.TenantID != "alpha" {
		t.Fatalf("unexpected joined response: %+v", msg)
	}
	if got := hub.sessionTenantID("s1"); got != "alpha" {
		t.Fatalf("session tenant = %q", got)
	}
}

func TestRuntimeSkillCapabilitiesAreIgnored(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), 3, 5, nil)
	rc := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindSkill, ID: "skill:timer"}
	if err := hub.runtimes.RegisterHello("local", rc, nil, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", wire.Capability{
		Target:      target,
		Tools:       []wire.ToolSpec{{Name: "timer_start"}},
		State:       wire.CapabilityStateManifestLoaded,
		Source:      wire.CapabilitySourceManifest,
		WorkerMode:  wire.WorkerModeCold,
		HarnessKind: wire.HarnessKindPython,
	})

	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		hub.toolExecMu.RLock()
		_, ok := hub.toolExec["timer_start"]
		hub.toolExecMu.RUnlock()
		if ok {
			t.Fatal("skill target capability registered executable tool")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

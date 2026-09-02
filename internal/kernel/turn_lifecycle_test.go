package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/bamanoz/tabula/internal/agent"
	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestSummarizeTurnToolCatalogIsOrderIndependent(t *testing.T) {
	first := json.RawMessage(`[{"name":"b","schema":{"type":"object"}},{"name":"a","params":{}}]`)
	second := json.RawMessage(`[{"name":"a","params":{}},{"schema":{"type":"object"},"name":"b"}]`)

	firstTools, firstSchemas, firstDigest := summarizeTurnToolCatalog(first)
	secondTools, secondSchemas, secondDigest := summarizeTurnToolCatalog(second)
	if firstTools != 2 || firstSchemas != 1 {
		t.Fatalf("first summary = %d tools, %d schemas", firstTools, firstSchemas)
	}
	if secondTools != firstTools || secondSchemas != firstSchemas || secondDigest != firstDigest {
		t.Fatalf("summaries differ: first=(%d,%d,%s) second=(%d,%d,%s)", firstTools, firstSchemas, firstDigest, secondTools, secondSchemas, secondDigest)
	}
}

type lifecycleHookProbe struct {
	engine *khooks.Engine
	hooks  []khooks.Subscription
	reply  func(*khooks.Message) (string, json.RawMessage)

	mu       sync.Mutex
	messages []*khooks.Message
}

type lifecycleAuditStore struct {
	memorySessionRecordStore
}

func (s *lifecycleAuditStore) snapshot() []map[string]any {
	records := s.memorySessionRecordStore.snapshot(hookDispatchAuditKind)
	payloads := make([]map[string]any, 0, len(records))
	for _, record := range records {
		var payload map[string]any
		if json.Unmarshal(record.Payload, &payload) == nil {
			payloads = append(payloads, payload)
		}
	}
	return payloads
}

func (p *lifecycleHookProbe) Name() string                 { return "lifecycle-probe" }
func (p *lifecycleHookProbe) Session() string              { return "" }
func (p *lifecycleHookProbe) ServesTenant(string) bool     { return true }
func (p *lifecycleHookProbe) IsConnected() bool            { return true }
func (p *lifecycleHookProbe) IsBusy() bool                 { return false }
func (p *lifecycleHookProbe) Hooks() []khooks.Subscription { return p.hooks }
func (p *lifecycleHookProbe) Done() <-chan struct{}        { return make(chan struct{}) }
func (p *lifecycleHookProbe) SendHook(message *khooks.Message) {
	copy := *message
	copy.Payload = append(json.RawMessage(nil), message.Payload...)
	p.mu.Lock()
	p.messages = append(p.messages, &copy)
	p.mu.Unlock()
	if p.reply == nil {
		return
	}
	action, payload := p.reply(message)
	p.engine.HandleResult(p, &khooks.Message{ID: message.ID, Action: action, Payload: payload})
}

func (p *lifecycleHookProbe) snapshot() []*khooks.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*khooks.Message(nil), p.messages...)
}

func newBeforeTurnHookTestHub(assignment agent.Assignment) *Hub {
	attempt := agent.Attempt{
		ID: assignment.AttemptID, Status: agent.AttemptAssigned, DriverInstanceID: assignment.Fence.DriverInstanceID,
		LeaseID: assignment.Fence.LeaseID, DriverGeneration: assignment.Fence.Generation,
	}
	turn := agent.Turn{ID: assignment.TurnID, Status: agent.TurnPreparing, ActiveAttemptID: assignment.AttemptID, Attempts: []agent.Attempt{attempt}}
	state := agent.NewState()
	state.TenantID = assignment.Key.TenantID
	state.SessionID = assignment.Key.SessionID
	state.Status = agent.SessionOpen
	state.ActiveTurnID = assignment.TurnID
	state.Turns[assignment.TurnID] = turn
	state.Driver = agent.Driver{Status: agent.DriverReady, Fence: assignment.Fence, Generation: assignment.Fence.Generation}
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetAgentSessionRepository(&toolAttemptRepository{record: agent.Record{Key: assignment.Key, State: state}})
	return hub
}

func TestPrepareInputContentRewritesOnlyTextAndPreservesSourceDigest(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	probe := &lifecycleHookProbe{engine: hub.hooks, hooks: []khooks.Subscription{{Event: "before_message"}}}
	probe.reply = func(message *khooks.Message) (string, json.RawMessage) {
		var payload map[string]any
		if err := json.Unmarshal(message.Payload, &payload); err != nil {
			t.Fatalf("decode before_message payload: %v", err)
		}
		payload["text"] = "rewritten"
		payload["injected"] = true
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encode hook reply: %v", err)
		}
		return "modify", raw
	}
	hub.hooks.RebuildIndex([]khooks.Subscriber{probe})

	original := json.RawMessage(`{"text":"original","provider":"openai","history":[{"role":"user","text":"old"}]}`)
	rewritten, sourceDigest, err := hub.prepareInputContent(original, agent.SessionKey{TenantID: "tenant", SessionID: "session"}, "input-1")
	if err != nil {
		t.Fatal(err)
	}
	wantDigest, err := agent.DigestJSON(original)
	if err != nil {
		t.Fatal(err)
	}
	if sourceDigest != wantDigest {
		t.Fatalf("source digest = %q, want %q", sourceDigest, wantDigest)
	}
	var content map[string]any
	if err := json.Unmarshal(rewritten, &content); err != nil {
		t.Fatal(err)
	}
	if content["text"] != "rewritten" || content["provider"] != "openai" || content["injected"] != nil || content["session"] != nil || content["input_id"] != nil {
		t.Fatalf("rewritten content = %#v", content)
	}
}

func TestPrepareTurnContextCarriesPromptToolsAndAttemptContext(t *testing.T) {
	assignment := agent.Assignment{
		Key: agent.SessionKey{TenantID: "tenant", SessionID: "session"}, TurnID: "turn-1", AttemptID: "attempt-1", TurnCorrelationID: "turn-1",
		Input: json.RawMessage(`{"text":"hello","provider":"openai"}`), Fence: agent.Fence{DriverInstanceID: "driver-1", LeaseID: "lease-1", Generation: 7},
	}
	hub := newBeforeTurnHookTestHub(assignment)
	hub.toolsJSON = json.RawMessage(`[{"name":"search","description":"Search","input_schema":{"type":"object"}}]`)
	hub.sessions.GetOrCreate("session", "tenant").SetInitContext("session instructions")
	audit := &lifecycleAuditStore{}
	hub.SetSessionRecordStore(audit)
	probe := &lifecycleHookProbe{engine: hub.hooks, hooks: []khooks.Subscription{{Event: "before_turn"}}}
	probe.reply = func(message *khooks.Message) (string, json.RawMessage) {
		var payload map[string]any
		if err := json.Unmarshal(message.Payload, &payload); err != nil {
			t.Fatalf("decode before_turn payload: %v", err)
		}
		if payload["turn_id"] != "turn-1" || payload["attempt_id"] != "attempt-1" || payload["turn_correlation_id"] != "turn-1" || payload["driver_instance_id"] != "driver-1" || payload["lease_id"] != "lease-1" || payload["driver_generation"] != float64(7) {
			t.Fatalf("before_turn correlation = %#v", payload)
		}
		return "modify", json.RawMessage(`{"context":"recalled memory"}`)
	}
	hub.hooks.RebuildIndex([]khooks.Subscriber{probe})

	prepared, err := hub.prepareTurnContext(assignment)
	if err != nil {
		t.Fatal(err)
	}
	var context struct {
		PromptContext string `json:"prompt_context"`
		TurnContext   string `json:"turn_context"`
		Tools         []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(prepared, &context); err != nil {
		t.Fatalf("decode prepared context: %v", err)
	}
	if context.PromptContext != "session instructions" || context.TurnContext != "recalled memory" || len(context.Tools) != 1 || context.Tools[0].Name != "search" {
		t.Fatalf("prepared context = %#v", context)
	}
	var entry map[string]any
	for _, audit := range audit.snapshot() {
		if audit["hook"] == "before_turn" {
			entry = audit
			break
		}
	}
	if entry == nil {
		t.Fatal("before_turn audit not found")
	}
	if entry["tenant_id"] != "tenant" || entry["session"] != "session" || entry["turn_id"] != "turn-1" || entry["attempt_id"] != "attempt-1" || entry["driver_instance_id"] != "driver-1" || entry["lease_id"] != "lease-1" || entry["driver_generation"] != float64(7) || entry["turn_correlation_id"] != "turn-1" || entry["attempt_correlation_valid"] != true {
		t.Fatalf("before_turn audit correlation = %#v", entry)
	}
}

func TestPrepareTurnContextAppliesBeforePromptBuildToDriverSurface(t *testing.T) {
	assignment := agent.Assignment{
		Key: agent.SessionKey{TenantID: "tenant", SessionID: "session"}, TurnID: "turn-1", AttemptID: "attempt-1", TurnCorrelationID: "turn-1",
		Input: json.RawMessage(`{"text":"hello"}`), Fence: agent.Fence{DriverInstanceID: "driver-1", LeaseID: "lease-1", Generation: 1},
	}
	hub := newBeforeTurnHookTestHub(assignment)
	hub.toolsJSON = json.RawMessage(`[{"name":"visible"},{"name":"hidden"}]`)
	hub.sessions.GetOrCreate("session", "tenant").SetInitContext("base context")
	probe := &lifecycleHookProbe{engine: hub.hooks, hooks: []khooks.Subscription{{Event: "before_prompt_build"}}}
	probe.reply = func(message *khooks.Message) (string, json.RawMessage) {
		var payload struct {
			Client  string            `json:"client"`
			Context string            `json:"context"`
			Tools   []json.RawMessage `json:"tools"`
		}
		if err := json.Unmarshal(message.Payload, &payload); err != nil {
			t.Fatalf("decode before_prompt_build payload: %v", err)
		}
		if payload.Client != "driver:driver-1" || payload.Context != "base context" || len(payload.Tools) != 2 {
			t.Fatalf("before_prompt_build payload = %#v", payload)
		}
		return "modify", json.RawMessage(`{"context":"filtered context","tools":[{"name":"visible"}]}`)
	}
	hub.hooks.RebuildIndex([]khooks.Subscriber{probe})

	prepared, err := hub.prepareTurnContext(assignment)
	if err != nil {
		t.Fatal(err)
	}
	var context struct {
		PromptContext string `json:"prompt_context"`
		Tools         []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(prepared, &context); err != nil {
		t.Fatalf("decode prepared context: %v", err)
	}
	if context.PromptContext != "base context\n\nfiltered context" || len(context.Tools) != 1 || context.Tools[0].Name != "visible" {
		t.Fatalf("prepared context = %#v", context)
	}
}

func TestBeforeTurnRejectsStaleAttemptFenceBeforeDispatch(t *testing.T) {
	assignment := agent.Assignment{
		Key: agent.SessionKey{TenantID: "tenant", SessionID: "session"}, TurnID: "turn-1", AttemptID: "attempt-1", TurnCorrelationID: "turn-1",
		Input: json.RawMessage(`{"text":"hello"}`), Fence: agent.Fence{DriverInstanceID: "driver-1", LeaseID: "lease-1", Generation: 1},
	}
	hub := newBeforeTurnHookTestHub(assignment)
	probe := &lifecycleHookProbe{engine: hub.hooks, hooks: []khooks.Subscription{{Event: "before_turn"}}}
	hub.hooks.RebuildIndex([]khooks.Subscriber{probe})
	assignment.Fence.Generation++

	if _, err := hub.prepareTurnContext(assignment); !errors.Is(err, agent.ErrPermissionDenied) {
		t.Fatalf("prepare turn with stale fence error = %v", err)
	}
	if got := len(probe.snapshot()); got != 0 {
		t.Fatalf("stale before_turn dispatched %d hook messages", got)
	}
}

func TestBeforeTurnSuspendBlocksWithoutLeakingHookBusyState(t *testing.T) {
	assignment := agent.Assignment{
		Key: agent.SessionKey{TenantID: "tenant", SessionID: "session"}, TurnID: "turn-1", AttemptID: "attempt-1", TurnCorrelationID: "turn-1",
		Input: json.RawMessage(`{"text":"hello"}`), Fence: agent.Fence{DriverInstanceID: "driver-1", LeaseID: "lease-1", Generation: 1},
	}
	hub := newBeforeTurnHookTestHub(assignment)
	probe := &lifecycleHookProbe{engine: hub.hooks, hooks: []khooks.Subscription{{Event: "before_turn"}}}
	probe.reply = func(*khooks.Message) (string, json.RawMessage) {
		return "suspend", nil
	}
	hub.hooks.RebuildIndex([]khooks.Subscriber{probe})
	for range 2 {
		if _, err := hub.prepareTurnContext(assignment); !errors.Is(err, agent.ErrPermissionDenied) {
			t.Fatalf("prepare turn error = %v", err)
		}
	}
	if got := len(probe.snapshot()); got != 2 {
		t.Fatalf("before_turn sends = %d, want 2", got)
	}
}

func TestBeforeTurnContextIsCommittedBeforeAssignmentDelivery(t *testing.T) {
	repository := agent.NewMemoryRepository()
	commitDriverSinkSession(t, repository)
	hub := NewHub(json.RawMessage(`[]`), nil)
	conn := newExecutionRecordingConn()
	if err := hub.runtimes.RegisterHello("runtime-a", conn, nil, 0, []string{"tenant"}); err != nil {
		t.Fatal(err)
	}
	hub.SetAgentSessionRepository(repository)
	probe := &lifecycleHookProbe{engine: hub.hooks, hooks: []khooks.Subscription{{Event: "before_turn"}}}
	probe.reply = func(*khooks.Message) (string, json.RawMessage) {
		return "modify", json.RawMessage(`{"context":"durable memory"}`)
	}
	hub.hooks.RebuildIndex([]khooks.Subscriber{probe})
	sink := hubRuntimeAsyncSink{hub: hub}
	ctx := context.Background()
	lease := registerDriverForSinkTest(t, sink)
	readyRequest := wire.DriverReady{Op: wire.OpDriverReady, RequestID: "ready", TenantID: "tenant", SessionID: "session", Fence: lease.Fence}
	ready, err := sink.DriverReady(ctx, "runtime-a", readyRequest)
	assertAcceptedDriverResult(t, ready, err)
	if _, err := hub.inputProcessor.Submit(ctx, agent.InputSubmitRequest{
		Key: agent.SessionKey{TenantID: "tenant", SessionID: "session"}, CommandID: "submit", InputID: "input", Content: json.RawMessage(`{"text":"hello"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := sink.DriverReadyAcknowledged(ctx, "runtime-a", readyRequest); err != nil {
		t.Fatal(err)
	}

	conn.mu.Lock()
	assigns := append([]wire.TurnAssign(nil), conn.assigns...)
	conn.mu.Unlock()
	wantPreparedContext := `{"tools":[],"turn_context":"durable memory"}`
	if len(assigns) != 1 || string(assigns[0].PreparedContext) != wantPreparedContext || assigns[0].CorrelationID == "" {
		t.Fatalf("assignments = %#v", assigns)
	}
	record, err := repository.Load(ctx, agent.SessionKey{TenantID: "tenant", SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	turn := record.State.Turns[record.State.ActiveTurnID]
	attempt, ok := coordinatorAttempt(turn, turn.ActiveAttemptID)
	if !ok || !attempt.PreparedContextSet || string(attempt.PreparedContext) != wantPreparedContext {
		t.Fatalf("durable attempt = %#v", attempt)
	}
}

func TestAfterTurnDispatchesOnceAfterCommittedTerminalOutcome(t *testing.T) {
	hub, repository, sink := newDriverExecutionSinkTest(t)
	probe := &lifecycleHookProbe{engine: hub.hooks, hooks: []khooks.Subscription{{Event: "after_turn"}}}
	hub.hooks.RebuildIndex([]khooks.Subscriber{probe})
	ctx := context.Background()
	lease := registerDriverForSinkTest(t, sink)
	ready, err := sink.DriverReady(ctx, "runtime-a", wire.DriverReady{Op: wire.OpDriverReady, RequestID: "ready", TenantID: "tenant", SessionID: "session", Fence: lease.Fence})
	assertAcceptedDriverResult(t, ready, err)
	_, assignment, execution := assignDriverTurnForSinkTest(t, repository)
	if _, err := execution.Prepare(ctx, assignment.Key, "runtime-a", assignment.TurnID, assignment.AttemptID, agentDriverFence(lease.Fence)); err != nil {
		t.Fatal(err)
	}
	if _, err := execution.Permit(ctx, assignment.Key, assignment.TurnID, assignment.AttemptID, agentDriverFence(lease.Fence)); err != nil {
		t.Fatal(err)
	}
	request := wire.TurnCompleted{Op: wire.OpTurnCompleted, RequestID: "completed-1", AttemptRef: sinkAttemptRef(assignment, lease.Fence), Sequence: 1}
	first, err := sink.TurnCompleted(ctx, "runtime-a", request)
	assertAcceptedDriverResult(t, first, err)
	duplicate, err := sink.TurnCompleted(ctx, "runtime-a", request)
	assertAcceptedDriverResult(t, duplicate, err)

	messages := probe.snapshot()
	if len(messages) != 1 {
		t.Fatalf("after_turn messages = %d, want 1", len(messages))
	}
	var payload map[string]any
	if err := json.Unmarshal(messages[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != string(agent.TurnCompleted) || payload["turn_correlation_id"] != assignment.TurnID || payload["attempt_id"] != assignment.AttemptID {
		t.Fatalf("after_turn payload = %#v", payload)
	}
}

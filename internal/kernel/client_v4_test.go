package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/agent"
	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	runtimemock "github.com/bamanoz/tabula/internal/runtime/mock"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/registryconfig"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestDecodeClientEnvelopeStrictWireJSON(t *testing.T) {
	t.Parallel()
	valid := []byte(`{"v":4,"kind":"command","op":"input.submit","id":"command-1","tenant_id":"tenant","session_id":"session","data":{"input_id":"input-1","content":{"text":"hello"}},"meta":{"trace_id":"trace-1"}}`)
	envelope, err := DecodeClientEnvelope(valid)
	if err != nil {
		t.Fatalf("DecodeClientEnvelope: %v", err)
	}
	if envelope.V != 4 || envelope.Op != "input.submit" {
		t.Fatalf("decoded envelope = %+v", envelope)
	}

	for _, raw := range []string{
		`{"v":3,"kind":"command","op":"input.submit","id":"x","tenant_id":"t","session_id":"s","data":{}}`,
		`{"v":4,"kind":"command","op":"input.submit","id":"x","tenant_id":"t","session_id":"s","data":{},"extra":true}`,
		`{"v":4,"kind":"event","op":"input.submit","id":"x","tenant_id":"t","session_id":"s","data":{}}`,
		`{"v":4,"kind":"command","op":"input.submit","tenant_id":"t","session_id":"s","data":{}}`,
	} {
		if _, err := DecodeClientEnvelope([]byte(raw)); err == nil {
			t.Fatalf("DecodeClientEnvelope(%s) succeeded", raw)
		}
	}
}

func TestHandleClientEnvelopeOpensAuthenticatedV4Connection(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetClientAuthToken("secret")
	client := &Client{hub: hub, sendCh: make(chan []byte, 2), done: make(chan struct{}), state: ClientSocketConnected}
	opened := handleClientV4(t, hub, client, &ClientEnvelope{
		V: ClientProtocolVersion, Kind: "command", Op: "connection.open", ID: "open-1",
		Data: json.RawMessage(`{"name":"gateway","auth_token":"secret","meta":{"role":"user"}}`),
	})
	if opened.Kind != "result" || opened.Op != "connection.open" || client.name != "gateway" || client.state != ClientProtocolReady {
		t.Fatalf("opened = %+v client=%+v", opened, client)
	}

	unauthenticated := &Client{hub: hub, sendCh: make(chan []byte, 1), done: make(chan struct{}), state: ClientSocketConnected}
	response := handleClientV4(t, hub, unauthenticated, requestEnvelope("query", "session.get", "get-1", `{}`))
	assertClientError(t, response, "authentication_failed")
}

func TestProtocolV4ExtensionTrafficPreservesArbitraryTopics(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	sender := &Client{
		hub: hub, name: "sender", tenantID: "tenant", session: "session",
		sends: map[string]bool{"custom.topic": true}, state: ClientJoined, done: make(chan struct{}),
	}
	receiver := &Client{
		hub: hub, name: "receiver", tenantID: "tenant", session: "session",
		receives: map[string]bool{"custom.topic": true}, state: ClientJoined,
		recvCh: make(chan *BusMessage, 1), done: make(chan struct{}),
	}
	if !hub.addClient(sender) || !hub.addClient(receiver) {
		t.Fatal("failed to register extension clients")
	}
	response, err := hub.handleClientExtensionSend(sender, &ClientEnvelope{
		V: ClientProtocolVersion, Kind: "command", Op: "extension.send", ID: "send-1",
		TenantID: "tenant", SessionID: "session",
		Data: json.RawMessage(`{"type":"event","topic":"custom.topic","data":{"value":1}}`),
	})
	if err != nil || response.Kind != "result" {
		t.Fatalf("response = %+v err=%v", response, err)
	}
	message := waitForMessage(t, receiver.recvCh)
	if message.Topic != "custom.topic" || message.TenantID != "tenant" || message.Session != "session" {
		t.Fatalf("message = %+v", message)
	}

	external := &Client{hub: hub, name: "external", sendCh: make(chan []byte, 1), done: make(chan struct{}), state: ClientProtocolReady}
	if !external.queueMsg(&BusMessage{Type: "event", Topic: "custom.topic", TenantID: "tenant", Session: "session", ID: "event-1", Data: json.RawMessage(`{"value":2}`)}) {
		t.Fatal("queue extension event failed")
	}
	var envelope ClientEnvelope
	if err := json.Unmarshal(<-external.sendCh, &envelope); err != nil {
		t.Fatalf("decode extension envelope: %v", err)
	}
	if envelope.V != 4 || envelope.Kind != "event" || envelope.Op != "extension.event" || envelope.ID != "event-1" {
		t.Fatalf("envelope = %+v data=%s", envelope, envelope.Data)
	}
}

func TestProtocolV4ExtensionSendUsesExplicitEnvelopeRouteWithoutLegacyJoin(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	sender := &Client{
		hub: hub, name: "gateway-web", sends: map[string]bool{"custom.topic": true},
		state: ClientProtocolReady, done: make(chan struct{}),
	}
	receiver := &Client{
		hub: hub, name: "driver", tenantID: "tenant", session: "session",
		receives: map[string]bool{"custom.topic": true}, state: ClientJoined,
		recvCh: make(chan *BusMessage, 1), done: make(chan struct{}),
	}
	if !hub.addClient(sender) || !hub.addClient(receiver) {
		t.Fatal("failed to register extension clients")
	}

	response, err := hub.handleClientExtensionSend(sender, &ClientEnvelope{
		V: ClientProtocolVersion, Kind: "command", Op: "extension.send", ID: "reply-1",
		TenantID: "tenant", SessionID: "session",
		Data: json.RawMessage(`{"type":"event","topic":"custom.topic","id":"event-1","data":{"value":1}}`),
	})
	if err != nil || response.Kind != "result" {
		t.Fatalf("response = %+v err=%v", response, err)
	}
	select {
	case message := <-receiver.recvCh:
		if message.Topic != "custom.topic" || message.TenantID != "tenant" || message.Session != "session" {
			t.Fatalf("message = %+v", message)
		}
	default:
		t.Fatal("protocol v4 extension event was denied because the client had no legacy join binding")
	}
}

func TestHandleClientEnvelopeCreatesDurableSessionIdempotently(t *testing.T) {
	repository := agent.NewMemoryRepository()
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetAgentSessionRepository(repository)
	client := &Client{
		hub: hub, name: "authenticated-user", tenantID: "tenant", session: "session",
		sendCh: make(chan []byte, 4), done: make(chan struct{}), state: ClientJoined,
	}
	request := requestEnvelope("command", "session.create", "create-1", `{"driver_component_id":"driver","agent_spec_revision":"sha256:spec"}`)
	first := handleClientV4(t, hub, client, request)
	if first.Kind != "result" || first.Op != "session.create" {
		t.Fatalf("first response = %+v data=%s", first, first.Data)
	}
	var created struct {
		SessionVersion uint64 `json:"session_version"`
		Cursor         string `json:"cursor"`
		Duplicate      bool   `json:"duplicate"`
	}
	decodeTestData(t, first.Data, &created)
	if created.SessionVersion != 1 || created.Cursor != "cur_1" || created.Duplicate {
		t.Fatalf("created = %+v", created)
	}

	retry := handleClientV4(t, hub, client, request)
	decodeTestData(t, retry.Data, &created)
	if !created.Duplicate {
		t.Fatalf("retry = %+v data=%s", retry, retry.Data)
	}
	record, err := repository.Load(context.Background(), agent.SessionKey{TenantID: "tenant", SessionID: "session"})
	if err != nil || record.State.DriverComponentID != "driver" || record.State.AgentSpecRevision != "sha256:spec" {
		t.Fatalf("record = %+v err=%v", record, err)
	}
}

type blockingDriverControlConn struct {
	*runtimemock.RuntimeConn
	started chan struct{}
	release chan struct{}
}

func (c *blockingDriverControlConn) PrepareTenant(context.Context, runtimeapi.PrepareTenantReq) error {
	return nil
}

func (c *blockingDriverControlConn) EnsureDriver(ctx context.Context, _ runtimeapi.DriverEnsureReq) error {
	select {
	case c.started <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.release:
		return nil
	}
}

func (c *blockingDriverControlConn) StopDriver(context.Context, runtimeapi.DriverStopReq) error {
	return nil
}

func TestHandleClientEnvelopeSessionCreateDoesNotWaitForDriverEnsure(t *testing.T) {
	repository := agent.NewMemoryRepository()
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetAgentSessionRepository(repository)
	conn := &blockingDriverControlConn{
		RuntimeConn: runtimemock.New(),
		started:     make(chan struct{}),
		release:     make(chan struct{}),
	}
	t.Cleanup(func() { close(conn.release) })
	if err := hub.ConfigureRuntimeRegistry(
		[]runtimeconfig.Definition{{ID: "local", Backend: "local"}},
		map[string]runtimeconfig.Binding{"tenant": {AllowedRuntimes: []string{"local"}, DefaultRuntime: "local"}},
	); err != nil {
		t.Fatalf("configure runtimes: %v", err)
	}
	if err := hub.runtimes.RegisterHello("local", conn, nil, 0, []string{"tenant"}); err != nil {
		t.Fatalf("register runtime: %v", err)
	}
	client := &Client{
		hub: hub, name: "authenticated-user", tenantID: "tenant", session: "session",
		sendCh: make(chan []byte, 1), done: make(chan struct{}), state: ClientJoined,
	}
	handled := make(chan struct{})
	go func() {
		hub.HandleClientEnvelope(client, requestEnvelope(
			"command", "session.create", "create-1",
			`{"driver_component_id":"driver","agent_spec_revision":"sha256:spec"}`,
		))
		close(handled)
	}()

	select {
	case raw := <-client.sendCh:
		var result ClientEnvelope
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("decode response %s: %v", raw, err)
		}
		if result.Kind != "result" || result.Op != "session.create" {
			t.Fatalf("response = %+v data=%s", result, result.Data)
		}
	case <-conn.started:
		t.Fatal("session.create synchronously invoked driver ensure")
	case <-time.After(time.Second):
		t.Fatal("session.create did not return promptly")
	}
	select {
	case <-handled:
	case <-time.After(time.Second):
		t.Fatal("session.create handler did not return")
	}
}

func TestHandleClientEnvelopeListsArchivesAndDeletesSessions(t *testing.T) {
	hub, repository, client := newClientV4Test(t)
	listed := handleClientV4(t, hub, client, &ClientEnvelope{
		V: ClientProtocolVersion, Kind: "query", Op: "session.list", ID: "list-1", TenantID: "tenant", Data: json.RawMessage(`{}`),
	})
	if listed.Kind != "reply" || listed.Op != "session.list" {
		t.Fatalf("listed = %+v data=%s", listed, listed.Data)
	}
	var page struct {
		Sessions []map[string]any `json:"sessions"`
	}
	decodeTestData(t, listed.Data, &page)
	if len(page.Sessions) != 1 || page.Sessions[0]["session_id"] != "session" {
		t.Fatalf("page = %+v", page)
	}

	archived := handleClientV4(t, hub, client, requestEnvelope("command", "session.archive", "archive-1", `{"expected_session_version":1}`))
	if archived.Kind != "result" || archived.Op != "session.archive" {
		t.Fatalf("archived = %+v data=%s", archived, archived.Data)
	}
	record, err := repository.Load(context.Background(), agent.SessionKey{TenantID: "tenant", SessionID: "session"})
	if err != nil || !record.State.Archived || record.Version != 2 {
		t.Fatalf("archived record = %+v err=%v", record, err)
	}

	unarchived := handleClientV4(t, hub, client, requestEnvelope("command", "session.unarchive", "unarchive-1", `{"expected_session_version":2}`))
	if unarchived.Kind != "result" || unarchived.Op != "session.unarchive" {
		t.Fatalf("unarchived = %+v data=%s", unarchived, unarchived.Data)
	}
	archived = handleClientV4(t, hub, client, requestEnvelope("command", "session.archive", "archive-2", `{"expected_session_version":3}`))
	if archived.Kind != "result" {
		t.Fatalf("re-archived = %+v data=%s", archived, archived.Data)
	}
	deleted := handleClientV4(t, hub, client, requestEnvelope("command", "session.delete", "delete-1", `{"expected_session_version":4}`))
	if deleted.Kind != "result" || deleted.Op != "session.delete" {
		t.Fatalf("deleted = %+v data=%s", deleted, deleted.Data)
	}
	record, err = repository.Load(context.Background(), agent.SessionKey{TenantID: "tenant", SessionID: "session"})
	if err != nil || record.State.Status != agent.SessionClosed || record.Version != 5 {
		t.Fatalf("deleted record = %+v err=%v", record, err)
	}
}

func TestHandleClientEnvelopeAppendsAndListsAuxiliarySessionRecords(t *testing.T) {
	hub, repository, client := newClientV4Test(t)
	store := &memorySessionRecordStore{}
	hub.SetSessionRecordStore(store)

	appended := handleClientV4(t, hub, client, requestEnvelope(
		"command", "session.record.append", "record-1", `{"kind":"edit.diff","payload":{"path":"README.md"}}`,
	))
	if appended.Kind != "result" || appended.Op != "session.record.append" {
		t.Fatalf("appended = %+v data=%s", appended, appended.Data)
	}
	var record clientSessionRecord
	decodeTestData(t, appended.Data, &record)
	if record.ID != 1 || record.Kind != "edit.diff" || record.Producer != "client:authenticated-user" || string(record.Payload) != `{"path":"README.md"}` || record.CreatedAt == "" {
		t.Fatalf("record = %+v", record)
	}

	handleClientV4(t, hub, client, requestEnvelope(
		"command", "session.record.append", "record-2", `{"kind":"hook.dispatch.audit","payload":{"hook":"before_tool"}}`,
	))
	listed := handleClientV4(t, hub, client, requestEnvelope(
		"query", "session.record.list", "records-1", `{"kind":"edit.diff","limit":10}`,
	))
	if listed.Kind != "reply" || listed.Op != "session.record.list" {
		t.Fatalf("listed = %+v data=%s", listed, listed.Data)
	}
	var page struct {
		Records []clientSessionRecord `json:"records"`
	}
	decodeTestData(t, listed.Data, &page)
	if len(page.Records) != 1 || page.Records[0].ID != 1 {
		t.Fatalf("page = %+v", page)
	}

	aggregate, err := repository.Load(context.Background(), agent.SessionKey{TenantID: "tenant", SessionID: "session"})
	if err != nil || aggregate.Version != 1 || aggregate.Cursor != 1 {
		t.Fatalf("auxiliary records changed aggregate: record=%+v err=%v", aggregate, err)
	}
}

func TestHandleClientEnvelopeInputAcceptanceIdempotencyAndConflict(t *testing.T) {
	hub, repository, client := newClientV4Test(t)

	first := handleClientV4(t, hub, client, requestEnvelope("command", "input.submit", "command-1", `{"input_id":"input-1","content":{"text":"hello"}}`))
	if first.Kind != "result" || first.Op != "input.accepted" {
		t.Fatalf("first response = %+v data=%s", first, first.Data)
	}
	var accepted clientInputAcceptance
	decodeTestData(t, first.Data, &accepted)
	if accepted.InputID != "input-1" {
		t.Fatalf("acceptance = %+v", accepted)
	}

	retry := handleClientV4(t, hub, client, requestEnvelope("command", "input.submit", "command-1", `{"input_id":"input-1","content":{"text":"hello"}}`))
	var retried clientInputAcceptance
	decodeTestData(t, retry.Data, &retried)
	if retried != accepted {
		t.Fatalf("retry acceptance = %+v, want %+v", retried, accepted)
	}
	record, err := repository.Load(context.Background(), agent.SessionKey{TenantID: "tenant", SessionID: "session"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(record.State.Inputs) != 1 || len(record.State.Turns) != 1 {
		t.Fatalf("record duplicated input: %+v", record.State)
	}
	if record.State.Inputs["input-1"].ActorID != client.name {
		t.Fatalf("input actor_id = %q, want authenticated name %q", record.State.Inputs["input-1"].ActorID, client.name)
	}

	conflict := handleClientV4(t, hub, client, requestEnvelope("command", "input.submit", "command-2", `{"input_id":"input-1","content":{"text":"different"}}`))
	assertClientError(t, conflict, "conflict")
}

func TestHandleClientEnvelopeInputVersionConflictIncludesCurrentVersion(t *testing.T) {
	hub, _, client := newClientV4Test(t)
	response := handleClientV4(t, hub, client, requestEnvelope("command", "input.submit", "command-1", `{"input_id":"input-1","content":{"text":"hello"},"expected_session_version":0}`))
	var data clientErrorData
	decodeTestData(t, response.Data, &data)
	if data.Code != "version_conflict" || data.SessionVersion == nil || *data.SessionVersion != 1 {
		t.Fatalf("error = %+v", data)
	}
}

func TestEncodeClientEventPreparedContextSetIsReplayableWithoutLeakingContext(t *testing.T) {
	typ, payload, err := encodeClientEvent(agent.AttemptPreparedContextSet{
		TurnID:          "turn-1",
		AttemptID:       "attempt-1",
		PreparedContext: json.RawMessage(`{"secret":"hook-owned"}`),
	})
	if err != nil {
		t.Fatalf("encodeClientEvent: %v", err)
	}
	if typ != "turn.state_changed" {
		t.Fatalf("type = %q, want turn.state_changed", typ)
	}
	var data map[string]any
	decodeTestData(t, payload, &data)
	if data["turn_id"] != "turn-1" || data["attempt_id"] != "attempt-1" || data["state"] != "preparing" {
		t.Fatalf("payload = %+v", data)
	}
	if _, ok := data["prepared_context"]; ok {
		t.Fatalf("prepared context leaked to client: %+v", data)
	}
}

func TestHandleClientEnvelopeExpiredCursorRequiresSnapshot(t *testing.T) {
	hub, repository, client := newClientV4Test(t)
	hub.SetAgentSessionRepository(cursorExpiredRepository{SessionRepository: repository})
	response := handleClientV4(t, hub, client, requestEnvelope("query", "session.subscribe", "query-expired", `{"after_cursor":"cur_0"}`))
	var data clientErrorData
	decodeTestData(t, response.Data, &data)
	if data.Code != "cursor_expired" || !data.SnapshotRequired || data.RetainedAfterCursor != "cur_1" || data.Retryable || data.SessionVersion == nil || *data.SessionVersion != 1 {
		t.Fatalf("error = %+v", data)
	}
}

type cursorExpiredRepository struct {
	agent.SessionRepository
}

func (r cursorExpiredRepository) ReadEvents(ctx context.Context, key agent.SessionKey, after agent.Cursor, _ int) ([]agent.StoredEvent, error) {
	record, err := r.Load(ctx, key)
	if err != nil {
		return nil, err
	}
	return nil, &agent.CursorExpiredError{After: after, RetainedAfter: 1, SnapshotCursor: record.Cursor}
}

func TestLegacySessionJSONCannotCreateV4Session(t *testing.T) {
	store := NewDiskSessionStore(t.TempDir())
	legacy := newSession("legacy", "tenant")
	legacy.AddClient("observational-client")
	if err := store.Save(legacy); err != nil {
		t.Fatal(err)
	}
	repository := agent.NewMemoryRepository()
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetAgentSessionRepository(repository)
	hub.SetSessionStore(store)
	client := &Client{hub: hub, name: "authenticated-user", tenantID: "tenant", session: "legacy", sendCh: make(chan []byte, 1), done: make(chan struct{}), state: ClientJoined}
	response := handleClientV4(t, hub, client, &ClientEnvelope{
		V: ClientProtocolVersion, Kind: "query", Op: "session.get", ID: "query-legacy",
		TenantID: "tenant", SessionID: "legacy", Data: json.RawMessage(`{}`),
	})
	assertClientError(t, response, "not_found")
}

func TestHandleClientEnvelopeSnapshotAndBoundedReplay(t *testing.T) {
	hub, repository, client := newClientV4Test(t)
	handleClientV4(t, hub, client, requestEnvelope("command", "input.submit", "command-1", `{"input_id":"input-1","content":{"text":"hello"}}`))

	snapshot := handleClientV4(t, hub, client, requestEnvelope("query", "session.get", "query-1", `{}`))
	var got struct {
		Projection map[string]any `json:"projection"`
		Cursor     string         `json:"cursor"`
	}
	decodeTestData(t, snapshot.Data, &got)
	record, err := repository.Load(context.Background(), agent.SessionKey{TenantID: "tenant", SessionID: "session"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Cursor != "cur_3" || got.Projection["session_version"] != float64(record.Version) || got.Projection["execution_state"] != "queued" {
		t.Fatalf("snapshot = %+v, record = %+v", got, record)
	}

	replay := handleClientV4(t, hub, client, requestEnvelope("query", "session.subscribe", "query-2", `{"after_cursor":"cur_1","limit":1}`))
	var page struct {
		Events []clientStoredEvent `json:"events"`
	}
	decodeTestData(t, replay.Data, &page)
	if len(page.Events) != 1 || page.Events[0].Cursor != "cur_2" || page.Events[0].Type != "input.accepted" || page.Events[0].OccurredAt == "" {
		t.Fatalf("replay = %+v", page)
	}

	badLimit := handleClientV4(t, hub, client, requestEnvelope("query", "session.subscribe", "query-3", `{"after_cursor":"cur_0","limit":257}`))
	assertClientError(t, badLimit, "invalid_argument")
	missingCursor := handleClientV4(t, hub, client, requestEnvelope("query", "session.subscribe", "query-4", `{}`))
	assertClientError(t, missingCursor, "invalid_argument")
}

func TestHandleClientEnvelopeCancellationAndRecoveryAuthority(t *testing.T) {
	hub, repository, client := newClientV4Test(t)
	accepted := handleClientV4(t, hub, client, requestEnvelope("command", "input.submit", "command-1", `{"input_id":"input-1","content":{"text":"hello"}}`))
	var acceptance clientInputAcceptance
	decodeTestData(t, accepted.Data, &acceptance)

	cancelled := handleClientV4(t, hub, client, requestEnvelope("command", "turn.cancel", "cancel-1", `{"turn_id":"`+acceptance.TurnID+`"}`))
	if cancelled.Kind != "result" {
		t.Fatalf("cancel response = %+v data=%s", cancelled, cancelled.Data)
	}
	record, err := repository.Load(context.Background(), agent.SessionKey{TenantID: "tenant", SessionID: "session"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if record.State.Turns[acceptance.TurnID].Status != agent.TurnCancelled {
		t.Fatalf("turn status = %s", record.State.Turns[acceptance.TurnID].Status)
	}

	unauthorized := handleClientV4(t, hub, client, requestEnvelope("command", "turn.retry", "retry-1", `{"turn_id":"`+acceptance.TurnID+`"}`))
	assertClientError(t, unauthorized, "permission_denied")
}

func TestHandleClientEnvelopeRejectsMalformedDataAndCrossScope(t *testing.T) {
	hub, _, client := newClientV4Test(t)
	unknown := handleClientV4(t, hub, client, requestEnvelope("command", "input.submit", "command-1", `{"input_id":"input-1","content":{},"actor_id":"forged"}`))
	assertClientError(t, unknown, "invalid_argument")

	crossTenant := requestEnvelope("query", "session.get", "query-1", `{}`)
	crossTenant.TenantID = "other"
	assertClientError(t, handleClientV4(t, hub, client, crossTenant), "permission_denied")

	crossSession := requestEnvelope("query", "session.get", "query-2", `{}`)
	crossSession.SessionID = "other"
	assertClientError(t, handleClientV4(t, hub, client, crossSession), "permission_denied")
}

func TestHandleClientEnvelopeAcceptsWhenDeliveryRuntimeUnavailable(t *testing.T) {
	hub, repository, client := newClientV4Test(t)
	response := handleClientV4(t, hub, client, requestEnvelope("command", "input.submit", "command-1", `{"input_id":"input-1","content":{"text":"hello"}}`))
	if response.Kind != "result" || response.Op != "input.accepted" {
		t.Fatalf("response = %+v data=%s", response, response.Data)
	}
	key := agent.SessionKey{TenantID: "tenant", SessionID: "session"}
	record, err := repository.Load(context.Background(), key)
	if err != nil || len(record.State.Inputs) != 1 {
		t.Fatalf("durable acceptance record=%+v err=%v", record, err)
	}
	if !client.subscribesAgentSession(key) {
		t.Fatal("input submitter was not subscribed to committed session events")
	}
}

func newClientV4Test(t *testing.T) (*Hub, *agent.MemoryRepository, *Client) {
	t.Helper()
	repository := agent.NewMemoryRepository()
	commitDriverSinkSession(t, repository)
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetAgentSessionRepository(repository)
	client := &Client{
		hub: hub, name: "authenticated-user", tenantID: "tenant", session: "session",
		sendCh: make(chan []byte, 16), done: make(chan struct{}), state: ClientJoined,
	}
	return hub, repository, client
}

func requestEnvelope(kind, op, id, data string) *ClientEnvelope {
	return &ClientEnvelope{V: ClientProtocolVersion, Kind: kind, Op: op, ID: id, TenantID: "tenant", SessionID: "session", Data: json.RawMessage(data)}
}

func handleClientV4(t *testing.T, hub *Hub, client *Client, envelope *ClientEnvelope) ClientEnvelope {
	t.Helper()
	hub.HandleClientEnvelope(client, envelope)
	select {
	case raw := <-client.sendCh:
		var response ClientEnvelope
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatalf("decode response %s: %v", raw, err)
		}
		return response
	default:
		t.Fatal("HandleClientEnvelope sent no response")
		return ClientEnvelope{}
	}
}

func decodeTestData(t *testing.T, raw json.RawMessage, target any) {
	t.Helper()
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("decode data %s: %v", raw, err)
	}
}

func assertClientError(t *testing.T, envelope ClientEnvelope, code string) {
	t.Helper()
	if envelope.Kind != "error" {
		t.Fatalf("response kind = %q, want error; data=%s", envelope.Kind, envelope.Data)
	}
	var data clientErrorData
	decodeTestData(t, envelope.Data, &data)
	if data.Code != code {
		t.Fatalf("error code = %q, want %q; error=%+v", data.Code, code, data)
	}
}

type failingCommitRepository struct {
	agent.SessionRepository
	err error
}

func (r failingCommitRepository) Commit(context.Context, agent.Commit) (agent.CommitResult, error) {
	return agent.CommitResult{}, r.err
}

func TestHandleClientEnvelopeDoesNotAcceptBeforeCommit(t *testing.T) {
	base := agent.NewMemoryRepository()
	commitDriverSinkSession(t, base)
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetAgentSessionRepository(failingCommitRepository{SessionRepository: base, err: errors.New("storage unavailable")})
	client := &Client{hub: hub, name: "user", tenantID: "tenant", session: "session", sendCh: make(chan []byte, 1), done: make(chan struct{}), state: ClientJoined}
	response := handleClientV4(t, hub, client, requestEnvelope("command", "input.submit", "command-1", `{"input_id":"input-1","content":{}}`))
	assertClientError(t, response, "storage_unavailable")
}

func TestHandleClientEnvelopeTriggersCoordinatorDelivery(t *testing.T) {
	hub, repository, sink := newDriverExecutionSinkTest(t)
	lease := registerDriverForSinkTest(t, sink)
	ready, err := sink.DriverReady(context.Background(), "runtime-a", wire.DriverReady{
		Op: wire.OpDriverReady, RequestID: "ready-v4", TenantID: "tenant", SessionID: "session", Fence: lease.Fence,
	})
	assertAcceptedDriverResult(t, ready, err)
	conn, _, err := hub.runtimes.RuntimeForTenant("tenant", "runtime-a")
	if err != nil {
		t.Fatalf("RuntimeForTenant: %v", err)
	}
	recorder := conn.(*executionRecordingConn)
	client := &Client{hub: hub, name: "authenticated-user", tenantID: "tenant", session: "session", sendCh: make(chan []byte, 1), done: make(chan struct{}), state: ClientJoined}

	response := handleClientV4(t, hub, client, requestEnvelope("command", "input.submit", "command-v4", `{"input_id":"input-v4","content":{"text":"hello"}}`))
	if response.Kind != "result" {
		t.Fatalf("response = %+v data=%s", response, response.Data)
	}
	recorder.mu.Lock()
	assigns := append([]wire.TurnAssign(nil), recorder.assigns...)
	recorder.mu.Unlock()
	if len(assigns) != 1 || !json.Valid(assigns[0].Input) {
		t.Fatalf("assignments = %+v", assigns)
	}
	record, err := repository.Load(context.Background(), agent.SessionKey{TenantID: "tenant", SessionID: "session"})
	if err != nil || record.State.ActiveTurnID == "" {
		t.Fatalf("delivery was not durably assigned: record=%+v err=%v", record, err)
	}
}

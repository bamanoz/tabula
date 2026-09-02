package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/bamanoz/tabula/internal/agent"
)

func TestToolAttemptContextRequiresCompleteFence(t *testing.T) {
	_, err := toolAttemptContextFromMeta(json.RawMessage(`{"turn_id":"turn-1","attempt_id":"attempt-1"}`))
	if !errors.Is(err, agent.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument for partial attempt context, got %v", err)
	}
}

func TestValidateToolAttemptRejectsStaleGeneration(t *testing.T) {
	fence := agent.Fence{DriverInstanceID: "driver-1", LeaseID: "lease-1", Generation: 2}
	repository := &toolAttemptRepository{record: permittedToolAttemptRecord(fence)}
	hub := NewHub(nil, nil)
	hub.SetAgentSessionRepository(repository)
	current := toolAttemptContext{TurnID: "turn-1", AttemptID: "attempt-1", DriverInstanceID: fence.DriverInstanceID, LeaseID: fence.LeaseID, DriverGeneration: fence.Generation, TurnCorrelationID: "turn-1"}
	if err := hub.validateToolAttempt("tenant-1", "session-1", current); err != nil {
		t.Fatalf("validate current attempt: %v", err)
	}

	stale := current
	stale.DriverGeneration--
	if err := hub.validateToolAttempt("tenant-1", "session-1", stale); !errors.Is(err, agent.ErrStaleDriver) {
		t.Fatalf("expected stale driver rejection, got %v", err)
	}
}

func TestSendToolResultRejectsCancelledAttempt(t *testing.T) {
	fence := agent.Fence{DriverInstanceID: "driver-1", LeaseID: "lease-1", Generation: 1}
	record := permittedToolAttemptRecord(fence)
	turn := record.State.Turns["turn-1"]
	turn.Status = agent.TurnCancelling
	record.State.Turns["turn-1"] = turn
	hub := NewHub(nil, nil)
	hub.SetAgentSessionRepository(&toolAttemptRepository{record: record})
	receiver := &Client{tenantID: "tenant-1", session: "session-1", recvCh: make(chan *BusMessage, 1), receives: map[string]bool{TopicToolResult: true}, state: ClientJoined, done: make(chan struct{})}
	hub.addClient(receiver)
	correlation := toolAttemptContext{TurnID: "turn-1", AttemptID: "attempt-1", DriverInstanceID: fence.DriverInstanceID, LeaseID: fence.LeaseID, DriverGeneration: fence.Generation, TurnCorrelationID: "turn-1"}

	if hub.sendToolResultForTool("tenant-1", "session-1", "tool-1", "exec", "late", nil, false, correlation) {
		t.Fatal("expected cancelled attempt result to be rejected")
	}
	select {
	case msg := <-receiver.recvCh:
		t.Fatalf("unexpected stale tool result broadcast: %+v", msg)
	default:
	}
}

func TestToolLifecyclePersistsAttemptFence(t *testing.T) {
	store := &memorySessionRecordStore{}
	hub := NewHub(nil, nil)
	hub.SetSessionRecordStore(store)
	correlation := toolAttemptContext{TurnID: "turn-1", AttemptID: "attempt-1", DriverInstanceID: "driver-1", LeaseID: "lease-1", DriverGeneration: 3}

	hub.recordToolStarted("tenant-1", "session-1", "tool-1", "exec", correlation)

	events := readToolLifecycleEvents(t, store)
	if len(events) != 1 {
		t.Fatalf("expected one lifecycle event, got %d", len(events))
	}
	event := events[0]
	if event.TurnID != correlation.TurnID || event.AttemptID != correlation.AttemptID || event.DriverInstanceID != correlation.DriverInstanceID || event.LeaseID != correlation.LeaseID || event.DriverGeneration != correlation.DriverGeneration || event.TurnCorrelationID != correlation.TurnCorrelationID {
		t.Fatalf("attempt fence not persisted: %+v", event)
	}
}

type toolAttemptRepository struct {
	record agent.Record
}

func (r *toolAttemptRepository) Load(context.Context, agent.SessionKey) (agent.Record, error) {
	return r.record, nil
}

func (*toolAttemptRepository) Commit(context.Context, agent.Commit) (agent.CommitResult, error) {
	panic("unexpected Commit")
}

func (*toolAttemptRepository) ReadEvents(context.Context, agent.SessionKey, agent.Cursor, int) ([]agent.StoredEvent, error) {
	panic("unexpected ReadEvents")
}

func (*toolAttemptRepository) ReadOutbox(context.Context, agent.SessionKey, agent.Cursor, int) ([]agent.StoredOutboxMessage, error) {
	panic("unexpected ReadOutbox")
}

func (*toolAttemptRepository) List(context.Context, agent.SessionQuery) (agent.SessionPage, error) {
	panic("unexpected List")
}

func permittedToolAttemptRecord(fence agent.Fence) agent.Record {
	attempt := agent.Attempt{ID: "attempt-1", Status: agent.AttemptPermitted, DriverInstanceID: fence.DriverInstanceID, LeaseID: fence.LeaseID, DriverGeneration: fence.Generation}
	turn := agent.Turn{ID: "turn-1", Status: agent.TurnExecuting, ActiveAttemptID: attempt.ID, Attempts: []agent.Attempt{attempt}}
	state := agent.NewState()
	state.TenantID = "tenant-1"
	state.SessionID = "session-1"
	state.Status = agent.SessionOpen
	state.ActiveTurnID = turn.ID
	state.Turns[turn.ID] = turn
	state.Driver = agent.Driver{Status: agent.DriverReady, Fence: fence, Generation: fence.Generation, RuntimeID: "runtime-1"}
	return agent.Record{Key: agent.SessionKey{TenantID: state.TenantID, SessionID: state.SessionID}, State: state}
}

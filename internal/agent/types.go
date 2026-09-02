package agent

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrInvalidArgument   = errors.New("invalid argument")
	ErrInvalidTransition = errors.New("invalid transition")
	ErrOutputConflict    = errors.New("output conflict")
	ErrCommandConflict   = errors.New("command conflict")
	ErrInputConflict     = errors.New("input conflict")
	ErrStaleDriver       = errors.New("stale driver")
	ErrPermissionDenied  = errors.New("permission denied")
)

type OutputReplayRequiredError struct {
	ExpectedSequence uint64
	ReceivedSequence uint64
}

func (e *OutputReplayRequiredError) Error() string {
	return "output replay required"
}

type SessionStatus string

const (
	SessionOpen      SessionStatus = "open"
	SessionSuspended SessionStatus = "suspended"
	SessionClosed    SessionStatus = "closed"
)

type TurnStatus string

const (
	TurnQueued           TurnStatus = "queued"
	TurnPreparing        TurnStatus = "preparing"
	TurnExecuting        TurnStatus = "executing"
	TurnCancelling       TurnStatus = "cancelling"
	TurnRecoveryRequired TurnStatus = "recovery_required"
	TurnCompleted        TurnStatus = "completed"
	TurnFailed           TurnStatus = "failed"
	TurnCancelled        TurnStatus = "cancelled"
	TurnDiscarded        TurnStatus = "discarded"
)

func (s TurnStatus) Terminal() bool {
	switch s {
	case TurnCompleted, TurnFailed, TurnCancelled, TurnDiscarded:
		return true
	default:
		return false
	}
}

type AttemptStatus string

const (
	AttemptAssigned   AttemptStatus = "assigned"
	AttemptPrepared   AttemptStatus = "prepared"
	AttemptPermitted  AttemptStatus = "permitted"
	AttemptCompleted  AttemptStatus = "completed"
	AttemptFailed     AttemptStatus = "failed"
	AttemptCancelled  AttemptStatus = "cancelled"
	AttemptUncertain  AttemptStatus = "uncertain"
	AttemptAbandoned  AttemptStatus = "abandoned"
	AttemptSuperseded AttemptStatus = "superseded"
)

type DriverStatus string

const (
	DriverAbsent       DriverStatus = "absent"
	DriverInitializing DriverStatus = "initializing"
	DriverReady        DriverStatus = "ready"
	DriverSuspect      DriverStatus = "suspect"
)

type ActorKind string

const (
	ActorClient          ActorKind = "client"
	ActorDriver          ActorKind = "driver"
	ActorKernel          ActorKind = "kernel"
	ActorHuman           ActorKind = "human"
	ActorRecoveryService ActorKind = "recovery_service"
)

type RecoveryAction string

const (
	RecoveryResume  RecoveryAction = "resume"
	RecoveryRetry   RecoveryAction = "retry"
	RecoveryDiscard RecoveryAction = "discard"
)

type CommandMeta struct {
	ID    string    `json:"id"`
	Actor ActorKind `json:"actor"`
}

type Fence struct {
	DriverInstanceID string `json:"driver_instance_id"`
	LeaseID          string `json:"lease_id"`
	Generation       uint64 `json:"generation"`
}

type Input struct {
	ID                string          `json:"id"`
	Content           json.RawMessage `json:"content"`
	Digest            string          `json:"digest"`
	SourceDigest      string          `json:"source_digest"`
	ActorKind         ActorKind       `json:"actor_kind"`
	ActorID           string          `json:"actor_id,omitempty"`
	AcceptanceVersion uint64          `json:"acceptance_version,omitempty"`
	AcceptanceCursor  Cursor          `json:"acceptance_cursor,omitempty"`
}

type AttemptOutput struct {
	Sequence uint64          `json:"sequence"`
	Type     string          `json:"type"`
	Payload  json.RawMessage `json:"payload"`
	Digest   string          `json:"digest"`
}

type Attempt struct {
	ID                     string          `json:"id"`
	Status                 AttemptStatus   `json:"status"`
	DriverInstanceID       string          `json:"driver_instance_id"`
	DriverGeneration       uint64          `json:"driver_generation"`
	LeaseID                string          `json:"lease_id"`
	PreparedContext        json.RawMessage `json:"prepared_context,omitempty"`
	PreparedContextSet     bool            `json:"prepared_context_set"`
	PermitID               string          `json:"permit_id,omitempty"`
	Failure                string          `json:"failure,omitempty"`
	ReconciliationEvidence string          `json:"reconciliation_evidence,omitempty"`
	Outputs                []AttemptOutput `json:"outputs"`
}

type Turn struct {
	ID                    string     `json:"id"`
	Position              uint64     `json:"position"`
	InputID               string     `json:"input_id"`
	Status                TurnStatus `json:"status"`
	Attempts              []Attempt  `json:"attempts"`
	ActiveAttemptID       string     `json:"active_attempt_id,omitempty"`
	CancellationRequested bool       `json:"cancellation_requested,omitempty"`
	RecoveryReason        string     `json:"recovery_reason,omitempty"`
}

type Driver struct {
	Status            DriverStatus `json:"status"`
	Fence             Fence        `json:"fence"`
	Generation        uint64       `json:"generation"`
	RuntimeID         string       `json:"runtime_id,omitempty"`
	ComponentID       string       `json:"component_id,omitempty"`
	AgentSpecRevision string       `json:"agent_spec_revision,omitempty"`
	HeartbeatSequence uint64       `json:"heartbeat_sequence,omitempty"`
	Deadline          time.Time    `json:"deadline,omitempty"`
}

type State struct {
	TenantID          string            `json:"tenant_id"`
	SessionID         string            `json:"session_id"`
	DriverComponentID string            `json:"driver_component_id"`
	AgentSpecRevision string            `json:"agent_spec_revision"`
	Status            SessionStatus     `json:"status"`
	Archived          bool              `json:"archived"`
	Version           uint64            `json:"version"`
	Inputs            map[string]Input  `json:"inputs"`
	Turns             map[string]Turn   `json:"turns"`
	Queue             []string          `json:"queue"`
	NextTurnPosition  uint64            `json:"next_turn_position"`
	ActiveTurnID      string            `json:"active_turn_id,omitempty"`
	Driver            Driver            `json:"driver"`
	AppliedCommands   map[string]string `json:"applied_commands"`
}

func NewState() State {
	return State{
		Inputs:          make(map[string]Input),
		Turns:           make(map[string]Turn),
		AppliedCommands: make(map[string]string),
		Driver:          Driver{Status: DriverAbsent},
	}
}

type Command interface {
	command()
	Metadata() CommandMeta
}

type CreateSession struct {
	Meta              CommandMeta `json:"meta"`
	TenantID          string      `json:"tenant_id"`
	SessionID         string      `json:"session_id"`
	DriverComponentID string      `json:"driver_component_id"`
	AgentSpecRevision string      `json:"agent_spec_revision"`
}

func (CreateSession) command()                {}
func (c CreateSession) Metadata() CommandMeta { return c.Meta }

type ArchiveSession struct {
	Meta CommandMeta `json:"meta"`
}

func (ArchiveSession) command()                {}
func (c ArchiveSession) Metadata() CommandMeta { return c.Meta }

type UnarchiveSession struct {
	Meta CommandMeta `json:"meta"`
}

func (UnarchiveSession) command()                {}
func (c UnarchiveSession) Metadata() CommandMeta { return c.Meta }

type SuspendSession struct {
	Meta CommandMeta `json:"meta"`
}

func (SuspendSession) command()                {}
func (c SuspendSession) Metadata() CommandMeta { return c.Meta }

type ResumeSession struct {
	Meta CommandMeta `json:"meta"`
}

func (ResumeSession) command()                {}
func (c ResumeSession) Metadata() CommandMeta { return c.Meta }

type DeleteSession struct {
	Meta CommandMeta `json:"meta"`
}

func (DeleteSession) command()                {}
func (c DeleteSession) Metadata() CommandMeta { return c.Meta }

type SubmitInput struct {
	Meta              CommandMeta     `json:"meta"`
	InputID           string          `json:"input_id"`
	TurnID            string          `json:"turn_id"`
	Content           json.RawMessage `json:"content"`
	SourceDigest      string          `json:"source_digest"`
	ActorID           string          `json:"actor_id,omitempty"`
	AcceptanceVersion uint64          `json:"acceptance_version,omitempty"`
	AcceptanceCursor  Cursor          `json:"acceptance_cursor,omitempty"`
}

func (SubmitInput) command()                {}
func (c SubmitInput) Metadata() CommandMeta { return c.Meta }

type GrantDriverLease struct {
	Meta              CommandMeta `json:"meta"`
	RuntimeID         string      `json:"runtime_id"`
	ComponentID       string      `json:"component_id"`
	AgentSpecRevision string      `json:"agent_spec_revision"`
	DriverInstanceID  string      `json:"driver_instance_id"`
	LeaseID           string      `json:"lease_id"`
	Deadline          time.Time   `json:"deadline"`
}

func (GrantDriverLease) command()                {}
func (c GrantDriverLease) Metadata() CommandMeta { return c.Meta }

type MarkDriverReady struct {
	Meta  CommandMeta `json:"meta"`
	Fence Fence       `json:"fence"`
}

func (MarkDriverReady) command()                {}
func (c MarkDriverReady) Metadata() CommandMeta { return c.Meta }

type RenewDriverLease struct {
	Meta              CommandMeta `json:"meta"`
	Fence             Fence       `json:"fence"`
	RuntimeID         string      `json:"runtime_id"`
	HeartbeatSequence uint64      `json:"heartbeat_sequence"`
	Deadline          time.Time   `json:"deadline"`
}

func (RenewDriverLease) command()                {}
func (c RenewDriverLease) Metadata() CommandMeta { return c.Meta }

type MarkDriverSuspect struct {
	Meta  CommandMeta `json:"meta"`
	Fence Fence       `json:"fence"`
}

func (MarkDriverSuspect) command()                {}
func (c MarkDriverSuspect) Metadata() CommandMeta { return c.Meta }

type ReleaseDriverLease struct {
	Meta      CommandMeta `json:"meta"`
	Fence     Fence       `json:"fence"`
	RuntimeID string      `json:"runtime_id"`
}

func (ReleaseDriverLease) command()                {}
func (c ReleaseDriverLease) Metadata() CommandMeta { return c.Meta }

type ExpireDriverLease struct {
	Meta CommandMeta `json:"meta"`
	Now  time.Time   `json:"now"`
}

func (ExpireDriverLease) command()                {}
func (c ExpireDriverLease) Metadata() CommandMeta { return c.Meta }

type AssignTurn struct {
	Meta      CommandMeta `json:"meta"`
	TurnID    string      `json:"turn_id"`
	AttemptID string      `json:"attempt_id"`
	Fence     Fence       `json:"fence"`
}

func (AssignTurn) command()                {}
func (c AssignTurn) Metadata() CommandMeta { return c.Meta }

type SetAttemptPreparedContext struct {
	Meta            CommandMeta     `json:"meta"`
	TurnID          string          `json:"turn_id"`
	AttemptID       string          `json:"attempt_id"`
	Fence           Fence           `json:"fence"`
	PreparedContext json.RawMessage `json:"prepared_context,omitempty"`
}

func (SetAttemptPreparedContext) command()                {}
func (c SetAttemptPreparedContext) Metadata() CommandMeta { return c.Meta }

type PrepareAttempt struct {
	Meta      CommandMeta `json:"meta"`
	TurnID    string      `json:"turn_id"`
	AttemptID string      `json:"attempt_id"`
	Fence     Fence       `json:"fence"`
}

func (PrepareAttempt) command()                {}
func (c PrepareAttempt) Metadata() CommandMeta { return c.Meta }

type PermitAttempt struct {
	Meta      CommandMeta `json:"meta"`
	TurnID    string      `json:"turn_id"`
	AttemptID string      `json:"attempt_id"`
	PermitID  string      `json:"permit_id"`
	Fence     Fence       `json:"fence"`
}

func (PermitAttempt) command()                {}
func (c PermitAttempt) Metadata() CommandMeta { return c.Meta }

type CompleteAttempt struct {
	Meta      CommandMeta `json:"meta"`
	TurnID    string      `json:"turn_id"`
	AttemptID string      `json:"attempt_id"`
	Fence     Fence       `json:"fence"`
}

func (CompleteAttempt) command()                {}
func (c CompleteAttempt) Metadata() CommandMeta { return c.Meta }

type AppendAttemptOutput struct {
	Meta      CommandMeta     `json:"meta"`
	TurnID    string          `json:"turn_id"`
	AttemptID string          `json:"attempt_id"`
	Fence     Fence           `json:"fence"`
	Sequence  uint64          `json:"sequence"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

func (AppendAttemptOutput) command()                {}
func (c AppendAttemptOutput) Metadata() CommandMeta { return c.Meta }

type FailAttempt struct {
	Meta      CommandMeta `json:"meta"`
	TurnID    string      `json:"turn_id"`
	AttemptID string      `json:"attempt_id"`
	Fence     Fence       `json:"fence"`
	Reason    string      `json:"reason"`
	Retryable bool        `json:"retryable"`
}

func (FailAttempt) command()                {}
func (c FailAttempt) Metadata() CommandMeta { return c.Meta }

type ReportAttemptUncertain struct {
	Meta                   CommandMeta `json:"meta"`
	TurnID                 string      `json:"turn_id"`
	AttemptID              string      `json:"attempt_id"`
	Fence                  Fence       `json:"fence"`
	Reason                 string      `json:"reason"`
	ReconciliationEvidence string      `json:"reconciliation_evidence,omitempty"`
}

func (ReportAttemptUncertain) command()                {}
func (c ReportAttemptUncertain) Metadata() CommandMeta { return c.Meta }

type CancelTurn struct {
	Meta   CommandMeta `json:"meta"`
	TurnID string      `json:"turn_id"`
}

func (CancelTurn) command()                {}
func (c CancelTurn) Metadata() CommandMeta { return c.Meta }

type ConfirmCancellation struct {
	Meta      CommandMeta `json:"meta"`
	TurnID    string      `json:"turn_id"`
	AttemptID string      `json:"attempt_id"`
	Fence     Fence       `json:"fence"`
}

func (ConfirmCancellation) command()                {}
func (c ConfirmCancellation) Metadata() CommandMeta { return c.Meta }

type RecoverTurn struct {
	Meta                   CommandMeta    `json:"meta"`
	TurnID                 string         `json:"turn_id"`
	Action                 RecoveryAction `json:"action"`
	Fence                  Fence          `json:"fence,omitempty"`
	ReconciliationEvidence string         `json:"reconciliation_evidence,omitempty"`
}

func (RecoverTurn) command()                {}
func (c RecoverTurn) Metadata() CommandMeta { return c.Meta }

type Event interface{ event() }

type Envelope struct {
	CommandID     string `json:"command_id"`
	CommandDigest string `json:"command_digest"`
	Body          Event  `json:"-"`
}

type SessionCreated struct {
	TenantID          string
	SessionID         string
	DriverComponentID string
	AgentSpecRevision string
}

func (SessionCreated) event() {}

type SessionArchived struct{}

func (SessionArchived) event() {}

type SessionUnarchived struct{}

func (SessionUnarchived) event() {}

type SessionSuspendedEvent struct{}

func (SessionSuspendedEvent) event() {}

type SessionResumedEvent struct{}

func (SessionResumedEvent) event() {}

type SessionDeletedEvent struct{}

func (SessionDeletedEvent) event() {}

type InputSubmitted struct {
	Input Input
	Turn  Turn
}

func (InputSubmitted) event() {}

type DriverLeaseGranted struct{ Driver Driver }

func (DriverLeaseGranted) event() {}

type DriverBecameReady struct{}

func (DriverBecameReady) event() {}

type DriverLeaseRenewed struct {
	Deadline          time.Time
	HeartbeatSequence uint64
}

func (DriverLeaseRenewed) event() {}

type DriverBecameSuspect struct{}

func (DriverBecameSuspect) event() {}

type DriverLeaseReleased struct{}

func (DriverLeaseReleased) event() {}

type DriverLeaseExpired struct{}

func (DriverLeaseExpired) event() {}

type AttemptAssignedEvent struct {
	TurnID  string
	Attempt Attempt
}

func (AttemptAssignedEvent) event() {}

type AttemptPreparedContextSet struct {
	TurnID, AttemptID string
	PreparedContext   json.RawMessage
}

func (AttemptPreparedContextSet) event() {}

type AttemptPreparedEvent struct{ TurnID, AttemptID string }

func (AttemptPreparedEvent) event() {}

type AttemptPermittedEvent struct{ TurnID, AttemptID, PermitID string }

func (AttemptPermittedEvent) event() {}

type AttemptOutputAppended struct {
	TurnID, AttemptID string
	Output            AttemptOutput
}

func (AttemptOutputAppended) event() {}

type AttemptCompletedEvent struct{ TurnID, AttemptID string }

func (AttemptCompletedEvent) event() {}

type AttemptFailedEvent struct {
	TurnID, AttemptID, Reason string
	Retryable                 bool
}

func (AttemptFailedEvent) event() {}

type AttemptAbandonedEvent struct{ TurnID, AttemptID, Reason string }

func (AttemptAbandonedEvent) event() {}

type AttemptUncertainEvent struct {
	TurnID, AttemptID, Reason string
	ReconciliationEvidence    string
}

func (AttemptUncertainEvent) event() {}

type TurnCancellationRequested struct{ TurnID string }

func (TurnCancellationRequested) event() {}

type AttemptCancelledEvent struct{ TurnID, AttemptID string }

func (AttemptCancelledEvent) event() {}

type AttemptResumedEvent struct {
	TurnID, AttemptID string
	Fence             Fence
	Evidence          string
}

func (AttemptResumedEvent) event() {}

type AttemptSupersededEvent struct{ TurnID, AttemptID string }

func (AttemptSupersededEvent) event() {}

type TurnDiscardedEvent struct{ TurnID string }

func (TurnDiscardedEvent) event() {}

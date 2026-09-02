package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bamanoz/tabula/internal/agent"
	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

type hubRuntimeAsyncSink struct {
	hub *Hub
}

func (h *Hub) runtimeAsyncSink() runtimeapi.AsyncSink {
	return hubRuntimeAsyncSink{hub: h}
}

func (s hubRuntimeAsyncSink) CatalogUpdated(runtimeID string, update wire.CatalogUpdate) error {
	if s.hub == nil || s.hub.runtimes == nil {
		return fmt.Errorf("kernel runtime sink is unavailable")
	}
	capability, ok, err := s.hub.runtimes.ApplyCatalogUpdate(runtimeID, update)
	if err != nil {
		return err
	}
	if ok {
		s.hub.syncRuntimeCapability(runtimeID, capability)
	}
	s.hub.rebuildHookIndex()
	if ok {
		s.hub.broadcastRuntimeCatalogUpdate(capability)
	}
	return nil
}

func (s hubRuntimeAsyncSink) HookEventReplied(runtimeID string, reply wire.HookEventReply) error {
	if reply.CallID == "" {
		return fmt.Errorf("hook_event_reply call_id is required")
	}
	action, err := kernelHookAction(reply.Action)
	if err != nil {
		return err
	}
	s.hub.hooks.HandleRuntimeResult(runtimeID, &khooks.Message{
		Type:    string(MsgHookReply),
		ID:      reply.CallID,
		Action:  action,
		Payload: reply.Data,
		Reason:  reply.Reason,
	})
	return nil
}

func (s hubRuntimeAsyncSink) PluginSent(runtimeID string, send wire.PluginSend) error {
	if err := send.Target.Validate(); err != nil {
		return err
	}
	if send.Channel != "bus" {
		return fmt.Errorf("plugin_send channel %q is not supported", send.Channel)
	}
	if send.Type == "" {
		return fmt.Errorf("plugin_send type is required")
	}
	tenantID := send.TenantID
	if tenantID == "" {
		tenantID = s.hub.sessionTenantID("", send.SessionID)
	}
	s.hub.broadcastToSession(tenantID, send.SessionID, send.Type, busMessage(send.Type, send.SessionID, send.Payload), nil)
	return nil
}

func (s hubRuntimeAsyncSink) PluginLogged(runtimeID string, log wire.PluginLog) {
	if err := log.Target.Validate(); err != nil {
		s.hub.Logger.Warn("dropping invalid runtime plugin log", "runtime_id", runtimeID, "err", err)
		return
	}
	var level slog.Level
	switch log.Level {
	case "debug":
		level = slog.LevelDebug
	case "info", "":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		s.hub.Logger.Warn("dropping runtime plugin log with unknown level", "runtime_id", runtimeID, "target", log.Target.ID, "level", log.Level)
		return
	}
	attrs := []slog.Attr{
		slog.String("runtime_id", runtimeID),
		slog.String("target_kind", string(log.Target.Kind)),
		slog.String("target", log.Target.ID),
	}
	if len(log.Fields) > 0 {
		attrs = append(attrs, slog.Bool("fields_redacted", true))
	}
	s.hub.Logger.LogAttrs(context.Background(), level, log.Message, attrs...)
	_ = runtimeID
}

func (s hubRuntimeAsyncSink) LifecycleNoticed(runtimeID string, notice wire.LifecycleNotice) error {
	if s.hub == nil || s.hub.runtimes == nil {
		return fmt.Errorf("kernel runtime sink is unavailable")
	}
	capability, ok, err := s.hub.runtimes.ApplyLifecycleNotice(runtimeID, notice)
	if err != nil {
		return err
	}
	if ok {
		s.hub.syncRuntimeCapability(runtimeID, capability)
		s.hub.rebuildHookIndex()
	}
	return nil
}

func (s hubRuntimeAsyncSink) DriverLifecycleNoticed(runtimeID string, lifecycle wire.DriverLifecycle) error {
	if s.hub == nil || s.hub.runtimes == nil {
		return fmt.Errorf("kernel runtime sink is unavailable")
	}
	if !s.hub.runtimes.RuntimeAllowedForTenant(lifecycle.TenantID, runtimeID) {
		return fmt.Errorf("runtime %q cannot report driver lifecycle for tenant %q", runtimeID, lifecycle.TenantID)
	}
	// Process lifecycle is observational until issue 08 binds registration and
	// lease transitions to authenticated runtime/worker identity.
	attrs := []any{
		"runtime_id", runtimeID,
		"tenant_id", lifecycle.TenantID,
		"session_id", lifecycle.SessionID,
		"component_id", lifecycle.ComponentID,
		"agent_spec_revision", lifecycle.AgentSpecRevision,
		"driver_instance_id", lifecycle.DriverInstanceID,
		"generation", lifecycle.DesiredGeneration,
		"state", lifecycle.State,
	}
	if lifecycle.State == wire.DriverLifecycleExited || lifecycle.State == wire.DriverLifecycleStopped {
		attrs = append(attrs, "exit_code", lifecycle.ExitCode)
	}
	if lifecycle.Message != "" {
		attrs = append(attrs, "diagnostic", lifecycle.Message)
	}
	if lifecycle.State == wire.DriverLifecycleExited {
		s.hub.Logger.Warn("driver process lifecycle", attrs...)
	} else {
		s.hub.Logger.Info("driver process lifecycle", attrs...)
	}
	return nil
}

func (s hubRuntimeAsyncSink) DriverRegister(ctx context.Context, runtimeID string, req wire.DriverRegister) (wire.DriverLeaseGranted, error) {
	rejected := func(err error) (wire.DriverLeaseGranted, error) {
		return wire.DriverLeaseGranted{
			Op:        wire.OpDriverLeaseGranted,
			RequestID: req.RequestID,
			TenantID:  req.TenantID,
			SessionID: req.SessionID,
			Accepted:  false,
			Error:     driverWireError(err),
		}, nil
	}
	if err := s.validateDriverRuntime(req.TenantID, runtimeID); err != nil {
		return rejected(err)
	}
	key := driverSessionKey(req.TenantID, req.SessionID)
	record, err := s.hub.agentSessions.Load(ctx, key)
	if err != nil {
		return rejected(err)
	}
	if req.DesiredGeneration != record.State.Driver.Generation+1 {
		return rejected(agent.ErrStaleDriver)
	}
	grant, err := s.hub.driverLeases.Register(ctx, agent.DriverIdentity{
		RuntimeID:         runtimeID,
		TenantID:          req.TenantID,
		SessionID:         req.SessionID,
		ComponentID:       req.ComponentID,
		AgentSpecRevision: req.AgentSpecRevision,
		DriverInstanceID:  req.DriverInstanceID,
	})
	if err != nil {
		return rejected(err)
	}
	return wire.DriverLeaseGranted{
		Op:                  wire.OpDriverLeaseGranted,
		RequestID:           req.RequestID,
		TenantID:            req.TenantID,
		SessionID:           req.SessionID,
		Accepted:            true,
		Fence:               wireDriverFence(grant.Fence),
		ExpiresAt:           grant.ExpiresAt,
		HeartbeatIntervalMS: uint64(grant.HeartbeatInterval.Milliseconds()),
		SessionVersion:      grant.SessionVersion,
	}, nil
}

func (s hubRuntimeAsyncSink) DriverReady(ctx context.Context, runtimeID string, req wire.DriverReady) (wire.DriverResult, error) {
	if err := s.validateDriverRuntime(req.TenantID, runtimeID); err != nil {
		return rejectedDriverResult(req.RequestID, err), nil
	}
	key := driverSessionKey(req.TenantID, req.SessionID)
	record, err := s.hub.driverLeases.Ready(ctx, key, runtimeID, agentDriverFence(req.Fence))
	return driverRecordResult(req.RequestID, record, err), nil
}

func (s hubRuntimeAsyncSink) DriverReadyAcknowledged(ctx context.Context, runtimeID string, req wire.DriverReady) error {
	if err := s.validateDriverRuntime(req.TenantID, runtimeID); err != nil {
		return err
	}
	if err := s.hub.execution.DeliverCurrent(ctx, driverSessionKey(req.TenantID, req.SessionID)); err != nil && !errors.Is(err, agent.ErrNoAssignableTurn) {
		return fmt.Errorf("deliver execution after driver ready acknowledgment: %w", err)
	}
	return nil
}

func (s hubRuntimeAsyncSink) DriverHeartbeat(ctx context.Context, runtimeID string, req wire.DriverHeartbeat) (wire.DriverResult, error) {
	if err := s.validateDriverRuntime(req.TenantID, runtimeID); err != nil {
		return rejectedDriverResult(req.RequestID, err), nil
	}
	key := driverSessionKey(req.TenantID, req.SessionID)
	if _, err := s.hub.driverLeases.Heartbeat(ctx, key, runtimeID, agentDriverFence(req.Fence), req.Sequence); err != nil {
		return rejectedDriverResult(req.RequestID, err), nil
	}
	record, err := s.hub.agentSessions.Load(ctx, key)
	return driverRecordResult(req.RequestID, record, err), nil
}

func (s hubRuntimeAsyncSink) TurnPrepared(ctx context.Context, runtimeID string, req wire.TurnPrepared) (wire.DriverResult, error) {
	if err := s.validateDriverRuntime(req.TenantID, runtimeID); err != nil {
		return rejectedDriverResult(req.RequestID, err), nil
	}
	permit, err := s.hub.execution.Prepared(ctx, driverSessionKey(req.TenantID, req.SessionID), runtimeID, req.TurnID, req.AttemptID, agentDriverFence(req.Fence))
	if err != nil {
		return rejectedDriverResult(req.RequestID, err), nil
	}
	return acceptedDriverRecordResult(req.RequestID, permit.SessionVersion, permit.Cursor), nil
}

func (s hubRuntimeAsyncSink) TurnPrepareFailed(ctx context.Context, runtimeID string, req wire.TurnPrepareFailed) (wire.DriverResult, error) {
	if err := s.validateDriverRuntime(req.TenantID, runtimeID); err != nil {
		return rejectedDriverResult(req.RequestID, err), nil
	}
	result, err := s.hub.turnExecutions.PrepareFailedResult(ctx, driverSessionKey(req.TenantID, req.SessionID), runtimeID, req.TurnID, req.AttemptID, agentDriverFence(req.Fence), req.Reason, req.Retryable)
	if err == nil && !result.Duplicate && !req.Retryable {
		s.hub.dispatchAfterTurn(result.Record, req.TurnID, req.Reason)
	}
	return driverRecordResult(req.RequestID, result.Record, err), nil
}

func (s hubRuntimeAsyncSink) TurnOutput(ctx context.Context, runtimeID string, req wire.TurnOutput) (wire.DriverResult, error) {
	if err := s.validateDriverRuntime(req.TenantID, runtimeID); err != nil {
		return rejectedOutputDriverResult(req, err), nil
	}
	accepted, err := s.hub.outputs.Append(ctx, agent.OutputRequest{
		Key:       driverSessionKey(req.TenantID, req.SessionID),
		RuntimeID: runtimeID,
		TurnID:    req.TurnID,
		AttemptID: req.AttemptID,
		Fence:     agentDriverFence(req.Fence),
		Sequence:  req.Sequence,
		Type:      string(req.OutputType),
		Payload:   req.Payload,
	})
	if err != nil {
		return rejectedOutputDriverResult(req, err), nil
	}
	if !accepted.Duplicate {
		s.hub.publishClientStoredEvent(driverSessionKey(req.TenantID, req.SessionID), accepted.Event)
	}
	return acceptedDriverRecordResult(req.RequestID, accepted.SessionVersion, accepted.Cursor), nil
}

func (s hubRuntimeAsyncSink) TurnToolCall(ctx context.Context, runtimeID string, req wire.TurnToolCall) (wire.DriverResult, error) {
	if err := s.validateDriverRuntime(req.TenantID, runtimeID); err != nil {
		return rejectedDriverResult(req.RequestID, err), nil
	}
	conn := s.hub.runtimeConn(runtimeID)
	execution, ok := conn.(runtimeapi.DriverExecutionConn)
	if !ok {
		return rejectedDriverResult(req.RequestID, fmt.Errorf("runtime %q does not support driver tool results", runtimeID)), nil
	}
	correlation := toolAttemptContext{
		TurnID: req.TurnID, AttemptID: req.AttemptID,
		DriverInstanceID: req.Fence.DriverInstanceID, LeaseID: req.Fence.LeaseID,
		DriverGeneration: req.Fence.Generation, TurnCorrelationID: req.CorrelationID,
	}
	msg := &BusMessage{ID: req.ToolCallID, Name: req.Name, Input: req.Input, Meta: correlation.meta()}
	deliver := func(output string, artifact json.RawMessage, truncated bool) error {
		deliveryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := execution.TurnToolResult(deliveryCtx, wire.TurnToolResult{
			Op: wire.OpTurnToolResult, RequestID: req.RequestID, AttemptRef: req.AttemptRef,
			ToolCallID: req.ToolCallID, Output: output, Artifact: artifact, Truncated: truncated,
		})
		if err != nil {
			return err
		}
		if !result.Accepted {
			return fmt.Errorf("runtime rejected tool result: %v", result.Error)
		}
		return nil
	}
	if err := s.hub.tools.HandleRuntimeToolCall(req.TenantID, req.SessionID, msg, deliver); err != nil {
		return rejectedDriverResult(req.RequestID, err), nil
	}
	return wire.DriverResult{Op: wire.OpDriverResult, RequestID: req.RequestID, Accepted: true}, nil
}

func (s hubRuntimeAsyncSink) TurnCompleted(ctx context.Context, runtimeID string, req wire.TurnCompleted) (wire.DriverResult, error) {
	if err := s.validateDriverRuntime(req.TenantID, runtimeID); err != nil {
		return rejectedDriverResult(req.RequestID, err), nil
	}
	result, err := s.hub.recovery.CompleteResult(ctx, driverSessionKey(req.TenantID, req.SessionID), req.RequestID, runtimeID, req.TurnID, req.AttemptID, agentDriverFence(req.Fence))
	if err == nil && !result.Duplicate {
		s.hub.dispatchAfterTurn(result.Record, req.TurnID, "")
		s.deliverNextTurn(ctx, result.Record.Key, req.TurnID)
	}
	return driverRecordResult(req.RequestID, result.Record, err), nil
}

func (s hubRuntimeAsyncSink) TurnFailed(ctx context.Context, runtimeID string, req wire.TurnFailed) (wire.DriverResult, error) {
	if err := s.validateDriverRuntime(req.TenantID, runtimeID); err != nil {
		return rejectedDriverResult(req.RequestID, err), nil
	}
	result, err := s.hub.recovery.FailResult(ctx, driverSessionKey(req.TenantID, req.SessionID), req.RequestID, runtimeID, req.TurnID, req.AttemptID, agentDriverFence(req.Fence), req.Reason)
	if err == nil && !result.Duplicate {
		s.hub.dispatchAfterTurn(result.Record, req.TurnID, req.Reason)
		s.deliverNextTurn(ctx, result.Record.Key, req.TurnID)
	}
	return driverRecordResult(req.RequestID, result.Record, err), nil
}

func (s hubRuntimeAsyncSink) TurnCancelled(ctx context.Context, runtimeID string, req wire.TurnCancelled) (wire.DriverResult, error) {
	if err := s.validateDriverRuntime(req.TenantID, runtimeID); err != nil {
		return rejectedDriverResult(req.RequestID, err), nil
	}
	result, err := s.hub.recovery.ConfirmCancellationResult(ctx, driverSessionKey(req.TenantID, req.SessionID), req.RequestID, runtimeID, req.TurnID, req.AttemptID, agentDriverFence(req.Fence))
	if err == nil && !result.Duplicate {
		s.hub.dispatchAfterTurn(result.Record, req.TurnID, "")
		s.deliverNextTurn(ctx, result.Record.Key, req.TurnID)
	}
	return driverRecordResult(req.RequestID, result.Record, err), nil
}

func (s hubRuntimeAsyncSink) TurnUncertain(ctx context.Context, runtimeID string, req wire.TurnUncertain) (wire.DriverResult, error) {
	if err := s.validateDriverRuntime(req.TenantID, runtimeID); err != nil {
		return rejectedDriverResult(req.RequestID, err), nil
	}
	result, err := s.hub.recovery.UncertainResult(ctx, driverSessionKey(req.TenantID, req.SessionID), req.RequestID, runtimeID, req.TurnID, req.AttemptID, agentDriverFence(req.Fence), req.Reason, req.ReconciliationEvidence)
	if err == nil && !result.Duplicate {
		s.hub.dispatchAfterTurn(result.Record, req.TurnID, req.Reason)
		s.deliverNextTurn(ctx, result.Record.Key, req.TurnID)
	}
	return driverRecordResult(req.RequestID, result.Record, err), nil
}

func (s hubRuntimeAsyncSink) deliverNextTurn(ctx context.Context, key agent.SessionKey, finishedTurnID string) {
	if err := s.hub.execution.DeliverCurrent(ctx, key); err != nil && !errors.Is(err, agent.ErrNoAssignableTurn) {
		s.hub.Logger.Warn("next queued turn delivery deferred",
			"tenant_id", key.TenantID,
			"session_id", key.SessionID,
			"finished_turn_id", finishedTurnID,
			"err", err,
		)
	}
}

func (s hubRuntimeAsyncSink) validateDriverRuntime(tenantID, runtimeID string) error {
	if s.hub == nil || s.hub.runtimes == nil || s.hub.driverLeases == nil || s.hub.turnExecutions == nil || s.hub.outputs == nil || s.hub.recovery == nil || s.hub.execution == nil || s.hub.agentSessions == nil {
		return fmt.Errorf("driver execution services are unavailable")
	}
	if !s.hub.runtimes.RuntimeAllowedForTenant(tenantID, runtimeID) {
		return agent.ErrPermissionDenied
	}
	return nil
}

func driverSessionKey(tenantID, sessionID string) agent.SessionKey {
	return agent.SessionKey{TenantID: tenantID, SessionID: sessionID}
}

func agentDriverFence(fence wire.DriverFence) agent.Fence {
	return agent.Fence{DriverInstanceID: fence.DriverInstanceID, LeaseID: fence.LeaseID, Generation: fence.Generation}
}

func wireDriverFence(fence agent.Fence) wire.DriverFence {
	return wire.DriverFence{DriverInstanceID: fence.DriverInstanceID, LeaseID: fence.LeaseID, Generation: fence.Generation}
}

func driverRecordResult(requestID string, record agent.Record, err error) wire.DriverResult {
	if err != nil {
		return rejectedDriverResult(requestID, err)
	}
	return acceptedDriverRecordResult(requestID, record.Version, record.Cursor)
}

func acceptedDriverRecordResult(requestID string, version uint64, cursor agent.Cursor) wire.DriverResult {
	return wire.DriverResult{Op: wire.OpDriverResult, RequestID: requestID, Accepted: true, SessionVersion: version, Cursor: uint64(cursor)}
}

func rejectedDriverResult(requestID string, err error) wire.DriverResult {
	result := wire.DriverResult{Op: wire.OpDriverResult, RequestID: requestID, Accepted: false}
	var replay *agent.OutputReplayRequiredError
	if errors.As(err, &replay) {
		result.ExpectedSequence = replay.ExpectedSequence
	}
	result.Error = driverWireError(err)
	return result
}

func rejectedOutputDriverResult(req wire.TurnOutput, err error) wire.DriverResult {
	result := rejectedDriverResult(req.RequestID, err)
	if result.ExpectedSequence == 0 {
		result.ExpectedSequence = req.Sequence
		if result.ExpectedSequence == 0 {
			result.ExpectedSequence = 1
		}
	}
	return result
}

func driverWireError(err error) *wire.Error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &wire.Error{Code: wire.ErrorTimeout, Retryable: true}
	case errors.Is(err, context.Canceled):
		return &wire.Error{Code: wire.ErrorCancelled, Retryable: true}
	case errors.Is(err, agent.ErrPermissionDenied), errors.Is(err, agent.ErrStaleDriver):
		return &wire.Error{Code: wire.ErrorUnauthorized, Retryable: false}
	case errors.Is(err, agent.ErrNotFound), errors.Is(err, agent.ErrSessionClosed):
		return &wire.Error{Code: wire.ErrorTargetUnknown, Retryable: false}
	case errors.Is(err, agent.ErrVersionConflict):
		return &wire.Error{Code: wire.ErrorRuntimeBusy, Retryable: true}
	case errors.Is(err, agent.ErrInvalidArgument), errors.Is(err, agent.ErrInvalidTransition), errors.Is(err, agent.ErrOutputConflict), errors.Is(err, agent.ErrCommandConflict), errors.Is(err, agent.ErrInputConflict):
		return &wire.Error{Code: wire.ErrorProtocolError, Retryable: false}
	default:
		var replay *agent.OutputReplayRequiredError
		if errors.As(err, &replay) {
			return &wire.Error{Code: wire.ErrorProtocolError, Retryable: true}
		}
		return &wire.Error{Code: wire.ErrorInternal, Retryable: true}
	}
}

func (s hubRuntimeAsyncSink) RuntimeProtocolError(runtimeID string, err error) {
	if s.hub == nil || s.hub.Logger == nil {
		return
	}
	s.hub.Logger.Warn("runtime protocol error", "runtime_id", runtimeID, "err", err)
}

func kernelHookAction(action wire.HookAction) (string, error) {
	switch action {
	case wire.HookActionOK:
		return string(khooks.ActionPass), nil
	case wire.HookActionRewrite:
		return string(khooks.ActionModify), nil
	case wire.HookActionDeny:
		return string(khooks.ActionBlock), nil
	case wire.HookActionClaim:
		return string(khooks.ActionClaim), nil
	case wire.HookActionSuspend:
		return string(khooks.ActionSuspend), nil
	default:
		return "", fmt.Errorf("unknown hook_event_reply action %q", action)
	}
}

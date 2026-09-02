package wire

import "encoding/json"

// Encode marshals a concrete Runtime API frame after validating it.
func Encode(frame any) ([]byte, error) {
	if err := validateFrame(frame); err != nil {
		return nil, err
	}
	return json.Marshal(frame)
}

// Decode unmarshals and validates one Runtime API JSON frame, returning the
// envelope metadata and a concrete frame pointer.
func Decode(data []byte) (Envelope, any, error) {
	var head Envelope
	if err := json.Unmarshal(data, &head); err != nil {
		return Envelope{}, nil, ProtocolErrorf("decode frame: %v", err)
	}
	if head.Op == "" {
		return Envelope{}, nil, ProtocolErrorf("op is required")
	}

	var frame any
	switch head.Op {
	case OpHello:
		frame = &Hello{}
	case OpHelloAck:
		frame = &HelloAck{}
	case OpInvoke:
		frame = &Invoke{}
	case OpInvokeResult:
		frame = &InvokeResult{}
	case OpInvokeResultStart:
		frame = &InvokeResultStart{}
	case OpInvokeResultDelta:
		frame = &InvokeResultDelta{}
	case OpInvokeResultEnd:
		frame = &InvokeResultEnd{}
	case OpCancel:
		frame = &Cancel{}
	case OpCancelAck:
		frame = &CancelAck{}
	case OpHealth:
		frame = &Health{}
	case OpHealthResp:
		frame = &HealthResp{}
	case OpListCapabilities:
		frame = &ListCapabilities{}
	case OpListCapabilitiesResp:
		frame = &ListCapabilitiesResp{}
	case OpReload:
		frame = &Reload{}
	case OpReloadAck:
		frame = &ReloadAck{}
	case OpPrepareTenant:
		frame = &PrepareTenant{}
	case OpPrepareTenantAck:
		frame = &PrepareTenantAck{}
	case OpHookEvent:
		frame = &HookEvent{}
	case OpCatalogUpdate:
		frame = &CatalogUpdate{}
	case OpHookEventReply:
		frame = &HookEventReply{}
	case OpPluginSend:
		frame = &PluginSend{}
	case OpPluginLog:
		frame = &PluginLog{}
	case OpLifecycleNotice:
		frame = &LifecycleNotice{}
	case OpDriverEnsure:
		frame = &DriverEnsure{}
	case OpDriverEnsureAck:
		frame = &DriverEnsureAck{}
	case OpDriverStop:
		frame = &DriverStop{}
	case OpDriverStopAck:
		frame = &DriverStopAck{}
	case OpDriverLifecycle:
		frame = &DriverLifecycle{}
	case OpDriverRegister:
		frame = &DriverRegister{}
	case OpDriverLeaseGranted:
		frame = &DriverLeaseGranted{}
	case OpDriverReady:
		frame = &DriverReady{}
	case OpDriverHeartbeat:
		frame = &DriverHeartbeat{}
	case OpDriverResult:
		frame = &DriverResult{}
	case OpTurnAssign:
		frame = &TurnAssign{}
	case OpTurnPrepared:
		frame = &TurnPrepared{}
	case OpTurnPrepareFailed:
		frame = &TurnPrepareFailed{}
	case OpTurnPermit:
		frame = &TurnPermit{}
	case OpTurnCancel:
		frame = &TurnCancel{}
	case OpTurnOutput:
		frame = &TurnOutput{}
	case OpTurnToolCall:
		frame = &TurnToolCall{}
	case OpTurnToolResult:
		frame = &TurnToolResult{}
	case OpTurnCompleted:
		frame = &TurnCompleted{}
	case OpTurnFailed:
		frame = &TurnFailed{}
	case OpTurnCancelled:
		frame = &TurnCancelled{}
	case OpTurnUncertain:
		frame = &TurnUncertain{}
	default:
		return Envelope{}, nil, ProtocolErrorf("unknown op %q", head.Op)
	}
	if err := json.Unmarshal(data, frame); err != nil {
		return Envelope{}, nil, ProtocolErrorf("decode %s: %v", head.Op, err)
	}
	if err := validateFrame(frame); err != nil {
		return Envelope{}, nil, err
	}

	switch f := frame.(type) {
	case *Invoke:
		head.CallID = f.CallID
	case *InvokeResult:
		head.CallID = f.CallID
	case *InvokeResultStart:
		head.CallID = f.CallID
	case *InvokeResultDelta:
		head.CallID = f.CallID
	case *InvokeResultEnd:
		head.CallID = f.CallID
	case *Cancel:
		head.CallID = f.CallID
	case *CancelAck:
		head.CallID = f.CallID
	case *PrepareTenant:
		head.CallID = f.RequestID
	case *PrepareTenantAck:
		head.CallID = f.RequestID
	case *HookEvent:
		head.CallID = f.CallID
	case *HookEventReply:
		head.CallID = f.CallID
	case *DriverEnsure:
		head.CallID = f.RequestID
	case *DriverEnsureAck:
		head.CallID = f.RequestID
	case *DriverStop:
		head.CallID = f.RequestID
	case *DriverStopAck:
		head.CallID = f.RequestID
	case *DriverRegister:
		head.CallID = f.RequestID
	case *DriverLeaseGranted:
		head.CallID = f.RequestID
	case *DriverReady:
		head.CallID = f.RequestID
	case *DriverHeartbeat:
		head.CallID = f.RequestID
	case *DriverResult:
		head.CallID = f.RequestID
	case *TurnAssign:
		head.CallID = f.RequestID
	case *TurnPrepared:
		head.CallID = f.RequestID
	case *TurnPrepareFailed:
		head.CallID = f.RequestID
	case *TurnPermit:
		head.CallID = f.RequestID
	case *TurnCancel:
		head.CallID = f.RequestID
	case *TurnOutput:
		head.CallID = f.RequestID
	case *TurnToolCall:
		head.CallID = f.RequestID
	case *TurnToolResult:
		head.CallID = f.RequestID
	case *TurnCompleted:
		head.CallID = f.RequestID
	case *TurnFailed:
		head.CallID = f.RequestID
	case *TurnCancelled:
		head.CallID = f.RequestID
	case *TurnUncertain:
		head.CallID = f.RequestID
	}
	return head, frame, nil
}

func validateFrame(frame any) error {
	switch f := frame.(type) {
	case Hello:
		return validateFrame(&f)
	case *Hello:
		if f.Op != OpHello {
			return ProtocolErrorf("hello op must be %q", OpHello)
		}
		if err := ValidateRuntimeID(f.RuntimeID); err != nil {
			return err
		}
		if f.ProtocolVersion == "" {
			return ProtocolErrorf("protocol_version is required")
		}
		if err := validateTenantsServed(f.TenantsServed); err != nil {
			return err
		}
		return validateCapabilities(f.Capabilities)
	case HelloAck:
		return validateFrame(&f)
	case *HelloAck:
		if f.Op != OpHelloAck {
			return ProtocolErrorf("hello_ack op must be %q", OpHelloAck)
		}
		if !f.Accepted {
			if f.Error == nil {
				return ProtocolErrorf("rejected hello_ack requires error")
			}
			return f.Error.Validate()
		}
		return nil
	case Invoke:
		return validateFrame(&f)
	case *Invoke:
		if f.Op != OpInvoke {
			return ProtocolErrorf("invoke op must be %q", OpInvoke)
		}
		if f.CallID == "" {
			return ProtocolErrorf("call_id is required")
		}
		if err := ValidateTenantID(f.TenantID); err != nil {
			return err
		}
		if err := f.Target.Validate(); err != nil {
			return err
		}
		if f.TenantID != "" {
			if err := ValidateTenantID(f.TenantID); err != nil {
				return err
			}
		}
		if f.Tool == "" {
			return ProtocolErrorf("tool is required")
		}
		return nil
	case InvokeResult:
		return validateFrame(&f)
	case *InvokeResult:
		if f.Op != OpInvokeResult {
			return ProtocolErrorf("invoke_result op must be %q", OpInvokeResult)
		}
		if f.CallID == "" {
			return ProtocolErrorf("call_id is required")
		}
		if !f.OK {
			if f.Error == nil {
				return ProtocolErrorf("failed invoke_result requires error")
			}
			return f.Error.Validate()
		}
		return nil
	case InvokeResultStart:
		return validateFrame(&f)
	case *InvokeResultStart:
		if f.Op != OpInvokeResultStart {
			return ProtocolErrorf("invoke_result_start op must be %q", OpInvokeResultStart)
		}
		if f.CallID == "" {
			return ProtocolErrorf("call_id is required")
		}
		return nil
	case InvokeResultDelta:
		return validateFrame(&f)
	case *InvokeResultDelta:
		if f.Op != OpInvokeResultDelta {
			return ProtocolErrorf("invoke_result_delta op must be %q", OpInvokeResultDelta)
		}
		if f.CallID == "" {
			return ProtocolErrorf("call_id is required")
		}
		if f.Seq <= 0 {
			return ProtocolErrorf("seq must be > 0")
		}
		return nil
	case InvokeResultEnd:
		return validateFrame(&f)
	case *InvokeResultEnd:
		if f.Op != OpInvokeResultEnd {
			return ProtocolErrorf("invoke_result_end op must be %q", OpInvokeResultEnd)
		}
		if f.CallID == "" {
			return ProtocolErrorf("call_id is required")
		}
		if f.Bytes < 0 {
			return ProtocolErrorf("bytes must be >= 0")
		}
		return nil
	case Cancel:
		return validateFrame(&f)
	case *Cancel:
		if f.Op != OpCancel {
			return ProtocolErrorf("cancel op must be %q", OpCancel)
		}
		if f.CallID == "" {
			return ProtocolErrorf("call_id is required")
		}
		return nil
	case CancelAck:
		return validateFrame(&f)
	case *CancelAck:
		if f.Op != OpCancelAck {
			return ProtocolErrorf("cancel_ack op must be %q", OpCancelAck)
		}
		if f.CallID == "" {
			return ProtocolErrorf("call_id is required")
		}
		return nil
	case Health:
		return validateFrame(&f)
	case *Health:
		if f.Op != OpHealth {
			return ProtocolErrorf("health op must be %q", OpHealth)
		}
		return nil
	case HealthResp:
		return validateFrame(&f)
	case *HealthResp:
		if f.Op != OpHealthResp {
			return ProtocolErrorf("health_resp op must be %q", OpHealthResp)
		}
		return nil
	case ListCapabilities:
		return validateFrame(&f)
	case *ListCapabilities:
		if f.Op != OpListCapabilities {
			return ProtocolErrorf("list_capabilities op must be %q", OpListCapabilities)
		}
		return nil
	case ListCapabilitiesResp:
		return validateFrame(&f)
	case *ListCapabilitiesResp:
		if f.Op != OpListCapabilitiesResp {
			return ProtocolErrorf("list_capabilities_resp op must be %q", OpListCapabilitiesResp)
		}
		return validateCapabilities(f.Targets)
	case Reload:
		return validateFrame(&f)
	case *Reload:
		if f.Op != OpReload {
			return ProtocolErrorf("reload op must be %q", OpReload)
		}
		if err := validateTenantsServed(f.Tenants); err != nil {
			return err
		}
		if f.Target != nil {
			return f.Target.Validate()
		}
		return nil
	case ReloadAck:
		return validateFrame(&f)
	case *ReloadAck:
		if f.Op != OpReloadAck {
			return ProtocolErrorf("reload_ack op must be %q", OpReloadAck)
		}
		for _, target := range f.EvictedTargets {
			if err := target.Validate(); err != nil {
				return err
			}
		}
		return nil
	case PrepareTenant:
		return validateFrame(&f)
	case *PrepareTenant:
		if f.Op != OpPrepareTenant || f.RequestID == "" {
			return ProtocolErrorf("prepare_tenant requires op %q and request_id", OpPrepareTenant)
		}
		return ValidateTenantID(f.TenantID)
	case PrepareTenantAck:
		return validateFrame(&f)
	case *PrepareTenantAck:
		if f.Op != OpPrepareTenantAck || f.RequestID == "" {
			return ProtocolErrorf("prepare_tenant_ack requires op %q and request_id", OpPrepareTenantAck)
		}
		return validateCapabilities(f.Capabilities)
	case HookEvent:
		return validateFrame(&f)
	case *HookEvent:
		if f.Op != OpHookEvent {
			return ProtocolErrorf("hook_event op must be %q", OpHookEvent)
		}
		if err := f.Target.Validate(); err != nil {
			return err
		}
		if f.Event == "" {
			return ProtocolErrorf("event is required")
		}
		switch f.ReplyMode {
		case HookReplyModeNone:
			return nil
		case HookReplyModeModifying, HookReplyModeClaiming:
			if f.CallID == "" {
				return ProtocolErrorf("call_id is required when reply_mode expects a reply")
			}
			return nil
		default:
			return ProtocolErrorf("unknown hook reply_mode %q", f.ReplyMode)
		}
	case CatalogUpdate:
		return validateFrame(&f)
	case *CatalogUpdate:
		if f.Op != OpCatalogUpdate {
			return ProtocolErrorf("catalog_update op must be %q", OpCatalogUpdate)
		}
		if err := (Capability{Target: f.Target, Tenants: f.Tenants, Tools: f.Tools, Hooks: f.Hooks, Revision: f.Revision, State: f.State, Source: f.Source}).Validate(); err != nil {
			return err
		}
		return nil
	case HookEventReply:
		return validateFrame(&f)
	case *HookEventReply:
		if f.Op != OpHookEventReply {
			return ProtocolErrorf("hook_event_reply op must be %q", OpHookEventReply)
		}
		if f.CallID == "" {
			return ProtocolErrorf("call_id is required")
		}
		switch f.Action {
		case HookActionOK, HookActionRewrite, HookActionDeny, HookActionClaim, HookActionSuspend:
			return nil
		default:
			return ProtocolErrorf("unknown hook action %q", f.Action)
		}
	case PluginSend:
		return validateFrame(&f)
	case *PluginSend:
		if f.Op != OpPluginSend {
			return ProtocolErrorf("plugin_send op must be %q", OpPluginSend)
		}
		if err := f.Target.Validate(); err != nil {
			return err
		}
		if f.Channel != "bus" {
			return ProtocolErrorf("unknown plugin_send channel %q", f.Channel)
		}
		if f.Type == "" {
			return ProtocolErrorf("type is required")
		}
		return nil
	case PluginLog:
		return validateFrame(&f)
	case *PluginLog:
		if f.Op != OpPluginLog {
			return ProtocolErrorf("plugin_log op must be %q", OpPluginLog)
		}
		if err := f.Target.Validate(); err != nil {
			return err
		}
		if f.Level == "" {
			return ProtocolErrorf("level is required")
		}
		if f.Message == "" {
			return ProtocolErrorf("message is required")
		}
		return nil
	case LifecycleNotice:
		return validateFrame(&f)
	case *LifecycleNotice:
		if f.Op != OpLifecycleNotice {
			return ProtocolErrorf("lifecycle_notice op must be %q", OpLifecycleNotice)
		}
		if err := f.Target.Validate(); err != nil {
			return err
		}
		switch f.State {
		case LifecycleStateStarting, LifecycleStateReady, LifecycleStateStopping, LifecycleStateExited, LifecycleStateCrashed:
			return nil
		default:
			return ProtocolErrorf("unknown lifecycle state %q", f.State)
		}
	case DriverEnsure:
		return validateFrame(&f)
	case *DriverEnsure:
		if f.Op != OpDriverEnsure {
			return ProtocolErrorf("driver.ensure op must be %q", OpDriverEnsure)
		}
		if err := validateDriverRequest(f.RequestID, f.TenantID, f.SessionID); err != nil {
			return err
		}
		if f.ComponentID == "" || f.AgentSpecRevision == "" || f.DesiredGeneration == 0 {
			return ProtocolErrorf("driver.ensure requires component_id, agent_spec_revision, and positive desired_generation")
		}
		return nil
	case DriverEnsureAck:
		return validateFrame(&f)
	case *DriverEnsureAck:
		if f.Op != OpDriverEnsureAck || f.RequestID == "" {
			return ProtocolErrorf("driver.ensure_ack requires op %q and request_id", OpDriverEnsureAck)
		}
		return nil
	case DriverStop:
		return validateFrame(&f)
	case *DriverStop:
		if f.Op != OpDriverStop {
			return ProtocolErrorf("driver.stop op must be %q", OpDriverStop)
		}
		return validateDriverRequest(f.RequestID, f.TenantID, f.SessionID)
	case DriverStopAck:
		return validateFrame(&f)
	case *DriverStopAck:
		if f.Op != OpDriverStopAck || f.RequestID == "" {
			return ProtocolErrorf("driver.stop_ack requires op %q and request_id", OpDriverStopAck)
		}
		return nil
	case DriverLifecycle:
		return validateFrame(&f)
	case *DriverLifecycle:
		if f.Op != OpDriverLifecycle {
			return ProtocolErrorf("driver.lifecycle op must be %q", OpDriverLifecycle)
		}
		if err := validateDriverRequest("lifecycle", f.TenantID, f.SessionID); err != nil {
			return err
		}
		if f.ComponentID == "" || f.AgentSpecRevision == "" || f.DesiredGeneration == 0 || f.DriverInstanceID == "" {
			return ProtocolErrorf("driver.lifecycle requires component_id, agent_spec_revision, positive desired_generation, and driver_instance_id")
		}
		switch f.State {
		case DriverLifecycleStarted, DriverLifecycleReady, DriverLifecycleInitFailed, DriverLifecycleExited, DriverLifecycleStopped:
			return nil
		default:
			return ProtocolErrorf("unknown driver lifecycle state %q", f.State)
		}
	case DriverRegister:
		return validateFrame(&f)
	case *DriverRegister:
		if f.Op != OpDriverRegister || f.RequestID == "" || f.ComponentID == "" || f.AgentSpecRevision == "" || f.DesiredGeneration == 0 || f.DriverInstanceID == "" {
			return ProtocolErrorf("driver.register requires op, request_id, component_id, agent_spec_revision, positive desired_generation, and driver_instance_id")
		}
		return validateAttemptScope(f.TenantID, f.SessionID)
	case DriverLeaseGranted:
		return validateFrame(&f)
	case *DriverLeaseGranted:
		if f.Op != OpDriverLeaseGranted || f.RequestID == "" {
			return ProtocolErrorf("driver.lease_granted requires op and request_id")
		}
		if f.Accepted {
			if f.Error != nil || f.ExpiresAt.IsZero() || f.HeartbeatIntervalMS == 0 || f.SessionVersion == 0 {
				return ProtocolErrorf("accepted driver.lease_granted requires fence, expiry, heartbeat interval, and session version without error")
			}
			return validateFenceScope(f.TenantID, f.SessionID, f.Fence)
		}
		if f.Error == nil {
			return ProtocolErrorf("rejected driver.lease_granted requires error")
		}
		return validateAttemptScope(f.TenantID, f.SessionID)
	case DriverReady:
		return validateFrame(&f)
	case *DriverReady:
		return validateDriverMutation(f.Op, OpDriverReady, f.RequestID, f.TenantID, f.SessionID, f.Fence)
	case DriverHeartbeat:
		return validateFrame(&f)
	case *DriverHeartbeat:
		if err := validateDriverMutation(f.Op, OpDriverHeartbeat, f.RequestID, f.TenantID, f.SessionID, f.Fence); err != nil {
			return err
		}
		if f.Sequence == 0 {
			return ProtocolErrorf("driver.heartbeat sequence must be positive")
		}
		return nil
	case DriverResult:
		return validateFrame(&f)
	case *DriverResult:
		if f.Op != OpDriverResult || f.RequestID == "" {
			return ProtocolErrorf("driver.result requires op and request_id")
		}
		if !f.Accepted {
			if f.Error == nil {
				return ProtocolErrorf("rejected driver.result requires error")
			}
			return f.Error.Validate()
		}
		if f.Error != nil {
			return ProtocolErrorf("accepted driver.result must not carry error")
		}
		return nil
	case TurnAssign:
		return validateFrame(&f)
	case *TurnAssign:
		if err := validateAttemptMutation(f.Op, OpTurnAssign, f.RequestID, f.AttemptRef); err != nil {
			return err
		}
		if len(f.Input) == 0 || f.SessionVersion == 0 {
			return ProtocolErrorf("turn.assign requires input and session_version")
		}
		return validateJSON("input", f.Input, 64<<10)
	case TurnPrepared:
		return validateFrame(&f)
	case *TurnPrepared:
		return validateAttemptMutation(f.Op, OpTurnPrepared, f.RequestID, f.AttemptRef)
	case TurnPrepareFailed:
		return validateFrame(&f)
	case *TurnPrepareFailed:
		if err := validateAttemptMutation(f.Op, OpTurnPrepareFailed, f.RequestID, f.AttemptRef); err != nil {
			return err
		}
		if f.Reason == "" {
			return ProtocolErrorf("turn.prepare_failed reason is required")
		}
		return nil
	case TurnPermit:
		return validateFrame(&f)
	case *TurnPermit:
		if err := validateAttemptMutation(f.Op, OpTurnPermit, f.RequestID, f.AttemptRef); err != nil {
			return err
		}
		if f.PermitID == "" || f.SessionVersion == 0 || f.Cursor == 0 {
			return ProtocolErrorf("turn.permit requires permit_id, session_version, and cursor")
		}
		return nil
	case TurnCancel:
		return validateFrame(&f)
	case *TurnCancel:
		if err := validateAttemptMutation(f.Op, OpTurnCancel, f.RequestID, f.AttemptRef); err != nil {
			return err
		}
		if f.SessionVersion == 0 {
			return ProtocolErrorf("turn.cancel session_version is required")
		}
		return nil
	case TurnOutput:
		return validateFrame(&f)
	case *TurnOutput:
		if err := validateAttemptMutation(f.Op, OpTurnOutput, f.RequestID, f.AttemptRef); err != nil {
			return err
		}
		if f.Sequence == 0 {
			return ProtocolErrorf("turn.output sequence must be positive")
		}
		switch f.OutputType {
		case OutputStreamDelta, OutputReasoning, OutputUsage, OutputProviderRetry, OutputProviderError, OutputCompaction, OutputToolResult:
		default:
			return ProtocolErrorf("unknown output_type %q", f.OutputType)
		}
		return validateJSON("payload", f.Payload, 64<<10)
	case TurnToolCall:
		return validateFrame(&f)
	case *TurnToolCall:
		if err := validateAttemptMutation(f.Op, OpTurnToolCall, f.RequestID, f.AttemptRef); err != nil {
			return err
		}
		if f.ToolCallID == "" || f.Name == "" {
			return ProtocolErrorf("turn.tool_call requires tool_call_id and name")
		}
		return validateJSON("input", f.Input, 64<<10)
	case TurnToolResult:
		return validateFrame(&f)
	case *TurnToolResult:
		if err := validateAttemptMutation(f.Op, OpTurnToolResult, f.RequestID, f.AttemptRef); err != nil {
			return err
		}
		if f.ToolCallID == "" {
			return ProtocolErrorf("turn.tool_result requires tool_call_id")
		}
		if len(f.Artifact) == 0 {
			return nil
		}
		return validateJSON("artifact", f.Artifact, 64<<10)
	case TurnCompleted:
		return validateFrame(&f)
	case *TurnCompleted:
		return validateTerminal(f.Op, OpTurnCompleted, f.RequestID, f.AttemptRef, f.Sequence, "")
	case TurnFailed:
		return validateFrame(&f)
	case *TurnFailed:
		return validateTerminal(f.Op, OpTurnFailed, f.RequestID, f.AttemptRef, f.Sequence, f.Reason)
	case TurnCancelled:
		return validateFrame(&f)
	case *TurnCancelled:
		return validateTerminal(f.Op, OpTurnCancelled, f.RequestID, f.AttemptRef, f.Sequence, "")
	case TurnUncertain:
		return validateFrame(&f)
	case *TurnUncertain:
		return validateTerminal(f.Op, OpTurnUncertain, f.RequestID, f.AttemptRef, f.Sequence, f.Reason)
	default:
		return ProtocolErrorf("unsupported frame type %T", frame)
	}
}

func validateAttemptScope(tenantID, sessionID string) error {
	if err := ValidateTenantID(tenantID); err != nil {
		return err
	}
	if sessionID == "" {
		return ProtocolErrorf("session_id is required")
	}
	return nil
}

func validateFenceScope(tenantID, sessionID string, fence DriverFence) error {
	if err := validateAttemptScope(tenantID, sessionID); err != nil {
		return err
	}
	if fence.DriverInstanceID == "" || fence.LeaseID == "" || fence.Generation == 0 {
		return ProtocolErrorf("complete driver fence is required")
	}
	return nil
}

func validateDriverMutation(op, expected Operation, requestID, tenantID, sessionID string, fence DriverFence) error {
	if op != expected || requestID == "" {
		return ProtocolErrorf("%s requires matching op and request_id", expected)
	}
	return validateFenceScope(tenantID, sessionID, fence)
}

func validateAttemptMutation(op, expected Operation, requestID string, ref AttemptRef) error {
	if err := validateDriverMutation(op, expected, requestID, ref.TenantID, ref.SessionID, ref.Fence); err != nil {
		return err
	}
	if ref.TurnID == "" || ref.AttemptID == "" || ref.CorrelationID == "" {
		return ProtocolErrorf("%s requires turn_id, attempt_id, and correlation_id", expected)
	}
	return nil
}

func validateTerminal(op, expected Operation, requestID string, ref AttemptRef, sequence uint64, reason string) error {
	if err := validateAttemptMutation(op, expected, requestID, ref); err != nil {
		return err
	}
	if sequence == 0 {
		return ProtocolErrorf("%s sequence must be positive", expected)
	}
	if (expected == OpTurnFailed || expected == OpTurnUncertain) && reason == "" {
		return ProtocolErrorf("%s reason is required", expected)
	}
	return nil
}

func validateJSON(name string, raw json.RawMessage, limit int) error {
	if len(raw) == 0 || !json.Valid(raw) {
		return ProtocolErrorf("%s must be valid JSON", name)
	}
	if len(raw) > limit {
		return ProtocolErrorf("%s exceeds %d bytes", name, limit)
	}
	return nil
}

func validateDriverRequest(requestID, tenantID, sessionID string) error {
	if requestID == "" {
		return ProtocolErrorf("request_id is required")
	}
	if err := ValidateTenantID(tenantID); err != nil {
		return err
	}
	if sessionID == "" {
		return ProtocolErrorf("session_id is required")
	}
	return nil
}

func validateTenantsServed(tenants []string) error {
	wildcard := false
	for _, tenantID := range tenants {
		if tenantID == "*" {
			wildcard = true
			continue
		}
		if err := ValidateTenantID(tenantID); err != nil {
			return err
		}
	}
	if wildcard && len(tenants) > 1 {
		return ProtocolErrorf("tenants_served cannot mix wildcard with explicit tenant ids")
	}
	return nil
}

func validateCapabilities(capabilities []Capability) error {
	for _, capability := range capabilities {
		if err := capability.Validate(); err != nil {
			return err
		}
	}
	return nil
}

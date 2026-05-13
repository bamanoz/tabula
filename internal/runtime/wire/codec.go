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
	case *Cancel:
		head.CallID = f.CallID
	case *CancelAck:
		head.CallID = f.CallID
	case *HookEvent:
		head.CallID = f.CallID
	case *HookEventReply:
		head.CallID = f.CallID
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
		case HookActionOK, HookActionRewrite, HookActionDeny, HookActionClaim:
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
	default:
		return ProtocolErrorf("unsupported frame type %T", frame)
	}
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

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
	default:
		return ProtocolErrorf("unsupported frame type %T", frame)
	}
}

func validateCapabilities(capabilities []Capability) error {
	for _, capability := range capabilities {
		if err := capability.Target.Validate(); err != nil {
			return err
		}
	}
	return nil
}

package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

type runtimeHookSubscriber struct {
	runtimeID  string
	capability wire.Capability
	conn       runtimeapi.RuntimeConn
	done       <-chan struct{}
	busy       func(string, wire.Target) bool
	logger     *slog.Logger
}

func newRuntimeHookSubscriber(target runtimeHookTarget, busy func(string, wire.Target) bool, logger *slog.Logger) HookSubscriber {
	return &runtimeHookSubscriber{
		runtimeID:  target.RuntimeID,
		capability: target.Capability,
		conn:       target.Conn,
		done:       target.Done,
		busy:       busy,
		logger:     logger,
	}
}

func (s *runtimeHookSubscriber) Name() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("runtime:%s:%s:%s", s.runtimeID, s.capability.Target.Kind, s.capability.Target.ID)
}

func (s *runtimeHookSubscriber) Session() string { return "" }

func (s *runtimeHookSubscriber) IsConnected() bool {
	return s != nil && s.conn != nil && s.capability.State == wire.CapabilityStateReady
}

func (s *runtimeHookSubscriber) IsBusy() bool {
	return s != nil && s.busy != nil && s.busy(s.runtimeID, s.capability.Target)
}

func (s *runtimeHookSubscriber) Hooks() []HookSubscription {
	if s == nil {
		return nil
	}
	out := make([]HookSubscription, 0, len(s.capability.Hooks))
	for _, hook := range s.capability.Hooks {
		var timeout *int
		if hook.TimeoutMS != nil {
			v := int(*hook.TimeoutMS)
			timeout = &v
		}
		out = append(out, HookSubscription{Event: hook.Event, Priority: hook.Priority, TimeoutMs: timeout})
	}
	return out
}

func (s *runtimeHookSubscriber) SendMsg(msg *Message) {
	if s == nil || s.conn == nil || msg == nil || msg.Type != string(MsgHook) {
		return
	}
	event := wire.HookEvent{
		Op:        wire.OpHookEvent,
		TenantID:  msg.TenantID,
		CallID:    msg.ID,
		Target:    s.capability.Target,
		Event:     msg.Name,
		ReplyMode: runtimeHookReplyMode(msg.Name),
		Data:      json.RawMessage(msg.Payload),
		SessionID: msg.Session,
	}
	if err := s.conn.SendHookEvent(context.Background(), event); err != nil {
		if s.logger != nil {
			s.logger.Warn(
				"runtime hook send failed",
				"runtime_id", s.runtimeID,
				"target_kind", string(s.capability.Target.Kind),
				"target", s.capability.Target.ID,
				"event", msg.Name,
				"hook_id", msg.ID,
				"tenant_id", msg.TenantID,
				"session", msg.Session,
				"payload_bytes", len(msg.Payload),
				"err", err,
			)
		}
		_ = s.conn.Close()
	}
}

func runtimeHookReplyMode(event string) wire.HookReplyMode {
	def, ok := HookEvents[event]
	if !ok {
		return wire.HookReplyModeModifying
	}
	switch def.Strategy {
	case strategyVoid:
		return wire.HookReplyModeNone
	case strategyClaiming:
		return wire.HookReplyModeClaiming
	default:
		return wire.HookReplyModeModifying
	}
}

func (s *runtimeHookSubscriber) Done() <-chan struct{} {
	if s == nil || s.done == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return s.done
}

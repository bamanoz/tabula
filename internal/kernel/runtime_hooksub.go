package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
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
	markBusy   func(string, wire.Target) func()
	tryBusy    func(string, wire.Target) (func(), bool)
	busyDone   func(string, wire.Target) <-chan struct{}
	logger     *slog.Logger
}

func newRuntimeHookSubscriber(target runtimeHookTarget, busy func(string, wire.Target) bool, markBusy func(string, wire.Target) func(), tryBusy func(string, wire.Target) (func(), bool), busyDone func(string, wire.Target) <-chan struct{}, logger *slog.Logger) khooks.Subscriber {
	return &runtimeHookSubscriber{
		runtimeID:  target.RuntimeID,
		capability: target.Capability,
		conn:       target.Conn,
		done:       target.Done,
		busy:       busy,
		markBusy:   markBusy,
		tryBusy:    tryBusy,
		busyDone:   busyDone,
		logger:     logger,
	}
}

func (s *runtimeHookSubscriber) RuntimeID() string {
	if s == nil {
		return ""
	}
	return s.runtimeID
}

func (s *runtimeHookSubscriber) Name() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("runtime:%s:%s:%s", s.runtimeID, s.capability.Target.Kind, s.capability.Target.ID)
}

func (s *runtimeHookSubscriber) Session() string { return "" }

func (s *runtimeHookSubscriber) ServesTenant(tenantID string) bool {
	if s == nil {
		return false
	}
	return tenantID == "" || len(s.capability.Tenants) == 0 || runtimeServesTenant(s.capability.Tenants, tenantID)
}

func (s *runtimeHookSubscriber) IsConnected() bool {
	return s != nil && s.conn != nil && len(s.capability.Hooks) > 0 && (s.capability.State == wire.CapabilityStateReady || s.capability.State == wire.CapabilityStateManifestLoaded)
}

func (s *runtimeHookSubscriber) IsBusy() bool {
	return s != nil && s.busy != nil && s.busy(s.runtimeID, s.capability.Target)
}

func (s *runtimeHookSubscriber) TryBusy() (func(), bool) {
	if s == nil || s.tryBusy == nil {
		return nil, true
	}
	return s.tryBusy(s.runtimeID, s.capability.Target)
}

func (s *runtimeHookSubscriber) BusyDone() <-chan struct{} {
	if s == nil || s.busyDone == nil {
		return nil
	}
	return s.busyDone(s.runtimeID, s.capability.Target)
}

func (s *runtimeHookSubscriber) Hooks() []khooks.Subscription {
	if s == nil {
		return nil
	}
	out := make([]khooks.Subscription, 0, len(s.capability.Hooks))
	for _, hook := range s.capability.Hooks {
		var timeout *int
		if hook.TimeoutMS != nil {
			v := int(*hook.TimeoutMS)
			timeout = &v
		}
		out = append(out, khooks.Subscription{Event: hook.Event, Priority: hook.Priority, TimeoutMs: timeout})
	}
	return out
}

func (s *runtimeHookSubscriber) SendHook(msg *khooks.Message) {
	if s == nil || s.conn == nil || msg == nil || msg.Type != string(MsgHook) {
		return
	}
	replyMode := khooks.ReplyMode(msg.Name)
	if replyMode != wire.HookReplyModeNone && msg.Release == nil && s.markBusy != nil {
		msg.Release = s.markBusy(s.runtimeID, s.capability.Target)
	}
	callID := msg.ID
	event := wire.HookEvent{
		Op:        wire.OpHookEvent,
		TenantID:  msg.TenantID,
		CallID:    callID,
		Target:    s.capability.Target,
		Event:     msg.Name,
		ReplyMode: replyMode,
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

func (s *runtimeHookSubscriber) Done() <-chan struct{} {
	if s == nil || s.done == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return s.done
}

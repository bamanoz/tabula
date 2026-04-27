package kernel

import (
	"encoding/json"

	"github.com/bamanoz/tabula/internal/kernel/plugin"
)

// pluginHookSubscriber adapts a plugin.Handle to the kernel HookSubscriber
// interface without making the plugin subpackage import its parent package.
// This keeps the supervision domains separated while letting HookEngine use
// the same priority/pending-hook path for WebSocket clients and stdio plugins
// (creative-plugin-runtime.md §2 / §11).
type pluginHookSubscriber struct {
	h *plugin.Handle
}

func newPluginHookSubscriber(h *plugin.Handle) HookSubscriber {
	return &pluginHookSubscriber{h: h}
}

func (s *pluginHookSubscriber) Name() string {
	if s == nil || s.h == nil {
		return ""
	}
	return s.h.ID()
}

func (s *pluginHookSubscriber) Session() string { return "" }

func (s *pluginHookSubscriber) IsConnected() bool {
	return s != nil && s.h != nil && s.h.IsAlive() && s.h.IsRegistered()
}

func (s *pluginHookSubscriber) Hooks() []HookSubscription {
	if s == nil || s.h == nil {
		return nil
	}
	subs := s.h.Subscriptions()
	out := make([]HookSubscription, 0, len(subs))
	for _, sub := range subs {
		out = append(out, HookSubscription{
			Event:     sub.Event,
			Priority:  sub.Priority,
			TimeoutMs: sub.TimeoutMs,
		})
	}
	return out
}

func (s *pluginHookSubscriber) SendMsg(msg *Message) {
	if s == nil || s.h == nil || msg == nil {
		return
	}
	if msg.Type != string(MsgHook) {
		return
	}
	params := &plugin.EventParams{
		CallID:  msg.ID,
		Event:   msg.Name,
		Data:    json.RawMessage(msg.Payload),
		Session: msg.Session,
	}
	_ = s.h.SendEvent(params)
}

func (s *pluginHookSubscriber) Done() <-chan struct{} {
	if s == nil || s.h == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return s.h.Done()
}

package kernel

// HookSubscriber is anything that can receive bus hook events.
//
// Both *Client (WebSocket-attached or internal channel-attached) and
// runtime-owned hook subscribers implement this interface so the HookEngine can
// dispatch events uniformly without conflating transport domains.
//
// Per creative `memory-bank/creative/creative-plugin-runtime.md` §2, this
// is the chosen abstraction (option C) over pseudo-Client (A) or a
// separate dispatcher (B): minimal surface, zero duplication of priority
// and pending-hook logic in HookEngine.
type HookSubscriber interface {
	Name() string
	Session() string
	ServesTenant(string) bool
	IsConnected() bool
	IsBusy() bool
	Hooks() []HookSubscription
	SendMsg(*Message)
	Done() <-chan struct{}
}

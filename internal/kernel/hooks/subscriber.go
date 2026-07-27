package hooks

// Subscriber is anything that can receive hook events.
type Subscriber interface {
	Name() string
	Session() string
	ServesTenant(string) bool
	IsConnected() bool
	IsBusy() bool
	Hooks() []Subscription
	SendHook(*Message)
	Done() <-chan struct{}
}

// BusySubscriber is a hook subscriber backed by a runtime target that can run
// only one reply-producing hook at a time.
type BusySubscriber interface {
	Subscriber
	RuntimeID() string
	TryBusy() (func(), bool)
	BusyDone() <-chan struct{}
}

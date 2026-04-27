package plugin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	DefaultBackoffInitial = time.Second
	DefaultBackoffMax     = 30 * time.Second
	DefaultMaxRestarts    = 5
	DefaultRestartWindow  = time.Minute
	DefaultCleanRunReset  = 120 * time.Second
)

// Supervisor wraps a Runtime with the crash-recovery policy frozen in
// creative-plugin-protocol.md §2.8. It is intentionally process-mechanics-only:
// the parent kernel package remains responsible for installing/removing handles
// from Hub registries when OnStart/OnExit callbacks fire.
type Supervisor struct {
	runtime Runtime
	policy  SupervisorPolicy
	clock   supervisorClock
}

// NonRestartableError marks plugin startup failures that are configuration or
// protocol-contract errors rather than transient crashes. Supervisor will not
// retry errors of this type (creative-plugin-protocol.md §2.6/§2.8).
type NonRestartableError struct{ Err error }

func (e *NonRestartableError) Error() string {
	if e == nil || e.Err == nil {
		return "plugin supervisor: non-restartable error"
	}
	return e.Err.Error()
}

func (e *NonRestartableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NonRestartable wraps err so Supervisor stops immediately instead of applying
// crash-recovery backoff. Nil is preserved.
func NonRestartable(err error) error {
	if err == nil {
		return nil
	}
	return &NonRestartableError{Err: err}
}

// SupervisorPolicy configures restart/backoff behaviour for plugin processes.
// Zero values are filled from the protocol defaults: 1s initial backoff, 30s
// cap, 5 restarts per 60s rolling window, and counter reset after 120s clean
// run.
type SupervisorPolicy struct {
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	MaxRestarts    int
	RestartWindow  time.Duration
	CleanRunReset  time.Duration
}

// SupervisorOptions configures a single supervised plugin lifecycle.
type SupervisorOptions struct {
	SpawnOptions
	Policy SupervisorPolicy
	// OnStart runs after Runtime.Spawn succeeds and before Monitor returns.
	// Hub.RegisterPlugin will use this to install the fresh handle in the plugin
	// registry and dispatch table.
	OnStart func(*Handle)
	// OnRestart runs after a restartable spawn failure or process exit is accepted
	// by the restart budget and before the backoff sleep. restartCount is the
	// cumulative number of scheduled restarts in the current rolling window;
	// nextBackoff is the delay before the next Spawn attempt.
	OnRestart func(handle *Handle, err error, restartCount int, nextBackoff time.Duration)
	// OnFinalExit runs once the supervisor stops retrying (context cancellation,
	// configuration failure, shutdown, or restart budget exhaustion).
	OnFinalExit func(*Handle, error)
	// Sleep is an injectable backoff hook for tests. Production callers leave it
	// nil, in which case time.NewTimer is used and cancellation is honoured.
	Sleep func(context.Context, time.Duration) error
}

// Supervise starts and restarts one plugin until ctx is cancelled, the restart
// budget is exhausted, or Runtime.Spawn returns a non-restartable error (for
// example manifest/register schema failures). Runtime.OnExit is wrapped so the
// supervisor can distinguish expected restart-triggering process exits from
// explicit shutdown/cancellation.
func (s *Supervisor) Supervise(ctx context.Context, manifest *Manifest, config map[string]any, opts SupervisorOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil {
		return errors.New("plugin supervisor: nil supervisor")
	}
	runtime := s.runtime
	if runtime == nil {
		runtime = NewRuntime()
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	policy := withSupervisorDefaults(mergeSupervisorPolicy(s.policy, opts.Policy))
	clock := s.clock
	if clock == nil {
		clock = realSupervisorClock{}
	}

	sleep := opts.Sleep
	if sleep == nil {
		sleep = clock.Sleep
	}

	var current *Handle
	restarts := make([]time.Time, 0, policy.MaxRestarts)
	backoff := policy.InitialBackoff

	for {
		select {
		case <-ctx.Done():
			return finishSupervision(opts.OnFinalExit, current, fmt.Errorf("plugin supervisor: stopped for %s: %w", manifestID(manifest), ctx.Err()))
		default:
		}

		exitCh := make(chan error, 1)
		spawnOpts := opts.SpawnOptions
		spawnOpts.OnExit = func(handle *Handle, err error) {
			if opts.SpawnOptions.OnExit != nil {
				opts.SpawnOptions.OnExit(handle, err)
			}
			select {
			case exitCh <- err:
			default:
			}
		}

		handle, err := runtime.Spawn(ctx, manifest, config, spawnOpts)
		if err != nil {
			if !isRestartableSpawnError(err) {
				return finishSupervision(opts.OnFinalExit, current, err)
			}
			if !allowRestart(clock.Now(), &restarts, policy) {
				return finishSupervision(opts.OnFinalExit, current, fmt.Errorf("plugin supervisor: restart budget exhausted for %s after spawn failure: %w", manifestID(manifest), err))
			}
			notifyRestart(opts.OnRestart, current, err, len(restarts), backoff)
			logger.Warn("plugin spawn failed; scheduling restart", "plugin", manifestID(manifest), "backoff", backoff, "err", err)
			if err := sleep(ctx, backoff); err != nil {
				return finishSupervision(opts.OnFinalExit, current, err)
			}
			backoff = nextBackoff(backoff, policy.MaxBackoff)
			continue
		}

		current = handle
		if opts.OnStart != nil {
			opts.OnStart(handle)
		}

		select {
		case <-ctx.Done():
			_ = handle.SendShutdown()
			handle.Close()
			return finishSupervision(opts.OnFinalExit, handle, fmt.Errorf("plugin supervisor: stopped for %s: %w", handle.ID(), ctx.Err()))
		case err := <-exitCh:
			runDuration := clock.Since(handle.StartedAt())
			if runDuration >= policy.CleanRunReset {
				restarts = restarts[:0]
				backoff = policy.InitialBackoff
			}
			if !allowRestart(clock.Now(), &restarts, policy) {
				finalErr := fmt.Errorf("plugin supervisor: restart budget exhausted for %s after exit: %w", handle.ID(), err)
				logger.Error("plugin restart budget exhausted", "plugin", handle.ID(), "err", err)
				return finishSupervision(opts.OnFinalExit, handle, finalErr)
			}
			notifyRestart(opts.OnRestart, handle, err, len(restarts), backoff)
			logger.Warn("plugin exited; scheduling restart", "plugin", handle.ID(), "backoff", backoff, "err", err)
			if err := sleep(ctx, backoff); err != nil {
				return finishSupervision(opts.OnFinalExit, handle, err)
			}
			backoff = nextBackoff(backoff, policy.MaxBackoff)
		}
	}
}

// NewSupervisor constructs a Supervisor around runtime. Passing nil uses the
// default in-tree Runtime when Supervise is called.
func NewSupervisor(runtime Runtime, policy SupervisorPolicy) *Supervisor {
	return &Supervisor{runtime: runtime, policy: policy, clock: realSupervisorClock{}}
}

func mergeSupervisorPolicy(base, override SupervisorPolicy) SupervisorPolicy {
	if override.InitialBackoff > 0 {
		base.InitialBackoff = override.InitialBackoff
	}
	if override.MaxBackoff > 0 {
		base.MaxBackoff = override.MaxBackoff
	}
	if override.MaxRestarts > 0 {
		base.MaxRestarts = override.MaxRestarts
	}
	if override.RestartWindow > 0 {
		base.RestartWindow = override.RestartWindow
	}
	if override.CleanRunReset > 0 {
		base.CleanRunReset = override.CleanRunReset
	}
	return base
}

func withSupervisorDefaults(policy SupervisorPolicy) SupervisorPolicy {
	if policy.InitialBackoff <= 0 {
		policy.InitialBackoff = DefaultBackoffInitial
	}
	if policy.MaxBackoff <= 0 {
		policy.MaxBackoff = DefaultBackoffMax
	}
	if policy.MaxRestarts <= 0 {
		policy.MaxRestarts = DefaultMaxRestarts
	}
	if policy.RestartWindow <= 0 {
		policy.RestartWindow = DefaultRestartWindow
	}
	if policy.CleanRunReset <= 0 {
		policy.CleanRunReset = DefaultCleanRunReset
	}
	if policy.MaxBackoff < policy.InitialBackoff {
		policy.MaxBackoff = policy.InitialBackoff
	}
	return policy
}

func allowRestart(now time.Time, restarts *[]time.Time, policy SupervisorPolicy) bool {
	cutoff := now.Add(-policy.RestartWindow)
	kept := (*restarts)[:0]
	for _, ts := range *restarts {
		if !ts.Before(cutoff) {
			kept = append(kept, ts)
		}
	}
	*restarts = kept
	if len(*restarts) >= policy.MaxRestarts {
		return false
	}
	*restarts = append(*restarts, now)
	return true
}

func nextBackoff(current, max time.Duration) time.Duration {
	if current <= 0 {
		current = DefaultBackoffInitial
	}
	next := current * 2
	if next > max {
		return max
	}
	return next
}

func notifyRestart(onRestart func(*Handle, error, int, time.Duration), handle *Handle, err error, restartCount int, nextBackoff time.Duration) {
	if onRestart != nil {
		onRestart(handle, err, restartCount, nextBackoff)
	}
}

func finishSupervision(onFinalExit func(*Handle, error), handle *Handle, err error) error {
	if onFinalExit != nil {
		onFinalExit(handle, err)
	}
	return err
}

func isRestartableSpawnError(err error) bool {
	if err == nil {
		return false
	}
	var manifestErr *ManifestError
	if errors.As(err, &manifestErr) {
		return false
	}
	var nonRestartable *NonRestartableError
	if errors.As(err, &nonRestartable) {
		return false
	}
	return true
}

func manifestID(m *Manifest) string {
	if m == nil || m.ID == "" {
		return "<unknown>"
	}
	return m.ID
}

type supervisorClock interface {
	Now() time.Time
	Since(time.Time) time.Duration
	Sleep(context.Context, time.Duration) error
}

type realSupervisorClock struct{}

func (realSupervisorClock) Now() time.Time { return time.Now() }

func (realSupervisorClock) Since(t time.Time) time.Duration { return time.Since(t) }

func (realSupervisorClock) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("plugin supervisor: sleep cancelled: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

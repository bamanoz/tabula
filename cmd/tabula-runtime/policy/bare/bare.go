// Package bare contains the M2 bare worker policy.
package bare

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bamanoz/tabula/cmd/tabula-runtime/policy"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

const defaultInitTimeout = 10 * time.Second

// Policy spawns workers directly with os/exec. It is the M2 default policy;
// stronger sandbox policies plug in behind the same policy.PluginExecPolicy
// interface in later milestones.
type Policy struct {
	RuntimeCommands map[string]string
	InitTimeout     time.Duration
}

var _ policy.PluginExecPolicy = (*Policy)(nil)

// New returns a bare worker policy.
func New() *Policy { return &Policy{} }

// Spawn starts the configured worker process and returns a handle ready for the
// WorkerInit handshake. The caller remains responsible for calling Worker.Init
// so alternate policies can separate process creation from protocol readiness.
func (p *Policy) Spawn(ctx context.Context, req policy.SpawnReq) (policy.Worker, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateSpawnReq(req); err != nil {
		return nil, err
	}
	cmd, err := p.buildCommand(ctx, req)
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("bare policy: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("bare policy: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("bare policy: stderr pipe: %w", err)
	}
	configureWorkerProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("bare policy: start %s: %w", req.TargetID, err)
	}
	w := &worker{
		cmd:            cmd,
		stdin:          stdin,
		stdout:         bufio.NewReader(stdout),
		stderr:         stderr,
		mode:           spawnMode(req.Mode),
		initTimeout:    p.initTimeout(),
		alive:          true,
		done:           make(chan struct{}),
		pendingEvents:  map[string]chan eventResult{},
		pendingCalls:   map[string]chan callResult{},
		ignoredEvents:  map[string]struct{}{},
		ignoredResults: map[string]struct{}{},
		events:         make(chan policy.WorkerAsyncEvent, 64),
	}
	go w.readLoop()
	go w.wait()
	return w, nil
}

func spawnMode(mode policy.SpawnMode) policy.SpawnMode {
	if mode == "" {
		return policy.SpawnModeWarm
	}
	return mode
}

func validateSpawnReq(req policy.SpawnReq) error {
	if strings.TrimSpace(req.KernelID) == "" {
		return errors.New("bare policy: kernel id is required")
	}
	if strings.TrimSpace(req.TenantID) == "" {
		return errors.New("bare policy: tenant id is required")
	}
	if strings.TrimSpace(req.TargetID) == "" {
		return errors.New("bare policy: target id is required")
	}
	if strings.TrimSpace(req.Runtime) == "" {
		return errors.New("bare policy: runtime is required")
	}
	if strings.TrimSpace(req.Entry) == "" {
		return errors.New("bare policy: entry is required")
	}
	if strings.TrimSpace(req.WorkingDir) == "" {
		return errors.New("bare policy: working dir is required")
	}
	switch req.Mode {
	case "":
		return nil
	case policy.SpawnModeWarm, policy.SpawnModeCold:
		return nil
	default:
		return fmt.Errorf("bare policy: unsupported spawn mode %q", req.Mode)
	}
}

func (p *Policy) buildCommand(ctx context.Context, req policy.SpawnReq) (*exec.Cmd, error) {
	bin := p.runtimeCommand(req.Runtime)
	if bin == "" {
		return nil, fmt.Errorf("bare policy: no command configured for runtime %q", req.Runtime)
	}
	entry := req.Entry
	if !filepath.IsAbs(entry) {
		entry = filepath.Join(req.WorkingDir, entry)
	}
	cmd := exec.CommandContext(ctx, bin, entry)
	cmd.Dir = req.WorkingDir
	cmd.Env = workerEnv(req)
	return cmd, nil
}

func (p *Policy) runtimeCommand(runtime string) string {
	if p != nil && p.RuntimeCommands != nil && p.RuntimeCommands[runtime] != "" {
		return p.RuntimeCommands[runtime]
	}
	switch runtime {
	case "python":
		return "python3"
	case "bash":
		return "bash"
	case "node":
		return "node"
	default:
		return ""
	}
}

func (p *Policy) initTimeout() time.Duration {
	if p != nil && p.InitTimeout > 0 {
		return p.InitTimeout
	}
	return defaultInitTimeout
}

func workerEnv(req policy.SpawnReq) []string {
	env := passthroughEnv(os.Environ())
	for key, value := range req.Env {
		if strings.TrimSpace(key) == "" {
			continue
		}
		env = setEnv(env, key, value)
	}
	env = setEnv(env, "TABULA_KERNEL_ID", req.KernelID)
	env = setEnv(env, "TABULA_TENANT_ID", req.TenantID)
	env = setEnv(env, "TABULA_TARGET_ID", req.TargetID)
	return env
}

func passthroughEnv(current []string) []string {
	allowed := map[string]struct{}{
		"TABULA_HOME": {},
		"PATH":        {},
		"HOME":        {},
		"LANG":        {},
		"LC_ALL":      {},
		"TMPDIR":      {},
	}
	out := make([]string, 0, len(allowed))
	for _, item := range current {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if _, keep := allowed[key]; keep {
			out = append(out, item)
		}
	}
	return out
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

type worker struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      *bufio.Reader
	stderr      io.ReadCloser
	mode        policy.SpawnMode
	initTimeout time.Duration

	mu             sync.Mutex
	called         bool
	inFlight       bool
	alive          bool
	exitInfo       policy.ExitInfo
	waitErr        error
	done           chan struct{}
	closeOnce      sync.Once
	initResult     chan initResult
	pendingCalls   map[string]chan callResult
	pendingEvents  map[string]chan eventResult
	ignoredResults map[string]struct{}
	ignoredEvents  map[string]struct{}
	events         chan policy.WorkerAsyncEvent
}

type initResult struct {
	ack workerwire.WorkerInitAck
	err error
}

type callResult struct {
	result workerwire.WorkerResult
	err    error
}

type eventResult struct {
	reply *workerwire.WorkerEventReply
	err   error
}

func (w *worker) Init(ctx context.Context, init workerwire.WorkerInit) (workerwire.WorkerInitAck, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	init.Op = workerwire.OpInit
	if init.Manifest == nil {
		init.Manifest = []byte(`{}`)
	}
	timeout := w.initTimeout
	if timeout <= 0 {
		timeout = defaultInitTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resultCh := make(chan initResult, 1)
	w.mu.Lock()
	w.initResult = resultCh
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		if w.initResult == resultCh {
			w.initResult = nil
		}
		w.mu.Unlock()
	}()
	if err := w.writeFrame(ctx, &init); err != nil {
		_ = w.kill()
		return workerwire.WorkerInitAck{}, fmt.Errorf("bare policy: send worker init: %w", err)
	}
	var result initResult
	select {
	case result = <-resultCh:
	case <-ctx.Done():
		_ = w.kill()
		return workerwire.WorkerInitAck{}, ctx.Err()
	case <-w.done:
		_ = w.kill()
		return workerwire.WorkerInitAck{}, errors.New("bare policy: worker exited before init ack")
	}
	if result.err != nil {
		_ = w.kill()
		return workerwire.WorkerInitAck{}, fmt.Errorf("bare policy: read worker init ack: %w", result.err)
	}
	ack := result.ack
	if !ack.Ready {
		msg := "worker init rejected"
		if ack.Error != nil && ack.Error.Message != "" {
			msg = ack.Error.Message
		}
		_ = w.kill()
		return workerwire.WorkerInitAck{}, fmt.Errorf("bare policy: %s", msg)
	}
	return ack, nil
}

func (w *worker) Call(ctx context.Context, call workerwire.WorkerCall) (workerwire.WorkerResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	w.mu.Lock()
	select {
	case <-ctx.Done():
		w.mu.Unlock()
		return workerwire.WorkerResult{}, ctx.Err()
	default:
	}
	if !w.isAliveLocked() {
		w.mu.Unlock()
		return workerwire.WorkerResult{}, errors.New("bare policy: worker is not alive")
	}
	if w.mode == policy.SpawnModeCold && w.called {
		w.mu.Unlock()
		return workerwire.WorkerResult{}, errors.New("bare policy: cold worker already handled a call")
	}
	if w.inFlight {
		w.mu.Unlock()
		return workerwire.WorkerResult{}, errors.New("bare policy: worker already has an in-flight call")
	}
	w.called = true
	w.inFlight = true
	call.Op = workerwire.OpCall
	resultCh := make(chan callResult, 1)
	w.pendingCalls[call.CallID] = resultCh
	if err := w.writeFrame(ctx, &call); err != nil {
		delete(w.pendingCalls, call.CallID)
		w.inFlight = false
		w.mu.Unlock()
		return workerwire.WorkerResult{}, fmt.Errorf("bare policy: send worker call: %w", err)
	}
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.inFlight = false
		delete(w.pendingCalls, call.CallID)
		w.mu.Unlock()
	}()

	select {
	case out := <-resultCh:
		if out.err != nil {
			return workerwire.WorkerResult{}, fmt.Errorf("bare policy: read worker result: %w", out.err)
		}
		return out.result, nil
	case <-ctx.Done():
		w.mu.Lock()
		delete(w.pendingCalls, call.CallID)
		w.ignoredResults[call.CallID] = struct{}{}
		w.mu.Unlock()
		return workerwire.WorkerResult{}, ctx.Err()
	case <-w.done:
		return workerwire.WorkerResult{}, errors.New("bare policy: worker exited before result")
	}
}

func (w *worker) HookEvent(ctx context.Context, event workerwire.WorkerEvent) (*workerwire.WorkerEventReply, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	w.mu.Lock()
	select {
	case <-ctx.Done():
		w.mu.Unlock()
		return nil, ctx.Err()
	default:
	}
	if !w.isAliveLocked() {
		w.mu.Unlock()
		return nil, errors.New("bare policy: worker is not alive")
	}
	if w.inFlight {
		w.mu.Unlock()
		return nil, errors.New("bare policy: worker already has an in-flight call")
	}
	w.inFlight = true
	event.Op = workerwire.OpEvent
	var resultCh chan eventResult
	if event.ReplyMode != workerwire.ReplyModeNone {
		resultCh = make(chan eventResult, 1)
		w.pendingEvents[event.CallID] = resultCh
	}
	if err := w.writeFrame(ctx, &event); err != nil {
		delete(w.pendingEvents, event.CallID)
		w.inFlight = false
		w.mu.Unlock()
		return nil, fmt.Errorf("bare policy: send worker event: %w", err)
	}
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.inFlight = false
		delete(w.pendingEvents, event.CallID)
		w.mu.Unlock()
	}()

	if event.ReplyMode == workerwire.ReplyModeNone {
		return nil, nil
	}

	select {
	case out := <-resultCh:
		if out.err != nil {
			return nil, fmt.Errorf("bare policy: read worker event reply: %w", out.err)
		}
		return out.reply, nil
	case <-ctx.Done():
		w.mu.Lock()
		delete(w.pendingEvents, event.CallID)
		w.ignoredEvents[event.CallID] = struct{}{}
		w.mu.Unlock()
		return nil, ctx.Err()
	case <-w.done:
		return nil, errors.New("bare policy: worker exited before event reply")
	}
}

func (w *worker) Events() <-chan policy.WorkerAsyncEvent { return w.events }

func (w *worker) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	w.mu.Lock()
	if !w.isAliveLocked() {
		w.mu.Unlock()
		return nil
	}
	_ = workerwire.WriteFrame(w.stdin, &workerwire.WorkerShutdown{Op: workerwire.OpShutdown, Reason: "runtime shutdown"})
	w.mu.Unlock()
	if err := w.waitWithin(ctx, 5*time.Second); err == nil {
		return nil
	}
	_ = w.terminate()
	if err := w.waitWithin(ctx, 5*time.Second); err == nil {
		return nil
	}
	return w.kill()
}

func (w *worker) Wait() (policy.ExitInfo, error) {
	<-w.done
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.exitInfo, w.waitErr
}

func (w *worker) IsAlive() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.isAliveLocked()
}

func (w *worker) isAliveLocked() bool {
	return w.alive
}

func (w *worker) wait() {
	err := w.cmd.Wait()
	w.mu.Lock()
	w.alive = false
	w.waitErr = err
	w.exitInfo = exitInfo(err)
	w.mu.Unlock()
	w.closePipes()
	close(w.done)
}

func (w *worker) writeFrame(ctx context.Context, frame any) error {
	return runWithContext(ctx, func() error { return workerwire.WriteFrame(w.stdin, frame) })
}

func runWithContext(ctx context.Context, fn func() error) error {
	result := make(chan error, 1)
	go func() { result <- fn() }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *worker) waitWithin(ctx context.Context, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-w.done:
		return nil
	case <-timer.C:
		return context.DeadlineExceeded
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *worker) terminate() error {
	if w.cmd == nil || w.cmd.Process == nil {
		return nil
	}
	return terminateWorkerProcessGroup(w.cmd)
}

func (w *worker) kill() error {
	if w.cmd == nil || w.cmd.Process == nil {
		return nil
	}
	return w.cmd.Process.Kill()
}

func (w *worker) closePipes() {
	w.closeOnce.Do(func() {
		_ = w.stdin.Close()
		_ = w.stderr.Close()
	})
}

func (w *worker) readLoop() {
	defer w.closeEvents()
	for {
		env, frame, err := workerwire.ReadFrame(w.stdout)
		if err != nil {
			w.failPending(err)
			w.publishAsync(policy.WorkerAsyncEvent{Envelope: env, Err: err})
			return
		}
		switch msg := frame.(type) {
		case *workerwire.WorkerInitAck:
			w.deliverInit(*msg)
		case *workerwire.WorkerResult:
			if !w.deliverCall(*msg) {
				err := fmt.Errorf("unexpected worker result call_id %q", msg.CallID)
				w.failPending(err)
				w.publishAsync(policy.WorkerAsyncEvent{Envelope: env, Err: err})
				return
			}
		case *workerwire.WorkerEventReply:
			if !w.deliverEvent(*msg) {
				err := fmt.Errorf("unexpected worker event_reply call_id %q", msg.CallID)
				w.failPending(err)
				w.publishAsync(policy.WorkerAsyncEvent{Envelope: env, Err: err})
				return
			}
		case *workerwire.WorkerToolsUpdated, *workerwire.WorkerSend, *workerwire.WorkerLog, *workerwire.WorkerError:
			w.publishAsync(policy.WorkerAsyncEvent{Envelope: env, Frame: frame})
		default:
			err := fmt.Errorf("unexpected worker frame %T", frame)
			w.failPending(err)
			w.publishAsync(policy.WorkerAsyncEvent{Envelope: env, Err: err})
			return
		}
	}
}

func (w *worker) deliverInit(ack workerwire.WorkerInitAck) {
	w.mu.Lock()
	ch := w.initResult
	w.initResult = nil
	w.mu.Unlock()
	if ch != nil {
		ch <- initResult{ack: ack}
	}
}

func (w *worker) deliverCall(result workerwire.WorkerResult) bool {
	w.mu.Lock()
	if _, ignore := w.ignoredResults[result.CallID]; ignore {
		delete(w.ignoredResults, result.CallID)
		w.mu.Unlock()
		return true
	}
	ch := w.pendingCalls[result.CallID]
	w.mu.Unlock()
	if ch == nil {
		return false
	}
	ch <- callResult{result: result}
	return true
}

func (w *worker) deliverEvent(reply workerwire.WorkerEventReply) bool {
	w.mu.Lock()
	if _, ignore := w.ignoredEvents[reply.CallID]; ignore {
		delete(w.ignoredEvents, reply.CallID)
		w.mu.Unlock()
		return true
	}
	ch := w.pendingEvents[reply.CallID]
	w.mu.Unlock()
	if ch == nil {
		return false
	}
	ch <- eventResult{reply: &reply}
	return true
}

func (w *worker) failPending(err error) {
	w.mu.Lock()
	initCh := w.initResult
	w.initResult = nil
	pending := make([]chan callResult, 0, len(w.pendingCalls))
	pendingEvents := make([]chan eventResult, 0, len(w.pendingEvents))
	for _, ch := range w.pendingCalls {
		pending = append(pending, ch)
	}
	for _, ch := range w.pendingEvents {
		pendingEvents = append(pendingEvents, ch)
	}
	w.pendingCalls = map[string]chan callResult{}
	w.pendingEvents = map[string]chan eventResult{}
	w.mu.Unlock()
	if initCh != nil {
		initCh <- initResult{err: err}
	}
	for _, ch := range pending {
		ch <- callResult{err: err}
	}
	for _, ch := range pendingEvents {
		ch <- eventResult{err: err}
	}
}

func (w *worker) publishAsync(event policy.WorkerAsyncEvent) {
	select {
	case w.events <- event:
	case <-w.done:
	}
}

func (w *worker) closeEvents() {
	defer func() { _ = recover() }()
	close(w.events)
}

func exitInfo(err error) policy.ExitInfo {
	info := policy.ExitInfo{Code: 0}
	if err == nil {
		return info
	}
	info.Message = err.Error()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		info.Code = exitErr.ExitCode()
		info.Signaled = info.Code < 0
		return info
	}
	info.Code = -1
	return info
}

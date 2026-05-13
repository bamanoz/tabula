package bash

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy"
	runtimewire "github.com/bamanoz/tabula/internal/runtime/wire"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

const shutdownGrace = 5 * time.Second

// Worker is a cold in-process harness that executes one skill exec template and
// speaks the runtime worker interface on the skill's behalf.
type Worker struct {
	req   policy.SpawnReq
	skill manifest.Skill

	mu      sync.Mutex
	inited  bool
	called  bool
	alive   bool
	running *exec.Cmd
	done    chan struct{}
	closed  bool
	exit    policy.ExitInfo
	waitErr error
	events  chan policy.WorkerAsyncEvent
}

func New(req policy.SpawnReq) (*Worker, error) {
	var skill manifest.Skill
	if err := json.Unmarshal(req.Manifest, &skill); err != nil {
		return nil, fmt.Errorf("bash harness: decode skill manifest: %w", err)
	}
	if err := skill.Validate(); err != nil {
		return nil, fmt.Errorf("bash harness: invalid skill manifest: %w", err)
	}
	if strings.TrimSpace(req.WorkingDir) == "" {
		return nil, fmt.Errorf("bash harness: working dir is required")
	}
	return &Worker{req: req, skill: skill, alive: true, done: make(chan struct{}), events: closedEvents()}, nil
}

func (w *Worker) Init(context.Context, workerwire.WorkerInit) (workerwire.WorkerInitAck, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.alive {
		return workerwire.WorkerInitAck{}, fmt.Errorf("bash harness: worker not alive")
	}
	w.inited = true
	capability := w.skill.Capability()
	return workerwire.WorkerInitAck{Op: workerwire.OpInitAck, Ready: true, Tools: capability.Tools, Subscriptions: capability.Hooks}, nil
}

func (w *Worker) Call(ctx context.Context, call workerwire.WorkerCall) (workerwire.WorkerResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	w.mu.Lock()
	if !w.alive {
		w.mu.Unlock()
		return workerwire.WorkerResult{}, fmt.Errorf("bash harness: worker not alive")
	}
	if !w.inited {
		w.mu.Unlock()
		return workerwire.WorkerResult{}, fmt.Errorf("bash harness: worker not initialized")
	}
	if w.called {
		w.mu.Unlock()
		return workerwire.WorkerResult{}, fmt.Errorf("bash harness: cold worker cannot be reused")
	}
	w.called = true
	tool, ok := w.skill.Tool(call.Tool)
	if !ok {
		w.mu.Unlock()
		w.finish(policy.ExitInfo{Code: 1, Message: "tool not found"}, fmt.Errorf("bash harness: tool %q not found", call.Tool))
		return workerwire.WorkerResult{}, fmt.Errorf("bash harness: tool %q not found", call.Tool)
	}
	argv, err := buildCommand(tool.Exec, w.req.WorkingDir, w.tabulaHome())
	if err != nil {
		w.mu.Unlock()
		w.finish(policy.ExitInfo{Code: 1, Message: err.Error()}, err)
		return workerwire.WorkerResult{}, err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = w.req.WorkingDir
	cmd.Env = skillEnv(w.req, w.req.WorkingDir, call.Tool, call.CallID, call.SessionID)
	configureProcessGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		w.mu.Unlock()
		w.finish(policy.ExitInfo{Code: 1, Message: err.Error()}, err)
		return workerwire.WorkerResult{}, fmt.Errorf("bash harness: stdin pipe: %w", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		w.mu.Unlock()
		w.finish(policy.ExitInfo{Code: 1, Message: err.Error()}, err)
		return workerwire.WorkerResult{}, fmt.Errorf("bash harness: start: %w", err)
	}
	w.running = cmd
	w.mu.Unlock()

	writeDone := make(chan error, 1)
	go func() {
		if len(call.Args) > 0 {
			_, _ = stdin.Write(call.Args)
		}
		writeDone <- stdin.Close()
	}()

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	select {
	case err := <-writeDone:
		if err != nil {
			_ = terminateProcessGroup(cmd)
			<-waitDone
			w.finish(exitInfo(err), err)
			return workerwire.WorkerResult{}, fmt.Errorf("bash harness: write stdin: %w", err)
		}
	case <-ctx.Done():
		terminateAndWait(cmd, waitDone)
		w.finish(policy.ExitInfo{Code: -1, Message: ctx.Err().Error()}, ctx.Err())
		return workerwire.WorkerResult{}, ctx.Err()
	}

	select {
	case err := <-waitDone:
		result, resultErr := w.resultForCall(call.CallID, stdout.Bytes(), stderr.Bytes(), err)
		w.finish(exitInfo(err), err)
		return result, resultErr
	case <-ctx.Done():
		terminateAndWait(cmd, waitDone)
		w.finish(policy.ExitInfo{Code: -1, Message: ctx.Err().Error()}, ctx.Err())
		return workerwire.WorkerResult{}, ctx.Err()
	}
}

func (w *Worker) HookEvent(context.Context, workerwire.WorkerEvent) (*workerwire.WorkerEventReply, error) {
	return nil, fmt.Errorf("bash harness: skills do not accept hook events")
}

func (w *Worker) Events() <-chan policy.WorkerAsyncEvent { return w.events }

func (w *Worker) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	w.mu.Lock()
	cmd := w.running
	alive := w.alive
	w.mu.Unlock()
	if !alive {
		return nil
	}
	if cmd == nil {
		w.finish(policy.ExitInfo{}, nil)
		return nil
	}
	done := make(chan struct{})
	go func() {
		terminateAndKill(cmd)
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *Worker) Wait() (policy.ExitInfo, error) {
	<-w.done
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.exit, w.waitErr
}

func (w *Worker) IsAlive() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.alive
}

func (w *Worker) tabulaHome() string {
	if value := strings.TrimSpace(w.req.Env["TABULA_HOME"]); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("TABULA_HOME"))
}

func (w *Worker) resultForCall(callID string, stdout, stderr []byte, waitErr error) (workerwire.WorkerResult, error) {
	if waitErr != nil {
		if code, message, ok := workerErrorFromStdout(stdout); ok {
			return workerwire.WorkerResult{Op: workerwire.OpResult, CallID: callID, OK: false, Error: &workerwire.WorkerErrorBody{Code: code, Message: message}}, nil
		}
		message := strings.TrimSpace(string(stderr))
		if message == "" {
			message = strings.TrimSpace(string(stdout))
		}
		if message == "" {
			message = "skill execution failed"
		}
		return workerwire.WorkerResult{Op: workerwire.OpResult, CallID: callID, OK: false, Error: &workerwire.WorkerErrorBody{Code: string(runtimewire.ErrorSkillExecFailed), Message: message}}, nil
	}
	data := bytes.TrimSpace(stdout)
	if len(data) == 0 {
		stderrData := bytes.TrimSpace(stderr)
		if len(stderrData) > 0 && json.Valid(stderrData) {
			data = stderrData
		}
	}
	if len(data) == 0 {
		data = []byte("null")
	}
	if !json.Valid(data) {
		return workerwire.WorkerResult{}, fmt.Errorf("bash harness: skill stdout is not valid JSON")
	}
	return workerwire.WorkerResult{Op: workerwire.OpResult, CallID: callID, OK: true, Data: json.RawMessage(append([]byte(nil), data...))}, nil
}

func workerErrorFromStdout(stdout []byte) (string, string, bool) {
	data := bytes.TrimSpace(stdout)
	if len(data) == 0 || !json.Valid(data) {
		return "", "", false
	}
	var direct struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &direct) == nil && strings.TrimSpace(direct.Code) != "" {
		return direct.Code, direct.Message, true
	}
	var wrapped struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &wrapped) == nil && wrapped.Error != nil && strings.TrimSpace(wrapped.Error.Code) != "" {
		return wrapped.Error.Code, wrapped.Error.Message, true
	}
	return "", "", false
}

func terminateAndWait(cmd *exec.Cmd, waitDone <-chan error) {
	if cmd == nil {
		return
	}
	_ = terminateProcessGroup(cmd)
	timer := time.NewTimer(shutdownGrace)
	select {
	case <-waitDone:
		timer.Stop()
	case <-timer.C:
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-waitDone
	}
}

func terminateAndKill(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	_ = terminateProcessGroup(cmd)
	if cmd.Process != nil {
		time.Sleep(shutdownGrace)
		_ = cmd.Process.Kill()
	}
}

func (w *Worker) finish(info policy.ExitInfo, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.running = nil
	w.alive = false
	w.exit = info
	w.waitErr = err
	if !w.closed {
		close(w.done)
		w.closed = true
	}
}

func skillEnv(req policy.SpawnReq, skillDir, toolName, callID, sessionID string) []string {
	allowed := map[string]struct{}{
		"TABULA_HOME": {},
		"TABULA_URL":  {},
		"PATH":        {},
		"PYTHONPATH":  {},
		"HOME":        {},
		"LANG":        {},
		"LC_ALL":      {},
		"TMPDIR":      {},
	}
	env := make([]string, 0, len(allowed)+len(req.Env)+8)
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			if _, keep := allowed[key]; keep {
				env = append(env, item)
			}
		}
	}
	for key, value := range req.Env {
		if strings.TrimSpace(key) != "" {
			env = setEnv(env, key, value)
		}
	}
	env = setEnv(env, "TABULA_KERNEL_ID", req.KernelID)
	env = setEnv(env, "TABULA_TENANT_ID", req.TenantID)
	if strings.TrimSpace(envValue(env, "TABULA_TENANT_DIR")) == "" {
		if home := tabulaHomeFromEnv(env); home != "" {
			env = setEnv(env, "TABULA_TENANT_DIR", filepath.Join(home, "tenants", req.TenantID))
		}
	}
	env = setEnv(env, "TABULA_TARGET_ID", req.TargetID)
	env = setEnv(env, "TABULA_SKILL_DIR", skillDir)
	env = setEnv(env, "TABULA_TOOL_NAME", toolName)
	env = setEnv(env, "TABULA_CALL_ID", callID)
	env = setEnv(env, "TABULA_SESSION", sessionID)
	return env
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

func envValue(env []string, key string) string {
	for _, item := range env {
		k, value, ok := strings.Cut(item, "=")
		if ok && k == key {
			return value
		}
	}
	return ""
}

func tabulaHomeFromEnv(env []string) string {
	if value := strings.TrimSpace(envValue(env, "TABULA_HOME")); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("TABULA_HOME"))
}

func buildCommand(execText, skillDir, tabulaHome string) ([]string, error) {
	replaced := strings.NewReplacer(
		"${SKILL_DIR}", skillDir,
		"${TABULA_HOME}", tabulaHome,
	).Replace(strings.TrimSpace(execText))
	argv, err := splitShellWords(replaced)
	if err != nil {
		return nil, err
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("bash harness: exec command is empty")
	}
	if !filepath.IsAbs(argv[0]) && strings.Contains(argv[0], string(filepath.Separator)) {
		argv[0] = filepath.Clean(filepath.Join(skillDir, argv[0]))
	}
	return argv, nil
}

func splitShellWords(input string) ([]string, error) {
	parts := []string{}
	var current strings.Builder
	quote := rune(0)
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			parts = append(parts, current.String())
			current.Reset()
		}
	}
	for _, ch := range input {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
				continue
			}
			current.WriteRune(ch)
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case ' ', '\t', '\n':
			flush()
		default:
			current.WriteRune(ch)
		}
	}
	if escaped {
		current.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("bash harness: unterminated quote in exec command")
	}
	flush()
	return parts, nil
}

func closedEvents() chan policy.WorkerAsyncEvent {
	ch := make(chan policy.WorkerAsyncEvent)
	close(ch)
	return ch
}

func exitInfo(err error) policy.ExitInfo {
	if err == nil {
		return policy.ExitInfo{Code: 0}
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return policy.ExitInfo{Code: exitErr.ExitCode(), Message: err.Error()}
	}
	return policy.ExitInfo{Code: 1, Message: err.Error()}
}

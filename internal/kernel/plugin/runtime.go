package plugin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

const (
	defaultRegisterTimeout = 10 * time.Second
	malformedWindow        = 10 * time.Second
	malformedThreshold     = 3
)

// Runtime starts plugin subprocesses, performs the register handshake, and
// forwards post-register protocol messages to the caller. The parent kernel
// package owns semantic dispatch; this subpackage owns stdio/process mechanics.
type Runtime interface {
	Spawn(ctx context.Context, manifest *Manifest, config map[string]any, opts SpawnOptions) (*Handle, error)
}

// SpawnOptions configures Runtime.Spawn without importing the parent kernel
// package (avoids an import cycle). Hub.RegisterPlugin will pass
// kernel.MinPluginProtocolVersion/MaxPluginProtocolVersion through
// MinProtocolVersion/MaxProtocolVersion.
type SpawnOptions struct {
	MinProtocolVersion int
	MaxProtocolVersion int
	RegisterTimeout    time.Duration
	Logger             *slog.Logger
	RuntimeCommands    map[string]string
	OnMessage          func(*Handle, *Message)
	OnStartProcess     func(*Handle, *exec.Cmd)
	OnExit             func(*Handle, error)
	ValidateRegister   func(*RegisterParams) error
}

// DefaultRuntime is the in-tree Runtime implementation backed by os/exec and
// NDJSON over plugin stdin/stdout.
type DefaultRuntime struct{}

func NewRuntime() Runtime { return &DefaultRuntime{} }

func (r *DefaultRuntime) Spawn(ctx context.Context, manifest *Manifest, config map[string]any, opts SpawnOptions) (*Handle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.MinProtocolVersion <= 0 || opts.MaxProtocolVersion <= 0 {
		return nil, errors.New("plugin runtime: min/max protocol version is required")
	}
	if opts.MinProtocolVersion > opts.MaxProtocolVersion {
		return nil, fmt.Errorf("plugin runtime: min protocol %d > max protocol %d", opts.MinProtocolVersion, opts.MaxProtocolVersion)
	}
	if err := ValidateManifest(manifest); err != nil {
		return nil, &ManifestError{Path: manifestPath(manifest), Err: err}
	}

	cmd, err := buildCommand(ctx, manifest, opts)
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin runtime: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin runtime: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin runtime: stderr pipe: %w", err)
	}

	handle := NewHandle(manifest.ID, mergeConfig(manifest.Config, config))
	handle.SetWriter(NewWriter(stdin))

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("plugin runtime: start %s: %w", manifest.ID, err)
	}
	handle.SetPID(cmd.Process.Pid)
	handle.MarkAlive()
	if opts.OnStartProcess != nil {
		opts.OnStartProcess(handle, cmd)
	}
	go forwardStderr(opts.Logger, manifest.ID, stderr)

	events := make(chan runtimeEvent, 16)
	go readLoop(handle, NewReader(stdout), opts.Logger, events)
	go waitForExit(handle, cmd, opts.OnExit)

	if err := handle.SendRegisterRequest(&RegisterRequestParams{
		MinProtocolVersion: opts.MinProtocolVersion,
		MaxProtocolVersion: opts.MaxProtocolVersion,
		PluginID:           manifest.ID,
		Config:             handle.Config(),
	}); err != nil {
		_ = terminatePluginProcessGroup(cmd)
		handle.Close()
		return nil, fmt.Errorf("plugin runtime: send register_request: %w", err)
	}

	if err := waitForRegister(ctx, handle, opts, events); err != nil {
		_ = terminatePluginProcessGroup(cmd)
		handle.Close()
		return nil, err
	}

	go dispatchPostRegister(handle, cmd, events, opts.OnMessage)
	return handle, nil
}

type runtimeEvent struct {
	msg *Message
	err error
}

func buildCommand(ctx context.Context, manifest *Manifest, opts SpawnOptions) (*exec.Cmd, error) {
	bin := runtimeCommand(manifest.Runtime, opts.RuntimeCommands)
	if bin == "" {
		return nil, fmt.Errorf("plugin runtime: no command configured for runtime %q", manifest.Runtime)
	}
	entry := manifest.Entry
	if manifest.RootDir != "" && !filepath.IsAbs(entry) {
		entry = filepath.Join(manifest.RootDir, entry)
	}
	cmd := exec.CommandContext(ctx, bin, entry)
	cmd.Dir = manifest.RootDir
	cmd.Env = append(os.Environ(),
		"TABULA_PLUGIN_ID="+manifest.ID,
		"TABULA_PLUGIN_PROTOCOL_MIN="+strconv.Itoa(opts.MinProtocolVersion),
		"TABULA_PLUGIN_PROTOCOL_MAX="+strconv.Itoa(opts.MaxProtocolVersion),
	)
	if manifest.RootDir != "" {
		libSrc := filepath.Join(filepath.Dir(filepath.Dir(manifest.RootDir)), "_lib", "python", "src")
		if stat, err := os.Stat(libSrc); err == nil && stat.IsDir() {
			cmd.Env = append(cmd.Env, prependPathEnv("PYTHONPATH", libSrc))
		}
	}
	configurePluginProcessGroup(cmd)
	return cmd, nil
}

func prependPathEnv(name, value string) string {
	if current := os.Getenv(name); current != "" {
		return name + "=" + value + string(os.PathListSeparator) + current
	}
	return name + "=" + value
}

func runtimeCommand(runtime string, overrides map[string]string) string {
	if overrides != nil && overrides[runtime] != "" {
		return overrides[runtime]
	}
	switch runtime {
	case "python":
		return "python3"
	default:
		return ""
	}
}

func readLoop(handle *Handle, reader *Reader, logger *slog.Logger, events chan<- runtimeEvent) {
	defer close(events)
	malformed := 0
	lastMalformed := time.Time{}
	for {
		msg, err := reader.ReadMessage()
		if err != nil {
			var malformedErr *MalformedError
			if errors.As(err, &malformedErr) {
				now := time.Now()
				if lastMalformed.IsZero() || now.Sub(lastMalformed) > malformedWindow {
					malformed = 0
				}
				lastMalformed = now
				malformed++
				logger.Warn("malformed plugin message", "plugin", handle.ID(), "err", err)
				if malformed >= malformedThreshold {
					events <- runtimeEvent{err: fmt.Errorf("plugin runtime: malformed message threshold reached: %w", err)}
					return
				}
				continue
			}
			if errors.Is(err, io.EOF) {
				events <- runtimeEvent{err: io.EOF}
				return
			}
			events <- runtimeEvent{err: err}
			return
		}
		malformed = 0
		events <- runtimeEvent{msg: msg}
	}
}

func waitForRegister(ctx context.Context, handle *Handle, opts SpawnOptions, events <-chan runtimeEvent) error {
	timeout := opts.RegisterTimeout
	if timeout <= 0 {
		timeout = defaultRegisterTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("plugin runtime: register cancelled for %s: %w", handle.ID(), ctx.Err())
		case <-timer.C:
			return fmt.Errorf("plugin runtime: register timeout for %s after %s", handle.ID(), timeout)
		case ev, ok := <-events:
			if !ok {
				return fmt.Errorf("plugin runtime: stdout closed before register for %s", handle.ID())
			}
			if ev.err != nil {
				return fmt.Errorf("plugin runtime: failed before register for %s: %w", handle.ID(), ev.err)
			}
			if ev.msg == nil {
				continue
			}
			if ev.msg.Method != MethodRegister {
				if opts.OnMessage != nil {
					opts.OnMessage(handle, ev.msg)
				}
				continue
			}
			var reg RegisterParams
			if err := ev.msg.DecodeParams(&reg); err != nil {
				return NonRestartable(fmt.Errorf("plugin runtime: invalid register params for %s: %w", handle.ID(), err))
			}
			if reg.ProtocolVersion < opts.MinProtocolVersion || reg.ProtocolVersion > opts.MaxProtocolVersion {
				return NonRestartable(fmt.Errorf("plugin runtime: protocol negotiation failed for %s: kernel range [%d,%d], plugin chose %d", handle.ID(), opts.MinProtocolVersion, opts.MaxProtocolVersion, reg.ProtocolVersion))
			}
			normalized, err := NormalizeRegisterParams(&reg)
			if err != nil {
				return NonRestartable(fmt.Errorf("plugin runtime: invalid register catalog for %s: %w", handle.ID(), err))
			}
			reg = *normalized
			if opts.ValidateRegister != nil {
				if err := opts.ValidateRegister(&reg); err != nil {
					return NonRestartable(fmt.Errorf("plugin runtime: register rejected for %s: %w", handle.ID(), err))
				}
			}
			if err := handle.MarkRegistered(&reg); err != nil {
				return NonRestartable(fmt.Errorf("plugin runtime: register rejected for %s: %w", handle.ID(), err))
			}
			return nil
		}
	}
}

func dispatchPostRegister(handle *Handle, cmd *exec.Cmd, events <-chan runtimeEvent, onMessage func(*Handle, *Message)) {
	for ev := range events {
		if ev.err != nil {
			_ = terminatePluginProcessGroup(cmd)
			handle.Close()
			return
		}
		if ev.msg != nil && onMessage != nil {
			onMessage(handle, ev.msg)
		}
	}
}

func waitForExit(handle *Handle, cmd *exec.Cmd, onExit func(*Handle, error)) {
	err := cmd.Wait()
	handle.Close()
	if onExit != nil {
		onExit(handle, err)
	}
}

func forwardStderr(logger *slog.Logger, id string, r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		logger.Info("plugin stderr", "plugin", id, "line", sc.Text())
	}
	if err := sc.Err(); err != nil {
		logger.Warn("plugin stderr read failed", "plugin", id, "err", err)
	}
}

func mergeConfig(defaults map[string]any, overrides map[string]any) map[string]any {
	out := make(map[string]any, len(defaults)+len(overrides))
	for k, v := range defaults {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}

func manifestPath(m *Manifest) string {
	if m == nil {
		return ""
	}
	if m.RootDir == "" {
		return ""
	}
	return filepath.Join(m.RootDir, "plugin.toml")
}

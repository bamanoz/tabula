package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/bamanoz/tabula/internal/kernel/plugin"
)

type pluginLifecycle struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type pluginLifecycleState struct {
	ID           string
	Status       string
	RestartCount int
	LastError    string
}

// RegisterPlugin starts a long-lived plugin process, waits for its register
// handshake, and installs its registered tools/subscriptions into the Hub.
//
// The method is intentionally additive: NewHub's signature stays unchanged,
// and callers opt in by invoking RegisterPlugin/LoadPlugins after NewHub and
// before StartReaper (creative-plugin-runtime.md §3). Re-registering the same
// plugin id replaces the prior handle via registerPluginHandle.
func (h *Hub) RegisterPlugin(manifest *plugin.Manifest, config map[string]any) error {
	if h == nil {
		return errors.New("kernel: nil Hub")
	}
	if manifest == nil {
		return errors.New("kernel: plugin manifest is nil")
	}
	if h.pluginRuntime == nil {
		h.pluginRuntime = plugin.NewRuntime()
	}
	run, err := h.startPluginLifecycle(manifest, config)
	if err != nil {
		return err
	}
	h.installPluginLifecycle(manifest.ID, run)
	return nil
}

// LoadPlugins loads plugin.toml manifests from boot entries and attempts to
// start each plugin. It continues after individual plugin failures so kernel
// boot can proceed with degraded plugin coverage; the returned joined error is
// intended for caller logging/diagnostics, not as an implicit boot abort.
func (h *Hub) LoadPlugins(entries []plugin.BootEntry) error {
	var errs []error
	for _, entry := range entries {
		manifest, err := plugin.LoadManifest(entry.ManifestPath)
		if err != nil {
			err = fmt.Errorf("load plugin manifest %q: %w", entry.ManifestPath, err)
			h.Logger.Warn("plugin load failed", "manifest", entry.ManifestPath, "err", err)
			errs = append(errs, err)
			continue
		}
		if err := h.RegisterPlugin(manifest, entry.Config); err != nil {
			h.Logger.Warn("plugin registration failed", "plugin", manifest.ID, "manifest", entry.ManifestPath, "err", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (h *Hub) handlePluginExit(handle *plugin.Handle, err error) {
	if h == nil || handle == nil {
		return
	}
	h.markPluginProcessExited(handle)
	if h.plugins != nil && h.plugins.Get(handle.ID()) == handle {
		h.removePluginHandle(handle)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		msg := strings.TrimSpace(err.Error())
		h.Logger.Warn("plugin exited", "plugin", handle.ID(), "err", msg)
		return
	}
	h.Logger.Info("plugin exited", "plugin", handle.ID())
}

func (h *Hub) startPluginLifecycle(manifest *plugin.Manifest, config map[string]any) (*pluginLifecycle, error) {
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan error, 1)
	done := make(chan struct{})
	run := &pluginLifecycle{cancel: cancel, done: done}
	var readyOnce sync.Once
	markReady := func(err error) {
		readyOnce.Do(func() { ready <- err })
	}

	supervisor := plugin.NewSupervisor(h.pluginRuntime, h.pluginSupervisorPolicy)
	go func() {
		defer close(done)
		err := supervisor.Supervise(ctx, manifest, config, plugin.SupervisorOptions{
			SpawnOptions: plugin.SpawnOptions{
				ProtocolVersion:  PluginProtocolVersion,
				Logger:           h.Logger,
				OnMessage:        h.handlePluginProtocolMessage,
				OnStartProcess:   h.handlePluginProcessStart,
				ValidateRegister: h.validatePluginRegisterParams,
			},
			OnStart: func(handle *plugin.Handle) {
				if err := h.validatePluginHandleCatalog(handle); err != nil {
					if handle != nil {
						handle.Close()
					}
					markReady(plugin.NonRestartable(fmt.Errorf("plugin %s register rejected: %w", manifest.ID, err)))
					return
				}
				h.recordPluginStart(handle)
				h.registerPluginHandle(handle)
				h.Logger.Info("plugin registered", "plugin", handle.ID())
				markReady(nil)
			},
			OnRestart: func(handle *plugin.Handle, err error, restartCount int, nextBackoff time.Duration) {
				h.recordPluginRestart(manifest.ID, handle, err, restartCount)
				h.removePluginHandle(handle)
				h.Logger.Warn("plugin restarting", "plugin", manifest.ID, "restart_count", restartCount, "backoff", nextBackoff, "err", err)
			},
			OnFinalExit: func(handle *plugin.Handle, err error) {
				markReady(err)
				h.handlePluginFinalExit(handle, err, run)
			},
		})
		markReady(err)
	}()

	if err := <-ready; err != nil {
		cancel()
		<-done
		return nil, fmt.Errorf("register plugin %s: %w", manifest.ID, err)
	}
	return run, nil
}

func (h *Hub) recordPluginStart(handle *plugin.Handle) {
	if h == nil || handle == nil {
		return
	}
	h.pluginStatesMu.Lock()
	defer h.pluginStatesMu.Unlock()
	if h.pluginStates == nil {
		h.pluginStates = make(map[string]*pluginLifecycleState)
	}
	state := h.pluginStates[handle.ID()]
	if state == nil {
		state = &pluginLifecycleState{ID: handle.ID()}
		h.pluginStates[handle.ID()] = state
	}
	state.Status = "running"
	state.LastError = ""
}

func (h *Hub) recordPluginRestart(id string, handle *plugin.Handle, err error, restartCount int) {
	if h == nil {
		return
	}
	if id == "" && handle != nil {
		id = handle.ID()
	}
	if id == "" {
		return
	}
	h.pluginStatesMu.Lock()
	defer h.pluginStatesMu.Unlock()
	if h.pluginStates == nil {
		h.pluginStates = make(map[string]*pluginLifecycleState)
	}
	state := h.pluginStates[id]
	if state == nil {
		state = &pluginLifecycleState{ID: id}
		h.pluginStates[id] = state
	}
	state.Status = "restarting"
	state.RestartCount = restartCount
	state.LastError = pluginErrorString(err)
}

func (h *Hub) recordPluginFinalExit(id string, err error) {
	if h == nil || id == "" {
		return
	}
	h.pluginStatesMu.Lock()
	defer h.pluginStatesMu.Unlock()
	if h.pluginStates == nil {
		h.pluginStates = make(map[string]*pluginLifecycleState)
	}
	state := h.pluginStates[id]
	if state == nil {
		state = &pluginLifecycleState{ID: id}
		h.pluginStates[id] = state
	}
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
		state.Status = "stopped"
		state.LastError = ""
		return
	}
	state.Status = "failed"
	state.LastError = pluginErrorString(err)
}

func (h *Hub) removePluginHandle(handle *plugin.Handle) {
	if h == nil || handle == nil || h.plugins == nil {
		return
	}
	h.removePluginTools(handle)
	h.plugins.Remove(handle.ID())
	h.rebuildHookIndex()
}

func pluginErrorString(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func (h *Hub) handlePluginProcessStart(handle *plugin.Handle, cmd *exec.Cmd) {
	if h == nil || handle == nil || cmd == nil || cmd.Process == nil || h.processes == nil {
		return
	}
	pid := cmd.Process.Pid
	h.processes.Register(cmd, "plugin:"+handle.ID(), "")
	h.Logger.Info("registered plugin process", "plugin", handle.ID(), "pid", pid)
}

func (h *Hub) markPluginProcessExited(handle *plugin.Handle) {
	if h == nil || handle == nil || h.processes == nil {
		return
	}
	pid := handle.PID()
	if pid == 0 {
		return
	}
	if _, ok := h.processes.MarkExited(pid); ok {
		h.Logger.Info("plugin process exited", "plugin", handle.ID(), "pid", pid)
	}
}

func (h *Hub) installPluginLifecycle(id string, run *pluginLifecycle) {
	if run == nil {
		return
	}
	select {
	case <-run.done:
		return
	default:
	}
	h.pluginRunsMu.Lock()
	if h.pluginRuns == nil {
		h.pluginRuns = make(map[string]*pluginLifecycle)
	}
	prior := h.pluginRuns[id]
	h.pluginRuns[id] = run
	h.pluginRunsMu.Unlock()

	if prior != nil {
		prior.cancel()
	}
}

func (h *Hub) handlePluginFinalExit(handle *plugin.Handle, err error, run *pluginLifecycle) {
	if h == nil || handle == nil {
		return
	}
	h.handlePluginExit(handle, err)
	h.recordPluginFinalExit(handle.ID(), err)
	h.pluginRunsMu.Lock()
	if h.pluginRuns[handle.ID()] == run {
		delete(h.pluginRuns, handle.ID())
	}
	h.pluginRunsMu.Unlock()
}

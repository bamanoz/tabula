package tabula

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/bamanoz/tabula/internal/kernel"
	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
)

const localRuntimeAttachTimeout = 10 * time.Second

// ensureRuntimeConfigExists verifies that the installer has produced
// $TABULA_HOME/config/runtime.toml. Plugin layout (plugin_dirs, skill_dirs,
// kernel block) is owned by the installer (`tabula-install` /
// `tabula-distro`); the kernel reads it as-is. If the file is missing we
// fail fast with an explicit message instructing the user to install a
// distro first — see docs/issues/refactoring/004-runtime-toml-as-plugin-source-of-truth.md.
func ensureRuntimeConfigExists(tabulaHome string) error {
	tabulaHome = strings.TrimSpace(tabulaHome)
	if tabulaHome == "" {
		return fmt.Errorf("TABULA_HOME is required")
	}
	path := filepath.Join(tabulaHome, "config", "runtime.toml")
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s is missing; run `tabula-install <distro>` first", path)
		}
		return fmt.Errorf("stat runtime config %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory, expected a file", path)
	}
	return nil
}

func localRuntimeSocketPath(tabulaHome string) string {
	if override := strings.TrimSpace(os.Getenv("TABULA_RUNTIME_SOCKET_PATH")); override != "" {
		return override
	}
	path := filepath.Join(tabulaHome, "run", "runtime.sock")
	if len(path) <= 100 {
		return path
	}
	sum := sha256.Sum256([]byte(tabulaHome))
	return filepath.Join(os.TempDir(), "tabula-rt-"+hex.EncodeToString(sum[:8]), "runtime.sock")
}

func resolveLocalRuntimeBinary(tabulaPath string, lookPath func(string) (string, error)) (string, error) {
	name := localRuntimeBinaryName()
	if tabulaPath = strings.TrimSpace(tabulaPath); tabulaPath != "" {
		sibling := filepath.Join(filepath.Dir(tabulaPath), name)
		if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
			return sibling, nil
		}
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	path, err := lookPath(name)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", name, err)
	}
	return path, nil
}

type managedLocalRuntime struct {
	cmd      *exec.Cmd
	done     chan struct{}
	waitErr  error
	waitOnce sync.Once
}

func newManagedLocalRuntime(cmd *exec.Cmd) *managedLocalRuntime {
	m := &managedLocalRuntime{cmd: cmd, done: make(chan struct{})}
	m.startWait()
	return m
}

func newLocalRuntimeCommand(tabulaPath, tabulaHome string, stderr io.Writer, lookPath func(string) (string, error)) (*exec.Cmd, error) {
	tabulaHome = strings.TrimSpace(tabulaHome)
	if tabulaHome == "" {
		return nil, fmt.Errorf("TABULA_HOME is required")
	}
	binary, err := resolveLocalRuntimeBinary(tabulaPath, lookPath)
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(tabulaHome, "config", "runtime.toml")
	cmd := exec.Command(binary, "start", "--config", configPath, "--runtime-id", runtimeauth.LocalRuntimeID)
	cmd.Env = append(os.Environ(), "TABULA_HOME="+tabulaHome)
	if stderr != nil {
		cmd.Stdout = stderr
		cmd.Stderr = stderr
	}
	return cmd, nil
}

func startManagedLocalRuntime(tabulaPath, tabulaHome string, stderr io.Writer, lookPath func(string) (string, error)) (*managedLocalRuntime, error) {
	cmd, err := newLocalRuntimeCommand(tabulaPath, tabulaHome, stderr, lookPath)
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start local runtime: %w", err)
	}
	return newManagedLocalRuntime(cmd), nil
}

func startAttachedLocalRuntime(hub *kernel.Hub, tabulaHome string, stderr io.Writer, lookPath func(string) (string, error), onStarted func(pid int)) (*managedLocalRuntime, error) {
	if hub == nil {
		return nil, fmt.Errorf("kernel hub is required")
	}
	tabulaPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve tabula binary: %w", err)
	}
	runtimeProc, err := startManagedLocalRuntime(tabulaPath, tabulaHome, stderr, lookPath)
	if err != nil {
		return nil, err
	}
	if onStarted != nil {
		onStarted(runtimeProc.PID())
	}
	if err := waitForLocalRuntimeAttachment(hub, runtimeProc, localRuntimeAttachTimeout); err != nil {
		_ = runtimeProc.Shutdown(5 * time.Second)
		return nil, err
	}
	hub.SetAttachedRuntimePID(runtimeauth.LocalRuntimeID, runtimeProc.PID())
	return runtimeProc, nil
}

func waitForLocalRuntimeAttachment(hub *kernel.Hub, runtimeProc *managedLocalRuntime, timeout time.Duration) error {
	if hub == nil {
		return fmt.Errorf("kernel hub is required")
	}
	if runtimeProc == nil {
		return fmt.Errorf("local runtime process is not running")
	}
	return runtimeProc.WaitForAttachment(func() bool {
		return hub.RuntimeAttached(runtimeauth.LocalRuntimeID)
	}, timeout)
}

func (m *managedLocalRuntime) startWait() {
	if m == nil {
		return
	}
	m.waitOnce.Do(func() {
		if m.done == nil {
			m.done = make(chan struct{})
		}
		go func() {
			if m.cmd != nil {
				m.waitErr = m.cmd.Wait()
			}
			close(m.done)
		}()
	})
}

func (m *managedLocalRuntime) PID() int {
	if m == nil || m.cmd == nil || m.cmd.Process == nil {
		return 0
	}
	return m.cmd.Process.Pid
}

func (m *managedLocalRuntime) Done() <-chan struct{} {
	if m == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	m.startWait()
	return m.done
}

func (m *managedLocalRuntime) Wait() error {
	if m == nil {
		return nil
	}
	m.startWait()
	<-m.done
	return m.waitErr
}

func (m *managedLocalRuntime) WaitForAttachment(attached func() bool, timeout time.Duration) error {
	if m == nil || m.cmd == nil || m.cmd.Process == nil {
		return fmt.Errorf("local runtime process is not running")
	}
	if attached == nil {
		return fmt.Errorf("local runtime attachment check is required")
	}
	if attached() {
		return nil
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-m.Done():
			if attached() {
				return nil
			}
			if err := m.Wait(); err != nil {
				return fmt.Errorf("local runtime exited before attach: %w", err)
			}
			return fmt.Errorf("local runtime exited before attach")
		case <-ticker.C:
			if attached() {
				return nil
			}
		case <-timer.C:
			if attached() {
				return nil
			}
			return fmt.Errorf("local runtime did not attach within %s", timeout)
		}
	}
}

func (m *managedLocalRuntime) Shutdown(timeout time.Duration) error {
	if m == nil || m.cmd == nil || m.cmd.Process == nil {
		return nil
	}
	m.startWait()
	select {
	case <-m.done:
		return m.waitErr
	default:
	}
	_ = m.cmd.Process.Signal(os.Interrupt)
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-m.done:
		return m.waitErr
	case <-timer.C:
		_ = m.cmd.Process.Kill()
		<-m.done
		return m.waitErr
	}
}

func localRuntimeBinaryName() string {
	if runtime.GOOS == "windows" {
		return "tabula-runtime.exe"
	}
	return "tabula-runtime"
}

package kernel

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/kernel/plugin"
)

func TestTabulaDistribGatewayPluginWrappersLiveE2E(t *testing.T) {
	root := filepath.Clean(filepath.Join(repoRoot(t), "..", "tabula-distrib"))
	if _, err := os.Stat(root); err != nil {
		t.Skipf("tabula-distrib checkout not available: %v", err)
	}
	bundlesRoot := filepath.Clean(filepath.Join(repoRoot(t), "..", "tabula-bundles"))
	if _, err := os.Stat(bundlesRoot); err != nil {
		t.Skipf("tabula-bundles checkout not available: %v", err)
	}

	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	t.Setenv("PYTHONPATH", filepath.Join(bundlesRoot, "_lib", "python", "src")+string(os.PathListSeparator)+repoRoot(t))

	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	hub.pluginSupervisorPolicy = plugin.SupervisorPolicy{
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		MaxRestarts:    1,
		RestartWindow:  time.Second,
		CleanRunReset:  time.Hour,
	}

	// Stage a copy of the plugin directory and replace daemon.py with a
	// no-op stub so the test does not actually start the Telegram polling.
	src := filepath.Join(root, "claw", "plugins", "gateway-telegram-plugin")
	stagedRoot := t.TempDir()
	staged := filepath.Join(stagedRoot, "gateway-telegram-plugin")
	if err := copyDir(src, staged); err != nil {
		t.Fatalf("stage plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staged, "daemon.py"), []byte(fakeGatewayRunnerPython), 0o755); err != nil {
		t.Fatalf("write fake daemon: %v", err)
	}

	recv := addToolResultCaptureClient(t, hub, "gateway-wrapper")

	manifest, err := plugin.LoadManifest(staged)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if err := hub.RegisterPlugin(manifest, nil); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	defer stopPluginRun(t, hub, "gateway-telegram")

	hub.tools.handleDynamicTool("gateway-wrapper", "gateway-telegram-status", "gateway_telegram_status", json.RawMessage(`{}`))
	msg := waitForMessage(t, recv.recvCh)
	if msg.Type != string(MsgToolResult) || !strings.Contains(msg.Output, `"running":true`) || !strings.Contains(msg.Output, `"pid"`) {
		t.Fatalf("gateway-telegram status result: %+v", msg)
	}

	gracefulStopPluginRun(t, hub, "gateway-telegram")
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}

const fakeGatewayRunnerPython = `#!/usr/bin/env python3
import signal
import sys
import time

running = True
def stop(_sig, _frame):
    global running
    running = False

signal.signal(signal.SIGTERM, stop)
signal.signal(signal.SIGINT, stop)
while running:
    time.sleep(0.1)
sys.exit(0)
`

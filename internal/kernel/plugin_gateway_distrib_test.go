package kernel

import (
	"encoding/json"
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

	for _, name := range []string{"gateway-api", "gateway-telegram"} {
		runner := filepath.Join(home, "skills", name, "run.py")
		if err := os.MkdirAll(filepath.Dir(runner), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", runner, err)
		}
		if err := os.WriteFile(runner, []byte(fakeGatewayRunnerPython), 0o755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}

	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	hub.pluginSupervisorPolicy = plugin.SupervisorPolicy{
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
		MaxRestarts:    1,
		RestartWindow:  time.Second,
		CleanRunReset:  time.Hour,
	}

	plugins := []struct {
		id     string
		path   string
		status string
	}{
		{"gateway-api", filepath.Join(root, "claw", "plugins", "gateway-api-plugin"), "gateway_api_status"},
		{"gateway-telegram", filepath.Join(root, "claw", "plugins", "gateway-telegram-plugin"), "gateway_telegram_status"},
	}
	recv := addToolResultCaptureClient(t, hub, "gateway-wrapper")
	for _, item := range plugins {
		manifest, err := plugin.LoadManifest(item.path)
		if err != nil {
			t.Fatalf("LoadManifest(%s): %v", item.id, err)
		}
		if err := hub.RegisterPlugin(manifest, nil); err != nil {
			t.Fatalf("RegisterPlugin(%s): %v", item.id, err)
		}
		defer stopPluginRun(t, hub, item.id)

		hub.tools.handleDynamicTool("gateway-wrapper", item.id+"-status", item.status, json.RawMessage(`{}`))
		msg := waitForMessage(t, recv.recvCh)
		if msg.Type != string(MsgToolResult) || !strings.Contains(msg.Output, `"running":true`) || !strings.Contains(msg.Output, `"pid"`) {
			t.Fatalf("%s status result: %+v", item.id, msg)
		}

		gracefulStopPluginRun(t, hub, item.id)
	}
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

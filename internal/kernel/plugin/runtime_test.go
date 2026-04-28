package plugin

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDefaultRuntimeSpawnRegistersAndDispatchesMessages(t *testing.T) {
	root := t.TempDir()
	writePluginScript(t, root, `
IFS= read -r line
printf '%s\n' '{"method":"register","params":{"protocol_version":42,"plugin_id":"hello","tools":[{"name":"hello_ping","deadline_ms":5}],"subscriptions":[{"event":"before_tool_call","priority":7}]}}'
printf '%s\n' '{"method":"log","params":{"level":"info","msg":"started"}}'
sleep 1
`)

	seen := make(chan *Message, 1)
	h, err := NewRuntime().Spawn(context.Background(), testManifest(root, "hello"), map[string]any{"override": true}, SpawnOptions{
		ProtocolVersion: 42,
		RegisterTimeout: time.Second,
		RuntimeCommands: map[string]string{"python": "/bin/sh"},
		OnMessage: func(_ *Handle, msg *Message) {
			seen <- msg
		},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(h.Close)

	if !h.IsAlive() || !h.IsRegistered() {
		t.Fatalf("handle state: alive=%v registered=%v", h.IsAlive(), h.IsRegistered())
	}
	if got := h.Config()["override"]; got != true {
		t.Fatalf("config override not delivered: %#v", h.Config())
	}
	tools := h.Tools()
	if len(tools) != 1 || tools[0].Name != "hello_ping" || tools[0].DeadlineMs != 5 {
		t.Fatalf("tools: %+v", tools)
	}
	subs := h.Subscriptions()
	if len(subs) != 1 || subs[0].Event != "before_tool_call" || subs[0].Priority != 7 {
		t.Fatalf("subscriptions: %+v", subs)
	}

	select {
	case msg := <-seen:
		if msg.Method != MethodLog {
			t.Fatalf("post-register method: %q", msg.Method)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for post-register log")
	}
}

func TestDefaultRuntimeRejectsProtocolMismatch(t *testing.T) {
	root := t.TempDir()
	writePluginScript(t, root, `
IFS= read -r line
printf '%s\n' '{"method":"register","params":{"protocol_version":99,"plugin_id":"bad","tools":[],"subscriptions":[]}}'
sleep 1
`)

	_, err := NewRuntime().Spawn(context.Background(), testManifest(root, "bad"), nil, SpawnOptions{
		ProtocolVersion: 42,
		RegisterTimeout: time.Second,
		RuntimeCommands: map[string]string{"python": "/bin/sh"},
	})
	if err == nil || !strings.Contains(err.Error(), "protocol mismatch") {
		t.Fatalf("expected protocol mismatch, got %v", err)
	}
}

func TestDefaultRuntimeRejectsInvalidRegisterCatalogNonRestartable(t *testing.T) {
	root := t.TempDir()
	writePluginScript(t, root, `
IFS= read -r line
printf '%s\n' '{"method":"register","params":{"protocol_version":42,"plugin_id":"bad","tools":[{"name":""}],"subscriptions":[]}}'
sleep 1
`)

	_, err := NewRuntime().Spawn(context.Background(), testManifest(root, "bad"), nil, SpawnOptions{
		ProtocolVersion: 42,
		RegisterTimeout: time.Second,
		RuntimeCommands: map[string]string{"python": "/bin/sh"},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid register catalog") {
		t.Fatalf("expected invalid register catalog, got %v", err)
	}
	var nonRestartable *NonRestartableError
	if !errors.As(err, &nonRestartable) {
		t.Fatalf("expected non-restartable invalid catalog error, got %T %v", err, err)
	}
}

func TestDefaultRuntimeRegisterValidatorRejectsSubscriptionNonRestartable(t *testing.T) {
	root := t.TempDir()
	writePluginScript(t, root, `
IFS= read -r line
printf '%s\n' '{"method":"register","params":{"protocol_version":42,"plugin_id":"bad","tools":[],"subscriptions":[{"event":"before_spawn"}]}}'
sleep 1
`)

	_, err := NewRuntime().Spawn(context.Background(), testManifest(root, "bad"), nil, SpawnOptions{
		ProtocolVersion: 42,
		RegisterTimeout: time.Second,
		RuntimeCommands: map[string]string{"python": "/bin/sh"},
		ValidateRegister: func(reg *RegisterParams) error {
			return errors.New("unsupported event before_spawn")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "register rejected") {
		t.Fatalf("expected validator rejection, got %v", err)
	}
	var nonRestartable *NonRestartableError
	if !errors.As(err, &nonRestartable) {
		t.Fatalf("expected non-restartable validator error, got %T %v", err, err)
	}
}

func TestDefaultRuntimeMalformedThresholdBeforeRegister(t *testing.T) {
	root := t.TempDir()
	writePluginScript(t, root, `
printf '%s\n' '{bad json'
printf '%s\n' '{bad json'
printf '%s\n' '{bad json'
sleep 1
`)

	_, err := NewRuntime().Spawn(context.Background(), testManifest(root, "badjson"), nil, SpawnOptions{
		ProtocolVersion: 42,
		RegisterTimeout: time.Second,
		RuntimeCommands: map[string]string{"python": "/bin/sh"},
	})
	if err == nil || !strings.Contains(err.Error(), "malformed message threshold") {
		t.Fatalf("expected malformed threshold error, got %v", err)
	}
}

func testManifest(root, id string) *Manifest {
	return &Manifest{
		ID:      id,
		Name:    id,
		Version: "1.0.0",
		Runtime: "python",
		Entry:   "plugin.sh",
		RootDir: root,
		Config:  map[string]any{"default": "yes"},
	}
}

func writePluginScript(t *testing.T, root, body string) {
	t.Helper()
	path := root + "/plugin.sh"
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
}

package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy/bare"
	"github.com/bamanoz/tabula/internal/runtime/host/pool"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestHandlerRejectsSkillTargetInvoke(t *testing.T) {
	dir := t.TempDir()
	writeRuntimeSkill(t, dir)
	store, err := manifest.NewStore([]string{dir})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	p := pool.New("main", store, bare.New())
	t.Cleanup(p.Close)
	h := NewHandler(Options{Store: store, Pool: p})

	caps, err := h.ListCapabilities(context.Background(), wire.ListCapabilities{Op: wire.OpListCapabilities})
	if err != nil {
		t.Fatalf("ListCapabilities: %v", err)
	}
	if len(caps.Targets) != 0 {
		t.Fatalf("skills must not publish executable capabilities: %#v", caps.Targets)
	}
	resp, err := h.Invoke(context.Background(), wire.Invoke{Op: wire.OpInvoke, CallID: "call-skill", TenantID: "tenant-a", Target: wire.Target{Kind: wire.TargetKindSkill, ID: "skill:testbed-echo"}, Tool: "testbed_echo", Args: json.RawMessage(`{"text":"hello"}`)})
	if err != nil {
		t.Fatalf("Invoke = %#v, %v", resp, err)
	}
	if resp.Error == nil || resp.Error.Code != wire.ErrorTargetForbidden {
		t.Fatalf("expected target_forbidden, got %#v", resp)
	}
}

func writeRuntimeSkill(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "skills", "testbed-echo")
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: testbed-echo
description: Deterministic test fixture skill that echoes JSON input.
tools:
  - name: testbed_echo
    description: Echo input.
    params:
      text: { type: string }
    required: []
    exec: "python3 ${SKILL_DIR}/scripts/run.py tool testbed_echo"
---
`), 0o644); err != nil {
		t.Fatalf("write skill manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte(`#!/usr/bin/env python3
import json, sys
params = json.loads(sys.stdin.read() or "{}")
print(json.dumps({"ok": True, "text": params.get("text", "")}, sort_keys=True))
`), 0o755); err != nil {
		t.Fatalf("write skill script: %v", err)
	}
}

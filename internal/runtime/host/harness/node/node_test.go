package node

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy"
	runtimewire "github.com/bamanoz/tabula/internal/runtime/wire"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

func TestNodeHarnessRunsSyntheticSkill(t *testing.T) {
	home := t.TempDir()
	worker := newNodeWorker(t, home, "node ${SKILL_DIR}/testdata/echo.js")
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{Op: workerwire.OpInit}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	result, err := worker.Call(context.Background(), workerwire.WorkerCall{Op: workerwire.OpCall, CallID: "node-1", Tool: "node_echo", Args: json.RawMessage(`{"text":"hello"}`)})
	if err != nil || !result.OK {
		t.Fatalf("Call = %#v, %v", result, err)
	}
	var got map[string]any
	if err := json.Unmarshal(result.Data, &got); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if got["platform"] != "node" {
		t.Fatalf("unexpected result: %#v", got)
	}
}

func TestNodeHarnessSetsNodeEnvAndReadsStdoutEnvelope(t *testing.T) {
	home := t.TempDir()
	worker := newNodeWorker(t, home, "node ${SKILL_DIR}/testdata/fail.js")
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{Op: workerwire.OpInit}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	result, err := worker.Call(context.Background(), workerwire.WorkerCall{Op: workerwire.OpCall, CallID: "node-2", Tool: "node_echo", Args: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.OK || result.Error == nil || result.Error.Code != "bad_input" || result.Error.Message != "node failure" {
		t.Fatalf("unexpected failure result: %#v", result)
	}
	if envWorker := newNodeWorker(t, home, `node -e "process.stdout.write(JSON.stringify({nodePath: process.env.NODE_PATH || '', nodeNoWarnings: process.env.NODE_NO_WARNINGS || ''}))"`); envWorker != nil {
		if _, err := envWorker.Init(context.Background(), workerwire.WorkerInit{Op: workerwire.OpInit}); err != nil {
			t.Fatalf("Init env worker: %v", err)
		}
		res, err := envWorker.Call(context.Background(), workerwire.WorkerCall{Op: workerwire.OpCall, CallID: "node-3", Tool: "node_echo", Args: json.RawMessage(`{}`)})
		if err != nil || !res.OK {
			t.Fatalf("Call env worker = %#v, %v", res, err)
		}
		var env map[string]string
		if err := json.Unmarshal(res.Data, &env); err != nil {
			t.Fatalf("decode env data: %v", err)
		}
		if env["nodeNoWarnings"] != "1" || env["nodePath"] != filepath.Join(home, "_lib", "node", "node_modules") {
			t.Fatalf("unexpected node env: %#v", env)
		}
		if envWorker.IsAlive() {
			t.Fatal("cold node worker should exit after one call")
		}
	}
}

func newNodeWorker(t *testing.T, home, execText string) policy.Worker {
	t.Helper()
	worker, err := New(policy.SpawnReq{
		KernelID:    "main",
		TenantID:    "tenant-a",
		TargetID:    "skill:test-node",
		TargetKind:  runtimewire.TargetKindSkill,
		HarnessKind: runtimewire.HarnessKindNode,
		Runtime:     "node",
		Entry:       "SKILL.md",
		Manifest:    skillManifestJSON(t, execText),
		WorkingDir:  filepath.Dir(filepath.Dir(testdataPath(t, "echo.js"))),
		Mode:        policy.SpawnModeCold,
		Env:         map[string]string{"TABULA_HOME": home},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return worker
}

func skillManifestJSON(t *testing.T, execText string) []byte {
	t.Helper()
	data, err := json.Marshal(manifest.Skill{Name: "test-node", WorkerMode: runtimewire.WorkerModeCold, HarnessKind: runtimewire.HarnessKindNode, Tools: []manifest.SkillTool{{Name: "node_echo", Exec: execText, HarnessKind: runtimewire.HarnessKindNode}}})
	if err != nil {
		t.Fatalf("marshal skill: %v", err)
	}
	return data
}

func testdataPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

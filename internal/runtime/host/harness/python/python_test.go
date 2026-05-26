package python

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy"
	runtimewire "github.com/bamanoz/tabula/internal/runtime/wire"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

func TestPythonHarnessSetsPythonEnv(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	t.Setenv("TABULA_HOME", home)
	writeFile(t, filepath.Join(dir, "scripts", "run.py"), `#!/usr/bin/env python3
import json, os
print(json.dumps({"pythonunbuffered": os.environ.get("PYTHONUNBUFFERED", ""), "pythonpath": os.environ.get("PYTHONPATH", "")}, sort_keys=True))
`)
	worker := newPythonWorker(t, dir, home, "python3 ${SKILL_DIR}/scripts/run.py tool env")
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{Op: workerwire.OpInit}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	result, err := worker.Call(context.Background(), workerwire.WorkerCall{Op: workerwire.OpCall, CallID: "env", Tool: "env_tool", Args: json.RawMessage(`{}`)})
	if err != nil || !result.OK {
		t.Fatalf("Call = %#v, %v", result, err)
	}
	var got map[string]string
	if err := json.Unmarshal(result.Data, &got); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if got["pythonunbuffered"] != "1" {
		t.Fatalf("PYTHONUNBUFFERED = %q", got["pythonunbuffered"])
	}
	if !strings.Contains(got["pythonpath"], filepath.Join(home, "_lib", "python", "src")) {
		t.Fatalf("PYTHONPATH = %q", got["pythonpath"])
	}
}

func TestPythonHarnessReadsStdoutErrorEnvelopeOnNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	t.Setenv("TABULA_HOME", home)
	writeFile(t, filepath.Join(dir, "scripts", "run.py"), `#!/usr/bin/env python3
import json, sys
print(json.dumps({"code": "bad_input", "message": "missing field"}))
raise SystemExit(1)
`)
	worker := newPythonWorker(t, dir, home, "python3 ${SKILL_DIR}/scripts/run.py tool fail")
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{Op: workerwire.OpInit}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	result, err := worker.Call(context.Background(), workerwire.WorkerCall{Op: workerwire.OpCall, CallID: "fail", Tool: "env_tool", Args: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.OK || result.Error == nil || result.Error.Code != "bad_input" || result.Error.Message != "missing field" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestPythonHarnessPrefersTabulaHomeVenvPython(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	t.Setenv("TABULA_HOME", home)
	realPython, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("LookPath python3: %v", err)
	}
	writeFile(t, filepath.Join(home, ".venv", "bin", "python3"), "#!/bin/sh\nexport SELECTED_PYTHON=\"$0\"\nexec \""+realPython+"\" \"$@\"\n")
	writeFile(t, filepath.Join(dir, "scripts", "run.py"), `#!/usr/bin/env python3
import json, os
print(json.dumps({"selected_python": os.environ.get("SELECTED_PYTHON", "")}, sort_keys=True))
`)
	worker := newPythonWorker(t, dir, home, "python3 ${SKILL_DIR}/scripts/run.py tool env")
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{Op: workerwire.OpInit}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	result, err := worker.Call(context.Background(), workerwire.WorkerCall{Op: workerwire.OpCall, CallID: "venv", Tool: "env_tool", Args: json.RawMessage(`{}`)})
	if err != nil || !result.OK {
		t.Fatalf("Call = %#v, %v", result, err)
	}
	var got map[string]string
	if err := json.Unmarshal(result.Data, &got); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if got["selected_python"] != filepath.Join(home, ".venv", "bin", "python3") {
		t.Fatalf("selected_python = %q", got["selected_python"])
	}
}

func TestPythonHarnessPrefersTabulaVenv(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	venv := filepath.Join(dir, "host-venv")
	t.Setenv("TABULA_HOME", home)
	t.Setenv("TABULA_VENV", venv)
	realPython, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("LookPath python3: %v", err)
	}
	writeFile(t, filepath.Join(home, ".venv", "bin", "python3"), "#!/bin/sh\nexport SELECTED_PYTHON=home\nexec \""+realPython+"\" \"$@\"\n")
	writeFile(t, filepath.Join(venv, "bin", "python3"), "#!/bin/sh\nexport SELECTED_PYTHON=host\nexec \""+realPython+"\" \"$@\"\n")
	writeFile(t, filepath.Join(dir, "scripts", "run.py"), `#!/usr/bin/env python3
import json, os
print(json.dumps({"selected_python": os.environ.get("SELECTED_PYTHON", "")}, sort_keys=True))
`)
	worker := newPythonWorker(t, dir, home, "python3 ${SKILL_DIR}/scripts/run.py tool env")
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{Op: workerwire.OpInit}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	result, err := worker.Call(context.Background(), workerwire.WorkerCall{Op: workerwire.OpCall, CallID: "venv", Tool: "env_tool", Args: json.RawMessage(`{}`)})
	if err != nil || !result.OK {
		t.Fatalf("Call = %#v, %v", result, err)
	}
	var got map[string]string
	if err := json.Unmarshal(result.Data, &got); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if got["selected_python"] != "host" {
		t.Fatalf("selected_python = %q", got["selected_python"])
	}
}

func newPythonWorker(t *testing.T, dir, home, execText string) policy.Worker {
	t.Helper()
	worker, err := New(policy.SpawnReq{
		KernelID:    "main",
		TenantID:    "tenant-a",
		TargetID:    "skill:test-python",
		TargetKind:  runtimewire.TargetKindSkill,
		HarnessKind: runtimewire.HarnessKindPython,
		Runtime:     "python",
		Entry:       "SKILL.md",
		Manifest:    skillManifestJSON(t, manifest.Skill{Name: "test-python", WorkerMode: runtimewire.WorkerModeCold, HarnessKind: runtimewire.HarnessKindPython, Tools: []manifest.SkillTool{{Name: "env_tool", Exec: execText, HarnessKind: runtimewire.HarnessKindPython}}}),
		WorkingDir:  dir,
		Mode:        policy.SpawnModeCold,
		Env:         map[string]string{"TABULA_HOME": home},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return worker
}

func skillManifestJSON(t *testing.T, skill manifest.Skill) []byte {
	t.Helper()
	data, err := json.Marshal(skill)
	if err != nil {
		t.Fatalf("marshal skill: %v", err)
	}
	return data
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

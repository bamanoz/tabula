package bash

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy"
	runtimewire "github.com/bamanoz/tabula/internal/runtime/wire"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

func TestBuildCommandSharedTemplateCorpus(t *testing.T) {
	tabulaBundles := siblingBundlesRoot(t)
	if tabulaBundles == "" {
		t.Skip("tabula-bundles checkout not found")
	}
	data, err := os.ReadFile(filepath.Join(tabulaBundles, "_lib", "_shared", "template_test_corpus.json"))
	if err != nil {
		t.Fatalf("read shared corpus: %v", err)
	}
	var corpus struct {
		SkillExec []struct {
			Name       string   `json:"name"`
			Exec       string   `json:"exec"`
			SkillDir   string   `json:"skill_dir"`
			TabulaHome string   `json:"tabula_home"`
			Expected   []string `json:"expected"`
		} `json:"skill_exec"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("decode shared corpus: %v", err)
	}
	for _, tc := range corpus.SkillExec {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := buildCommand(tc.Exec, tc.SkillDir, tc.TabulaHome)
			if err != nil {
				t.Fatalf("buildCommand: %v", err)
			}
			if strings.Join(got, "\x00") != strings.Join(tc.Expected, "\x00") {
				t.Fatalf("argv = %#v, want %#v", got, tc.Expected)
			}
		})
	}
}

func siblingBundlesRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for dir := cwd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if filepath.Base(dir) == "tabula" {
			candidate := filepath.Join(filepath.Dir(dir), "tabula-bundles")
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return ""
}

func TestWorkerInitAndCallPythonStyleSkill(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "scripts", "run.py"), `#!/usr/bin/env python3
import json, sys
params = json.loads(sys.stdin.read() or "{}")
print(json.dumps({"ok": True, "text": params.get("text", "")}, sort_keys=True))
`)
	worker := newWorker(t, policy.SpawnReq{
		KernelID:    "main",
		TenantID:    "tenant-a",
		TargetID:    "skill:test-echo",
		TargetKind:  runtimewire.TargetKindSkill,
		HarnessKind: runtimewire.HarnessKindPython,
		Runtime:     "python",
		Entry:       "SKILL.md",
		Manifest:    skillManifestJSON(t, manifest.Skill{Name: "test-echo", RootDir: dir, WorkerMode: runtimewire.WorkerModeCold, HarnessKind: runtimewire.HarnessKindPython, Tools: []manifest.SkillTool{{Name: "testbed_echo", Exec: "python3 ${SKILL_DIR}/scripts/run.py tool testbed_echo", HarnessKind: runtimewire.HarnessKindPython}}}),
		WorkingDir:  dir,
		Mode:        policy.SpawnModeCold,
	})
	ack, err := worker.Init(context.Background(), workerwire.WorkerInit{Op: workerwire.OpInit})
	if err != nil || !ack.Ready || len(ack.Tools) != 1 || ack.Tools[0].Name != "testbed_echo" {
		t.Fatalf("Init = %#v, %v", ack, err)
	}
	result, err := worker.Call(context.Background(), workerwire.WorkerCall{Op: workerwire.OpCall, CallID: "call-1", Tool: "testbed_echo", Args: json.RawMessage(`{"text":"pong"}`)})
	if err != nil || !result.OK {
		t.Fatalf("Call = %#v, %v", result, err)
	}
	if string(result.Data) != `{"ok": true, "text": "pong"}` && string(result.Data) != `{"ok":true,"text":"pong"}` {
		t.Fatalf("unexpected data: %s", result.Data)
	}
}

func TestWorkerUsesJSONFromStderrWhenStdoutEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "scripts", "stderr.py"), `#!/usr/bin/env python3
import json, sys
print(json.dumps({"ok": True, "text": "stderr-json"}, sort_keys=True), file=sys.stderr)
`)
	worker := newWorker(t, policy.SpawnReq{
		KernelID:    "main",
		TenantID:    "tenant-a",
		TargetID:    "skill:test-stderr",
		TargetKind:  runtimewire.TargetKindSkill,
		HarnessKind: runtimewire.HarnessKindPython,
		Runtime:     "python",
		Entry:       "SKILL.md",
		Manifest:    skillManifestJSON(t, manifest.Skill{Name: "test-stderr", RootDir: dir, WorkerMode: runtimewire.WorkerModeCold, HarnessKind: runtimewire.HarnessKindPython, Tools: []manifest.SkillTool{{Name: "stderr_tool", Exec: "python3 ${SKILL_DIR}/scripts/stderr.py tool stderr_tool", HarnessKind: runtimewire.HarnessKindPython}}}),
		WorkingDir:  dir,
		Mode:        policy.SpawnModeCold,
	})
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{Op: workerwire.OpInit}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	result, err := worker.Call(context.Background(), workerwire.WorkerCall{Op: workerwire.OpCall, CallID: "call-stderr", Tool: "stderr_tool", Args: json.RawMessage(`{}`)})
	if err != nil || !result.OK {
		t.Fatalf("Call = %#v, %v", result, err)
	}
	if string(result.Data) != `{"ok": true, "text": "stderr-json"}` && string(result.Data) != `{"ok":true,"text":"stderr-json"}` {
		t.Fatalf("unexpected data: %s", result.Data)
	}
}

func TestWorkerExpandsSkillDirAndSetsSkillEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TABULA_HOME", filepath.Join(dir, "home"))
	writeFile(t, filepath.Join(dir, "scripts", "env.sh"), "#!/bin/sh\nprintf '{\"skill_dir\":\"%s\",\"tool\":\"%s\",\"call\":\"%s\",\"session\":\"%s\",\"tenant\":\"%s\",\"tenant_dir\":\"%s\",\"kernel\":\"%s\",\"target\":\"%s\",\"home\":\"%s\"}\\n' \"$TABULA_SKILL_DIR\" \"$TABULA_TOOL_NAME\" \"$TABULA_CALL_ID\" \"$TABULA_SESSION\" \"$TABULA_TENANT_ID\" \"$TABULA_TENANT_DIR\" \"$TABULA_KERNEL_ID\" \"$TABULA_TARGET_ID\" \"$TABULA_HOME\"\n")
	worker := newWorker(t, policy.SpawnReq{
		KernelID:    "main",
		TenantID:    "tenant-b",
		TargetID:    "skill:env-skill",
		TargetKind:  runtimewire.TargetKindSkill,
		HarnessKind: runtimewire.HarnessKindBash,
		Runtime:     "bash",
		Entry:       "SKILL.md",
		Manifest:    skillManifestJSON(t, manifest.Skill{Name: "env-skill", RootDir: dir, WorkerMode: runtimewire.WorkerModeCold, HarnessKind: runtimewire.HarnessKindBash, Tools: []manifest.SkillTool{{Name: "env_tool", Exec: "bash ${SKILL_DIR}/scripts/env.sh", HarnessKind: runtimewire.HarnessKindBash}}}),
		WorkingDir:  dir,
		Mode:        policy.SpawnModeCold,
		Env:         map[string]string{"TABULA_HOME": filepath.Join(dir, "home")},
	})
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{Op: workerwire.OpInit}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	result, err := worker.Call(context.Background(), workerwire.WorkerCall{Op: workerwire.OpCall, CallID: "call-env", Tool: "env_tool", Args: json.RawMessage(`{}`), SessionID: "session-env"})
	if err != nil || !result.OK {
		t.Fatalf("Call = %#v, %v", result, err)
	}
	var got map[string]string
	if err := json.Unmarshal(result.Data, &got); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if got["skill_dir"] != dir || got["tool"] != "env_tool" || got["call"] != "call-env" || got["session"] != "session-env" || got["tenant"] != "tenant-b" || got["tenant_dir"] != filepath.Join(dir, "home", "tenants", "tenant-b") || got["kernel"] != "main" || got["target"] != "skill:env-skill" || got["home"] != filepath.Join(dir, "home") {
		t.Fatalf("unexpected env data: %#v", got)
	}
}

func TestWorkerNonZeroExitReturnsSkillExecFailed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "scripts", "fail.sh"), "#!/bin/sh\necho harness-boom >&2\nexit 7\n")
	worker := newWorker(t, policy.SpawnReq{
		KernelID:    "main",
		TenantID:    "tenant-c",
		TargetID:    "skill:fail-skill",
		TargetKind:  runtimewire.TargetKindSkill,
		HarnessKind: runtimewire.HarnessKindBash,
		Runtime:     "bash",
		Entry:       "SKILL.md",
		Manifest:    skillManifestJSON(t, manifest.Skill{Name: "fail-skill", RootDir: dir, WorkerMode: runtimewire.WorkerModeCold, HarnessKind: runtimewire.HarnessKindBash, Tools: []manifest.SkillTool{{Name: "fail_tool", Exec: "bash ${SKILL_DIR}/scripts/fail.sh", HarnessKind: runtimewire.HarnessKindBash}}}),
		WorkingDir:  dir,
		Mode:        policy.SpawnModeCold,
	})
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{Op: workerwire.OpInit}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	result, err := worker.Call(context.Background(), workerwire.WorkerCall{Op: workerwire.OpCall, CallID: "call-fail", Tool: "fail_tool", Args: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.OK || result.Error == nil || result.Error.Code != string(runtimewire.ErrorSkillExecFailed) || result.Error.Message != "harness-boom" {
		t.Fatalf("unexpected failure result: %#v", result)
	}
}

func TestWorkerRejectsSecondCall(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "scripts", "ok.sh"), "#!/bin/sh\necho '{}'\n")
	worker := newWorker(t, policy.SpawnReq{
		KernelID:    "main",
		TenantID:    "tenant-d",
		TargetID:    "skill:once",
		TargetKind:  runtimewire.TargetKindSkill,
		HarnessKind: runtimewire.HarnessKindBash,
		Runtime:     "bash",
		Entry:       "SKILL.md",
		Manifest:    skillManifestJSON(t, manifest.Skill{Name: "once", RootDir: dir, WorkerMode: runtimewire.WorkerModeCold, HarnessKind: runtimewire.HarnessKindBash, Tools: []manifest.SkillTool{{Name: "once_tool", Exec: "bash ${SKILL_DIR}/scripts/ok.sh", HarnessKind: runtimewire.HarnessKindBash}}}),
		WorkingDir:  dir,
		Mode:        policy.SpawnModeCold,
	})
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{Op: workerwire.OpInit}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := worker.Call(context.Background(), workerwire.WorkerCall{Op: workerwire.OpCall, CallID: "call-1", Tool: "once_tool", Args: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := worker.Call(context.Background(), workerwire.WorkerCall{Op: workerwire.OpCall, CallID: "call-2", Tool: "once_tool", Args: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("expected cold worker reuse error")
	}
}

func TestWorkerCancelStopsLongRunningSkill(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "scripts", "slow.sh"), "#!/bin/sh\ntrap 'exit 0' TERM\nsleep 30\n")
	worker := newWorker(t, policy.SpawnReq{
		KernelID:    "main",
		TenantID:    "tenant-e",
		TargetID:    "skill:slow",
		TargetKind:  runtimewire.TargetKindSkill,
		HarnessKind: runtimewire.HarnessKindBash,
		Runtime:     "bash",
		Entry:       "SKILL.md",
		Manifest:    skillManifestJSON(t, manifest.Skill{Name: "slow", RootDir: dir, WorkerMode: runtimewire.WorkerModeCold, HarnessKind: runtimewire.HarnessKindBash, Tools: []manifest.SkillTool{{Name: "slow_tool", Exec: "bash ${SKILL_DIR}/scripts/slow.sh", HarnessKind: runtimewire.HarnessKindBash}}}),
		WorkingDir:  dir,
		Mode:        policy.SpawnModeCold,
	})
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{Op: workerwire.OpInit}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := worker.Call(ctx, workerwire.WorkerCall{Op: workerwire.OpCall, CallID: "slow-1", Tool: "slow_tool", Args: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("expected context cancellation")
	}
	if worker.IsAlive() {
		t.Fatal("worker should not remain alive after cancelled call")
	}
}

func TestConcurrentWorkersStayTenantIsolated(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "scripts", "pid.sh"), "#!/bin/sh\nprintf '{\"pid\":%s,\"tenant\":\"%s\"}\\n' \"$$\" \"$TABULA_TENANT_ID\"\n")
	call := func(tenant string) map[string]any {
		worker := newWorker(t, policy.SpawnReq{
			KernelID:    "main",
			TenantID:    tenant,
			TargetID:    "skill:pid",
			TargetKind:  runtimewire.TargetKindSkill,
			HarnessKind: runtimewire.HarnessKindBash,
			Runtime:     "bash",
			Entry:       "SKILL.md",
			Manifest:    skillManifestJSON(t, manifest.Skill{Name: "pid", RootDir: dir, WorkerMode: runtimewire.WorkerModeCold, HarnessKind: runtimewire.HarnessKindBash, Tools: []manifest.SkillTool{{Name: "pid_tool", Exec: "bash ${SKILL_DIR}/scripts/pid.sh", HarnessKind: runtimewire.HarnessKindBash}}}),
			WorkingDir:  dir,
			Mode:        policy.SpawnModeCold,
		})
		if _, err := worker.Init(context.Background(), workerwire.WorkerInit{Op: workerwire.OpInit}); err != nil {
			t.Fatalf("Init: %v", err)
		}
		result, err := worker.Call(context.Background(), workerwire.WorkerCall{Op: workerwire.OpCall, CallID: tenant, Tool: "pid_tool", Args: json.RawMessage(`{}`)})
		if err != nil || !result.OK {
			t.Fatalf("Call(%s) = %#v, %v", tenant, result, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(result.Data, &decoded); err != nil {
			t.Fatalf("decode result: %v", err)
		}
		return decoded
	}
	a := call("tenant-one")
	b := call("tenant-two")
	if a["tenant"] != "tenant-one" || b["tenant"] != "tenant-two" || a["pid"] == b["pid"] {
		t.Fatalf("unexpected tenant isolation results: a=%#v b=%#v", a, b)
	}
}

func newWorker(t *testing.T, req policy.SpawnReq) *Worker {
	t.Helper()
	worker, err := New(req)
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

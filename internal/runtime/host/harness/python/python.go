package python

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	bashharness "github.com/bamanoz/tabula/internal/runtime/host/harness/bash"
	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy"
)

func New(req policy.SpawnReq) (policy.Worker, error) {
	if req.Env == nil {
		req.Env = map[string]string{}
	}
	req.Env = cloneEnv(req.Env)
	req.Env["PYTHONUNBUFFERED"] = "1"
	tabulaHome := strings.TrimSpace(req.Env["TABULA_HOME"])
	if tabulaHome == "" {
		tabulaHome = strings.TrimSpace(os.Getenv("TABULA_HOME"))
	}
	installed := ""
	if tabulaHome != "" {
		installed = filepath.Join(tabulaHome, "_lib", "python", "src")
	}
	if preferred := preferredPython(tabulaHome); preferred != "" {
		if rewritten, err := rewritePythonExecs(req.Manifest, preferred); err == nil {
			req.Manifest = rewritten
		}
	}
	req.Env["PYTHONPATH"] = prependPath(installed, req.Env["PYTHONPATH"], os.Getenv("PYTHONPATH"))
	return bashharness.New(req)
}

func preferredPython(tabulaHome string) string {
	if path := preferredPythonAt(os.Getenv("TABULA_VENV")); path != "" {
		return path
	}
	if strings.TrimSpace(tabulaHome) == "" {
		return ""
	}
	return preferredPythonAt(filepath.Join(tabulaHome, ".venv"))
}

func preferredPythonAt(venv string) string {
	venv = strings.TrimSpace(venv)
	if venv == "" {
		return ""
	}
	path := filepath.Join(venv, "bin", "python3")
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

func rewritePythonExecs(raw []byte, pythonPath string) ([]byte, error) {
	if len(raw) == 0 || strings.TrimSpace(pythonPath) == "" {
		return raw, nil
	}
	var skill manifest.Skill
	if err := json.Unmarshal(raw, &skill); err != nil {
		return raw, err
	}
	for i := range skill.Tools {
		skill.Tools[i].Exec = replacePythonInterpreter(skill.Tools[i].Exec, pythonPath)
	}
	return json.Marshal(skill)
}

func replacePythonInterpreter(execText, pythonPath string) string {
	trimmed := strings.TrimSpace(execText)
	for _, prefix := range []string{"python ", "python3 ", "python3.11 ", "python3.12 ", "python3.13 "} {
		if strings.HasPrefix(trimmed, prefix) {
			return pythonPath + trimmed[len(prefix)-1:]
		}
	}
	return execText
}

func cloneEnv(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func prependPath(prefix string, values ...string) string {
	parts := make([]string, 0, len(values)+1)
	if strings.TrimSpace(prefix) != "" {
		parts = append(parts, prefix)
	}
	seen := map[string]struct{}{}
	for _, value := range values {
		for _, part := range filepath.SplitList(value) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

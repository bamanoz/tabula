package node

import (
	"os"
	"path/filepath"
	"strings"

	bashharness "github.com/bamanoz/tabula/internal/runtime/host/harness/bash"
	"github.com/bamanoz/tabula/internal/runtime/host/policy"
)

// New returns the runtime-side node harness. It reuses the shared cold exec
// scaffold from M3-02 and only adds node-specific environment shaping.
func New(req policy.SpawnReq) (policy.Worker, error) {
	if req.Env == nil {
		req.Env = map[string]string{}
	}
	req.Env = cloneEnv(req.Env)
	req.Env["NODE_NO_WARNINGS"] = "1"
	tabulaHome := strings.TrimSpace(req.Env["TABULA_HOME"])
	if tabulaHome == "" {
		tabulaHome = strings.TrimSpace(os.Getenv("TABULA_HOME"))
	}
	installed := ""
	if tabulaHome != "" {
		installed = filepath.Join(tabulaHome, "_lib", "node", "node_modules")
	}
	req.Env["NODE_PATH"] = prependPath(installed, req.Env["NODE_PATH"], os.Getenv("NODE_PATH"))
	return bashharness.New(req)
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

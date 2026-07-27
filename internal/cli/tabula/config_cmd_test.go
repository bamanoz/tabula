package tabula

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
)

func TestConfigInspectCmdPrintsJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	writeTabulaRuntimeConfig(t, home)
	writeTabulaPlugin(t, filepath.Join(home, "plugins", "fs", "plugin.toml"), "fs", "fs_read")

	var stdout, stderr bytes.Buffer
	if code := configInspectCmd([]string{"--format=json", "--plugin", "fs"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var body struct {
		Paths struct {
			Home string `json:"home"`
		} `json:"paths"`
		Plugins []struct {
			ID string `json:"id"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		t.Fatalf("json output invalid: %v\n%s", err, stdout.String())
	}
	if body.Paths.Home != home || len(body.Plugins) != 1 || body.Plugins[0].ID != "fs" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestConfigInspectCmdRejectsUnsupportedFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := configInspectCmd([]string{"--format=yaml"}, &stdout, &stderr); code == 0 {
		t.Fatal("expected failure")
	}
	if !strings.Contains(stderr.String(), "unsupported format") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func writeTabulaRuntimeConfig(t *testing.T, home string) {
	t.Helper()
	if err := runtimeconfig.Save(filepath.Join(home, "config", "runtime.toml"), runtimeconfig.Config{
		Kernels:    []runtimeconfig.Kernel{{ID: "local", URL: "unix:///tmp/runtime.sock", TokenFile: "/tmp/token"}},
		PluginDirs: []string{filepath.Join(home, "plugins")},
	}); err != nil {
		t.Fatalf("save runtime config: %v", err)
	}
}

func writeTabulaPlugin(t *testing.T, path, id, tool string) {
	t.Helper()
	body := `id = "` + id + `"
name = "` + id + `"
version = "0.1.0"
[worker]
command = ["python3", "run.py"]
mode = "warm"

[[tools]]
name = "` + tool + `"

[requires]
kernel = ">=0.9.0,<1.0.0"
protocol_version = 1
sdk = "tabula-plugin-sdk>=1.0.0,<2.0.0"
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write plugin: %v", err)
	}
}

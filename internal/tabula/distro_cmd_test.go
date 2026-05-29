package tabula

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bamanoz/tabula/internal/runtime/trust"
)

// writeRuntimeConfigWithDistro stages a minimal runtime.toml that declares
// the active distro. Used by trust CLI tests so they exercise the same
// happy path the installer produces.
func writeRuntimeConfigWithDistro(t *testing.T, home, distroID, distroDir string) {
	t.Helper()
	cfgPath := filepath.Join(home, "config", "runtime.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	body := "plugin_dirs = []\nskill_dirs = []\n\n" +
		"[[kernel]]\nid = \"main\"\nurl = \"unix:///tmp/x.sock\"\ntoken_file = \"/tmp/t\"\ntenants = [\"*\"]\n\n" +
		"[distro]\nactive = \"" + distroID + "\"\ndir = \"" + distroDir + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write runtime.toml: %v", err)
	}
}

// stageDistroDir lays out a minimal distro tree under home/distrib/<id>/
// good enough for HashDir to succeed. The Go-side HashDir tests already
// pin the exact byte-level digest; here we only need *some* distro tree
// the trust CLI can lock onto.
func stageDistroDir(t *testing.T, home, distroID string) string {
	t.Helper()
	dir := filepath.Join(home, "distrib", distroID)
	if err := os.MkdirAll(filepath.Join(dir, "application"), 0o755); err != nil {
		t.Fatalf("mkdir distro: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "boot.py"), []byte("print('boot')\n"), 0o644); err != nil {
		t.Fatalf("write boot.py: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "distro.toml"), []byte("id = \""+distroID+"\"\n"), 0o644); err != nil {
		t.Fatalf("write distro.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "application", "main.py"), []byte("x = 1\n"), 0o644); err != nil {
		t.Fatalf("write main.py: %v", err)
	}
	return dir
}

func TestDistroTrustCmd_ApprovesActiveDistro(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	distroDir := stageDistroDir(t, home, "code-immune")
	writeRuntimeConfigWithDistro(t, home, "code-immune", distroDir)

	var stdout, stderr bytes.Buffer
	stdin := strings.NewReader("")
	code := distroTrustCmd([]string{"--yes"}, &stdout, &stderr, stdin)
	if code != 0 {
		t.Fatalf("trust exit = %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Trusted code-immune") {
		t.Errorf("missing approval line in stdout: %s", stdout.String())
	}
	// DB should now report the distro as trusted with a matching SHA.
	db, err := trust.Load(filepath.Join(home, "state", "trust.json"))
	if err != nil {
		t.Fatalf("load db: %v", err)
	}
	rec, ok := db.Distros["code-immune"]
	if !ok {
		t.Fatalf("entry missing in db: %#v", db)
	}
	if rec.BootSHA256 == "" {
		t.Errorf("empty sha in record: %#v", rec)
	}
	if rec.TrustedBy != "user" {
		t.Errorf("trusted_by = %q, want user", rec.TrustedBy)
	}
}

func TestDistroTrustCmd_RefusesNonActiveDistro(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	distroDir := stageDistroDir(t, home, "code-immune")
	writeRuntimeConfigWithDistro(t, home, "code-immune", distroDir)

	var stdout, stderr bytes.Buffer
	code := distroTrustCmd([]string{"--yes", "other-distro"}, &stdout, &stderr, strings.NewReader(""))
	if code == 0 {
		t.Fatalf("expected refusal, got 0; stdout=%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "refusing to trust") {
		t.Errorf("expected mismatch error in stderr: %s", stderr.String())
	}
}

func TestDistroTrustCmd_PromptAborts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	distroDir := stageDistroDir(t, home, "code-immune")
	writeRuntimeConfigWithDistro(t, home, "code-immune", distroDir)

	var stdout, stderr bytes.Buffer
	// User answers "n" — interactive abort, no write.
	code := distroTrustCmd(nil, &stdout, &stderr, strings.NewReader("n\n"))
	if code == 0 {
		t.Fatalf("expected non-zero exit on abort")
	}
	if _, err := os.Stat(filepath.Join(home, "state", "trust.json")); err == nil {
		t.Errorf("trust.json should not exist after abort")
	}
}

func TestDistroTrustCmd_MissingTabulaHome(t *testing.T) {
	t.Setenv("TABULA_HOME", "")
	var stdout, stderr bytes.Buffer
	code := distroTrustCmd([]string{"--yes"}, &stdout, &stderr, strings.NewReader(""))
	if code == 0 {
		t.Fatalf("expected non-zero exit, stdout=%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "TABULA_HOME") {
		t.Errorf("expected TABULA_HOME error in stderr: %s", stderr.String())
	}
}

func TestDistroTrustCmd_NoActiveDistro(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	// runtime.toml without a [distro] table → installer hasn't recorded
	// an active distro yet.
	cfgPath := filepath.Join(home, "config", "runtime.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte("plugin_dirs = []\nskill_dirs = []\n\n[[kernel]]\nid=\"m\"\nurl=\"unix:///t\"\ntoken_file=\"/t\"\ntenants=[\"*\"]\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := distroTrustCmd([]string{"--yes"}, &stdout, &stderr, strings.NewReader(""))
	if code == 0 {
		t.Fatalf("expected non-zero exit, stdout=%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no active distro") {
		t.Errorf("expected no-active-distro error: %s", stderr.String())
	}
}

func TestDistroTrustCmd_ListEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	var stdout, stderr bytes.Buffer
	code := distroTrustCmd([]string{"--list"}, &stdout, &stderr, strings.NewReader(""))
	if code != 0 {
		t.Fatalf("list exit = %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no trusted distros") {
		t.Errorf("expected empty marker, got %s", stdout.String())
	}
}

func TestDistroTrustCmd_ListPopulated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	distroDir := stageDistroDir(t, home, "code-immune")
	writeRuntimeConfigWithDistro(t, home, "code-immune", distroDir)
	if _, err := trust.Approve(filepath.Join(home, "state", "trust.json"), "code-immune", distroDir, "user"); err != nil {
		t.Fatalf("seed approve: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := distroTrustCmd([]string{"--list"}, &stdout, &stderr, strings.NewReader(""))
	if code != 0 {
		t.Fatalf("list exit = %d, stderr=%s", code, stderr.String())
	}
	var parsed map[string]trust.Record
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("parse list json: %v\nout=%s", err, stdout.String())
	}
	if _, ok := parsed["code-immune"]; !ok {
		t.Errorf("missing entry in list: %#v", parsed)
	}
}

func TestDistroUntrustCmd_Revokes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	distroDir := stageDistroDir(t, home, "code-immune")
	dbPath := filepath.Join(home, "state", "trust.json")
	if _, err := trust.Approve(dbPath, "code-immune", distroDir, "user"); err != nil {
		t.Fatalf("seed approve: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := distroUntrustCmd([]string{"code-immune"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("untrust exit = %d, stderr=%s", code, stderr.String())
	}
	db, err := trust.Load(dbPath)
	if err != nil {
		t.Fatalf("load db: %v", err)
	}
	if _, ok := db.Distros["code-immune"]; ok {
		t.Errorf("entry still present after revoke: %#v", db)
	}
}

func TestDistroUntrustCmd_MissingIsNoOp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TABULA_HOME", home)
	var stdout, stderr bytes.Buffer
	code := distroUntrustCmd([]string{"ghost"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("untrust missing exit = %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "not trusted") {
		t.Errorf("expected no-op message: %s", stdout.String())
	}
}

func TestDistroUntrustCmd_RequiresID(t *testing.T) {
	t.Setenv("TABULA_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := distroUntrustCmd(nil, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit for missing id")
	}
	if !strings.Contains(stderr.String(), "Usage") {
		t.Errorf("expected Usage message: %s", stderr.String())
	}
}

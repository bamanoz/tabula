package hostcmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type gitDiagnosticCase struct {
	argv []string
	cwd  string
	env  []string
}

func TestDiagnosticGitVersionReturns(t *testing.T) {
	if os.Getenv("TABULA_RUN_GIT_DIAGNOSTIC") != "1" {
		t.Skip("set TABULA_RUN_GIT_DIAGNOSTIC=1 to run host git process diagnostic")
	}

	cases := map[string]gitDiagnosticCase{
		"direct_go_exec": {argv: []string{"git", "--version"}},
	}
	if runtime.GOOS == "windows" {
		cases["powershell_exec"] = gitDiagnosticCase{argv: []string{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, "-NoProfile", "-NonInteractive", "-Command", "git --version"}}
	}
	if _, err := exec.LookPath("python"); err == nil {
		cases["python_exec"] = gitDiagnosticCase{argv: []string{"python", "-c", "import subprocess; p = subprocess.run(['git', '--version'], capture_output=True, text=True, timeout=5); print(p.returncode, p.stdout.strip(), p.stderr.strip())"}}
	}
	addExecPluginDiagnosticCase(t, cases)

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			t.Cleanup(cancel)

			cmd := exec.CommandContext(ctx, tc.argv[0], tc.argv[1:]...)
			cmd.Dir = tc.cwd
			if len(tc.env) > 0 {
				cmd.Env = append(os.Environ(), tc.env...)
			}
			out, err := cmd.CombinedOutput()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				t.Fatalf("%s timed out on %s/%s running %q; PATH=%q; output=%q", name, runtime.GOOS, runtime.GOARCH, tc.argv, os.Getenv("PATH"), string(out))
			}
			if err != nil {
				t.Fatalf("%s failed running %q: %v; PATH=%q; output=%q", name, tc.argv, err, os.Getenv("PATH"), string(out))
			}
			t.Logf("%s output: %s", name, out)
		})
	}
}

func addExecPluginDiagnosticCase(t *testing.T, cases map[string]gitDiagnosticCase) {
	if _, err := exec.LookPath("python"); err != nil {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	repoRoot := filepath.Clean(filepath.Join(cwd, "..", "..", ".."))
	bundlesRoot := filepath.Clean(filepath.Join(repoRoot, "..", "tabula-bundles"))
	testPath := filepath.Join(bundlesRoot, "workspace", "exec", "tests", "test_exec_plugin.py")
	if _, err := os.Stat(testPath); err != nil {
		return
	}
	pythonPackages := filepath.Join(repoRoot, ".tabula-dev", "distrib", "active", "current", "packages", "python", "src")
	cases["exec_plugin_unittest"] = gitDiagnosticCase{
		argv: []string{"python", "-m", "unittest", "workspace.exec.tests.test_exec_plugin.ExecPluginTests.test_diagnostic_git_version_returns_through_exec_shell", "-v"},
		cwd:  bundlesRoot,
		env: []string{
			"TABULA_RUN_GIT_DIAGNOSTIC=1",
			"PYTHONPATH=" + bundlesRoot + string(os.PathListSeparator) + pythonPackages,
			"TABULA_GIT_DIAGNOSTIC_CWD=" + repoRoot,
		},
	}
}

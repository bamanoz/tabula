package hostcmd

import (
	"path/filepath"
	"reflect"
	"testing"

	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
)

func TestRuntimePythonPathIncludesInstalledActivePackages(t *testing.T) {
	home := t.TempDir()
	generation := filepath.Join(home, "distrib", "code-immune", "generations", "0001")
	got := runtimePythonPath(runtimeconfig.Config{Distro: runtimeconfig.Distro{Dir: generation}}, home)
	want := []string{
		filepath.Join(home, "distrib", "active", "packages", "python", "src"),
		filepath.Join(generation, "packages", "python", "src"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("runtimePythonPath() = %#v, want %#v", got, want)
	}
}

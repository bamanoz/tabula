package tabula

import "testing"

func TestParseServeFlagsDefaultsToExternalRuntimeMode(t *testing.T) {
	opts, code := parseServeFlags(nil)
	if code != 0 {
		t.Fatalf("parseServeFlags default = %d", code)
	}
	if opts.runtimeMode != localRuntimeModeExternal {
		t.Fatalf("runtimeMode = %q, want external", opts.runtimeMode)
	}
}

func TestParseServeFlagsAcceptsForeground(t *testing.T) {
	_, code := parseServeFlags([]string{"--foreground"})
	if code != 0 {
		t.Fatalf("parseServeFlags --foreground = %d", code)
	}
}

func TestParseServeFlagsRejectsUnknown(t *testing.T) {
	_, code := parseServeFlags([]string{"--detach"})
	if code == 0 {
		t.Fatal("expected unknown serve flag to fail")
	}
}

func TestParseServeFlagsRuntimeModeFlag(t *testing.T) {
	t.Setenv("TABULA_LOCAL_RUNTIME_MODE", "disabled")
	opts, code := parseServeFlags([]string{"--runtime-mode", "external"})
	if code != 0 {
		t.Fatalf("parseServeFlags runtime-mode = %d", code)
	}
	if opts.runtimeMode != localRuntimeModeExternal {
		t.Fatalf("runtimeMode = %q, want external", opts.runtimeMode)
	}
}

func TestParseServeFlagsRuntimeModeEnv(t *testing.T) {
	t.Setenv("TABULA_LOCAL_RUNTIME_MODE", "disabled")
	opts, code := parseServeFlags(nil)
	if code != 0 {
		t.Fatalf("parseServeFlags env runtime-mode = %d", code)
	}
	if opts.runtimeMode != localRuntimeModeDisabled {
		t.Fatalf("runtimeMode = %q, want disabled", opts.runtimeMode)
	}
}

func TestParseServeFlagsRejectsInvalidRuntimeMode(t *testing.T) {
	_, code := parseServeFlags([]string{"--runtime-mode", "childish"})
	if code == 0 {
		t.Fatal("expected invalid runtime mode to fail")
	}
}

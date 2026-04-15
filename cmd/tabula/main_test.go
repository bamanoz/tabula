package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebSocketOriginCheck_DefaultAllowsLocalhost(t *testing.T) {
	t.Setenv("TABULA_ALLOWED_ORIGINS", "")

	req := httptest.NewRequest(http.MethodGet, "http://tabula.local/ws", nil)
	req.Host = "tabula.local:8089"
	req.Header.Set("Origin", "http://localhost:3000")

	if !checkWebSocketOrigin(req) {
		t.Fatal("expected localhost origin to be allowed by default")
	}
}

func TestWebSocketOriginCheck_DefaultRejectsRemoteOrigin(t *testing.T) {
	t.Setenv("TABULA_ALLOWED_ORIGINS", "")

	req := httptest.NewRequest(http.MethodGet, "http://tabula.local/ws", nil)
	req.Host = "tabula.local:8089"
	req.Header.Set("Origin", "https://evil.example")

	if checkWebSocketOrigin(req) {
		t.Fatal("expected remote origin to be rejected by default")
	}
}

func TestWebSocketOriginCheck_AllowsConfiguredOrigin(t *testing.T) {
	t.Setenv("TABULA_ALLOWED_ORIGINS", "https://app.example, https://admin.example")

	req := httptest.NewRequest(http.MethodGet, "http://tabula.local/ws", nil)
	req.Host = "tabula.local:8089"
	req.Header.Set("Origin", "https://admin.example")

	if !checkWebSocketOrigin(req) {
		t.Fatal("expected configured origin to be allowed")
	}
}

func TestParseSkillExecMap(t *testing.T) {
	tools := json.RawMessage(`[{"name":"echo","exec":"python skills/echo/run.py"}]`)

	parsed, err := parseSkillExecMap(tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(parsed))
	}
	if parsed[0].Name != "echo" || parsed[0].Exec != "python skills/echo/run.py" {
		t.Fatalf("unexpected parsed tool: %+v", parsed[0])
	}
}

func TestParseSkillExecMap_InvalidJSON(t *testing.T) {
	_, err := parseSkillExecMap(json.RawMessage(`{`))
	if err == nil {
		t.Fatal("expected parse error for invalid tools json")
	}
}

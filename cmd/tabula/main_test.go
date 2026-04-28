package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bamanoz/tabula/internal/kernel"
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

func TestValidateBootSkillTools_ValidTrimsAndPreservesMetadata(t *testing.T) {
	raw := json.RawMessage(`[{"name":" echo ","description":"Echo text","params":{"text":{"type":"string"}},"required":["text"],"exec":" python skills/echo/run.py "}]`)

	bootTools, parsed, err := validateBootSkillTools(raw)
	if err != nil {
		t.Fatalf("validateBootSkillTools: %v", err)
	}
	if len(bootTools) != 1 || len(parsed) != 1 {
		t.Fatalf("expected 1 boot tool and parsed descriptor, got %d/%d", len(bootTools), len(parsed))
	}
	if parsed[0].Name != "echo" || parsed[0].Exec != "python skills/echo/run.py" {
		t.Fatalf("expected trimmed name/exec, got %+v", parsed[0])
	}
	var meta map[string]any
	if err := json.Unmarshal(bootTools[0], &meta); err != nil {
		t.Fatalf("unmarshal boot metadata: %v", err)
	}
	if meta["description"] != "Echo text" || meta["params"] == nil || meta["required"] == nil {
		t.Fatalf("expected non-exec metadata to be preserved, got %#v", meta)
	}
}

func TestValidateBootSkillTools_MissingOrBlankFieldsFail(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{name: "missing name", raw: json.RawMessage(`[{"exec":"run.sh"}]`), want: "skills[0].name is required"},
		{name: "blank name", raw: json.RawMessage(`[{"name":"  ","exec":"run.sh"}]`), want: "skills[0].name is required"},
		{name: "missing exec", raw: json.RawMessage(`[{"name":"echo"}]`), want: "skills[0].exec is required"},
		{name: "blank exec", raw: json.RawMessage(`[{"name":"echo","exec":"  "}]`), want: "skills[0].exec is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := validateBootSkillTools(tt.raw)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if got := err.Error(); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestValidateBootSkillTools_DuplicateNamesFail(t *testing.T) {
	_, _, err := validateBootSkillTools(json.RawMessage(`[{"name":"echo","exec":"a.sh"},{"name":" echo ","exec":"b.sh"}]`))
	if err == nil {
		t.Fatal("expected duplicate-name validation error")
	}
	if got := err.Error(); got != `skills[1].name duplicates skills[0].name "echo"` {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestValidateBootSkillTools_LegacyToolsFallbackInvalidDescriptorFails(t *testing.T) {
	cfg := &BootConfig{Tools: json.RawMessage(`[{"name":"legacy"}]`)}
	raw, legacy := resolveBootSkills(cfg)
	if !legacy {
		t.Fatal("expected legacy tools fallback")
	}
	_, _, err := validateBootSkillTools(raw)
	if err == nil {
		t.Fatal("expected missing exec validation error")
	}
	if got := err.Error(); got != "skills[0].exec is required" {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestResolveBootSkills_PrefersSkillsField(t *testing.T) {
	cfg := &BootConfig{
		Skills: json.RawMessage(`[{"name":"a","exec":"a.sh"}]`),
		Tools:  json.RawMessage(`[{"name":"b","exec":"b.sh"}]`),
	}
	raw, legacy := resolveBootSkills(cfg)
	if legacy {
		t.Fatal("expected legacy=false when skills is populated")
	}
	if string(raw) != `[{"name":"a","exec":"a.sh"}]` {
		t.Fatalf("expected skills field to win, got %s", raw)
	}
}

func TestResolveBootSkills_FallsBackToLegacyTools(t *testing.T) {
	cfg := &BootConfig{
		Tools: json.RawMessage(`[{"name":"b","exec":"b.sh"}]`),
	}
	raw, legacy := resolveBootSkills(cfg)
	if !legacy {
		t.Fatal("expected legacy=true when only tools is populated")
	}
	if string(raw) != `[{"name":"b","exec":"b.sh"}]` {
		t.Fatalf("expected tools fallback, got %s", raw)
	}
}

func TestResolveBootSkills_EmptyJSONTreatedAsAbsent(t *testing.T) {
	cfg := &BootConfig{
		Skills: json.RawMessage(`[]`),
		Tools:  json.RawMessage(`[]`),
	}
	raw, legacy := resolveBootSkills(cfg)
	if raw != nil || legacy {
		t.Fatalf("expected nil/false for empty JSON arrays, got raw=%s legacy=%v", raw, legacy)
	}
}

func TestResolveBootSkills_NilConfig(t *testing.T) {
	raw, legacy := resolveBootSkills(nil)
	if raw != nil || legacy {
		t.Fatalf("expected nil/false for nil config, got raw=%s legacy=%v", raw, legacy)
	}
}

func TestLoadEnvFileLoadsMissingValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("TABULA_PROVIDER=openai\nOPENAI_MODEL=gpt-5.4\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	t.Setenv("TABULA_PROVIDER", "")
	os.Unsetenv("TABULA_PROVIDER")
	t.Setenv("OPENAI_MODEL", "")
	os.Unsetenv("OPENAI_MODEL")

	loadEnvFile(path)

	if got := os.Getenv("TABULA_PROVIDER"); got != "openai" {
		t.Fatalf("expected TABULA_PROVIDER=openai, got %q", got)
	}
	if got := os.Getenv("OPENAI_MODEL"); got != "gpt-5.4" {
		t.Fatalf("expected OPENAI_MODEL=gpt-5.4, got %q", got)
	}
}

func TestLoadEnvFileDoesNotOverrideExistingValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("TABULA_PROVIDER=anthropic\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	t.Setenv("TABULA_PROVIDER", "openai")
	loadEnvFile(path)

	if got := os.Getenv("TABULA_PROVIDER"); got != "openai" {
		t.Fatalf("expected existing TABULA_PROVIDER to win, got %q", got)
	}
}

func TestHealthEndpoint(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "127.0.0.1:8089")

	req := httptest.NewRequest(http.MethodGet, "http://tabula.local/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected application/json content type, got %q", got)
	}

	var body struct {
		Status          string `json:"status"`
		Version         string `json:"version"`
		Commit          string `json:"commit"`
		ProtocolVersion int    `json:"protocol_version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal health response: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("expected status ok, got %q", body.Status)
	}
	if body.Version == "" || body.Commit == "" {
		t.Fatalf("expected version and commit in response, got %+v", body)
	}
	if body.ProtocolVersion != 1 {
		t.Fatalf("expected protocol version 1, got %d", body.ProtocolVersion)
	}
}

func TestHealthEndpointRejectsNonGET(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "127.0.0.1:8089")

	req := httptest.NewRequest(http.MethodPost, "http://tabula.local/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("expected Allow: GET, got %q", got)
	}
}

func TestPluginSnapshotEndpointAllowsLoopbackRequest(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "127.0.0.1:8089")

	req := httptest.NewRequest(http.MethodGet, "http://localhost:8089/internal/snapshot/plugins", nil)
	req.Host = "localhost:8089"
	req.RemoteAddr = "127.0.0.1:34567"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected application/json content type, got %q", got)
	}
}

func TestPluginSnapshotEndpointAllowsIPv6LoopbackRequest(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "[::1]:8089")

	req := httptest.NewRequest(http.MethodGet, "http://[::1]:8089/internal/snapshot/plugins", nil)
	req.Host = "[::1]:8089"
	req.RemoteAddr = "[::1]:34567"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPluginSnapshotEndpointRejectsRemoteAddress(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "0.0.0.0:8089")

	req := httptest.NewRequest(http.MethodGet, "http://localhost:8089/internal/snapshot/plugins", nil)
	req.Host = "localhost:8089"
	req.RemoteAddr = "203.0.113.10:34567"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPluginSnapshotEndpointRejectsRemoteHost(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "0.0.0.0:8089")

	req := httptest.NewRequest(http.MethodGet, "http://example.com/internal/snapshot/plugins", nil)
	req.Host = "example.com"
	req.RemoteAddr = "127.0.0.1:34567"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPluginSnapshotEndpointRejectsMalformedRemoteAddr(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "127.0.0.1:8089")

	req := httptest.NewRequest(http.MethodGet, "http://localhost:8089/internal/snapshot/plugins", nil)
	req.Host = "localhost:8089"
	req.RemoteAddr = "not-a-host-port"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPluginSnapshotEndpointRejectsForwardedLocalityHeaders(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "127.0.0.1:8089")

	req := httptest.NewRequest(http.MethodGet, "http://localhost:8089/internal/snapshot/plugins", nil)
	req.Host = "localhost:8089"
	req.RemoteAddr = "203.0.113.10:34567"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("X-Forwarded-Host", "localhost:8089")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPluginSnapshotEndpointRejectsNonGETBeforeSnapshot(t *testing.T) {
	hub := kernel.NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	mux := http.NewServeMux()
	registerKernelHTTPHandlers(mux, hub, "127.0.0.1:8089")

	req := httptest.NewRequest(http.MethodPost, "http://localhost:8089/internal/snapshot/plugins", nil)
	req.Host = "localhost:8089"
	req.RemoteAddr = "127.0.0.1:34567"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("expected Allow: GET, got %q", got)
	}
}

func TestFilterKernelTools_DefaultIsEmpty(t *testing.T) {
	filtered, warnings, err := filterKernelTools(embeddedToolsJSON, nil)
	if err != nil {
		t.Fatalf("filterKernelTools: %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("expected no builtin tools by default, got %d", len(filtered))
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for empty default, got %#v", warnings)
	}
}

func TestFilterKernelTools_ExplicitLegacySelectionWarnsAndReturnsEmpty(t *testing.T) {
	filtered, warnings, err := filterKernelTools(embeddedToolsJSON, []string{"shell_exec", "process_list"})
	if err != nil {
		t.Fatalf("filterKernelTools: %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("expected explicit legacy tools to be empty, got %d", len(filtered))
	}
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %#v", warnings)
	}
	if warnings[0] == "" || warnings[1] == "" {
		t.Fatalf("expected non-empty warnings, got %#v", warnings)
	}
}

func TestFilterKernelTools_UnknownSelectionWarnsAndReturnsEmpty(t *testing.T) {
	filtered, warnings, err := filterKernelTools(embeddedToolsJSON, []string{"does_not_exist"})
	if err != nil {
		t.Fatalf("filterKernelTools: %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("expected unknown kernel tool selection to be empty, got %d", len(filtered))
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %#v", warnings)
	}
}

func TestLoadToolMetas_ParsesEmbeddedToolNames(t *testing.T) {
	metas, err := loadToolMetas(embeddedToolsJSON)
	if err != nil {
		t.Fatalf("loadToolMetas: %v", err)
	}
	if len(metas) != 0 {
		t.Fatalf("expected empty embedded kernel tool metadata, got %#v", metas)
	}
}

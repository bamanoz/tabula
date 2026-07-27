package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestNewUsesExplicitJSONFormatAndComponent(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Component:     "runtime",
		ConsoleLevel:  "info",
		ConsoleFormat: "json",
		ConsoleWriter: &buf,
		FileLevel:     "silent",
		ConsoleOutput: "discard",
	})

	logger.Info("hello", "tenant_id", "default")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("decode json log: %v", err)
	}
	if record["msg"] != "hello" {
		t.Fatalf("msg = %v, want hello", record["msg"])
	}
	if record["component"] != "runtime" {
		t.Fatalf("component = %v, want runtime", record["component"])
	}
	if record["tenant_id"] != "default" {
		t.Fatalf("tenant_id = %v, want default", record["tenant_id"])
	}
}

func TestNewUsesExplicitTextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		ConsoleLevel:  "INFO",
		ConsoleFormat: "text",
		ConsoleWriter: &buf,
		FileLevel:     "silent",
	})

	logger.Debug("debug hidden")
	logger.Info("hello")

	out := buf.String()
	if strings.Contains(out, "debug hidden") {
		t.Fatalf("debug log should be filtered: %q", out)
	}
	if !strings.Contains(out, "msg=hello") {
		t.Fatalf("text log missing message: %q", out)
	}
}

func TestLevelVarCanChangeAtRuntime(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		ConsoleLevel:  "info",
		ConsoleFormat: "json",
		ConsoleWriter: &buf,
		FileLevel:     "silent",
	})

	logger.Debug("before")
	logger.ConsoleLevel.Set(slog.LevelDebug)
	logger.Debug("after")

	out := buf.String()
	if strings.Contains(out, "before") {
		t.Fatalf("debug before level change should be filtered: %q", out)
	}
	if !strings.Contains(out, "after") {
		t.Fatalf("debug after level change missing: %q", out)
	}
}

func TestApplyEnvUsesPrefix(t *testing.T) {
	t.Setenv("TABULA_RUNTIME_LOG_LEVEL", "debug")
	t.Setenv("TABULA_RUNTIME_LOG_TYPE", "text")
	t.Setenv("TABULA_RUNTIME_LOG_OUTPUT", "stderr")
	t.Setenv("TABULA_RUNTIME_LOG_FILE", "/tmp/runtime.log")
	t.Setenv("TABULA_RUNTIME_FILE_LOG_LEVEL", "error")
	t.Setenv("TABULA_RUNTIME_FILE_LOG_TYPE", "text")

	cfg := ApplyEnv(Config{ConsoleLevel: "info", ConsoleFormat: "json"}, "TABULA_RUNTIME")

	if cfg.ConsoleLevel != "debug" {
		t.Fatalf("ConsoleLevel = %q, want debug", cfg.ConsoleLevel)
	}
	if cfg.ConsoleFormat != "text" {
		t.Fatalf("ConsoleFormat = %q, want text", cfg.ConsoleFormat)
	}
	if cfg.ConsoleOutput != "stderr" {
		t.Fatalf("ConsoleOutput = %q, want stderr", cfg.ConsoleOutput)
	}
	if cfg.FilePath != "/tmp/runtime.log" {
		t.Fatalf("FilePath = %q, want /tmp/runtime.log", cfg.FilePath)
	}
	if cfg.FileLevel != "error" {
		t.Fatalf("FileLevel = %q, want error", cfg.FileLevel)
	}
	if cfg.FileFormat != "text" {
		t.Fatalf("FileFormat = %q, want text", cfg.FileFormat)
	}
}

func TestMultiHandlerReturnsFirstError(t *testing.T) {
	want := errHandlerError{}
	mh := &multiHandler{handlers: []slog.Handler{errHandler{err: want}, discardHandler{}}}
	record := slog.NewRecord(time.Time{}, slog.LevelInfo, "hello", 0)

	if err := mh.Handle(context.Background(), record); err != want {
		t.Fatalf("Handle error = %v, want %v", err, want)
	}
}

type errHandlerError struct{}

func (errHandlerError) Error() string { return "handler failed" }

type errHandler struct {
	err error
}

func (errHandler) Enabled(context.Context, slog.Level) bool    { return true }
func (e errHandler) Handle(context.Context, slog.Record) error { return e.err }
func (e errHandler) WithAttrs([]slog.Attr) slog.Handler        { return e }
func (e errHandler) WithGroup(string) slog.Handler             { return e }

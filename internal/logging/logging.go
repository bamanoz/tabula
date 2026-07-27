// Package logging provides structured logging setup for Tabula processes.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	formatAuto = "auto"
	formatJSON = "json"
	formatText = "text"

	outputStdout  = "stdout"
	outputStderr  = "stderr"
	outputDiscard = "discard"
)

// Config controls logging policy for one process.
type Config struct {
	// Component is attached to every log record when set.
	Component string

	// ConsoleLevel controls console logging (default: "warn"). Set to
	// "silent" to disable.
	ConsoleLevel string
	// ConsoleFormat controls console format: "auto", "json", or "text".
	// Empty means "auto".
	ConsoleFormat string
	// ConsoleOutput controls default console destination: "stdout", "stderr",
	// or "discard". Empty means "stdout". ConsoleWriter wins when set.
	ConsoleOutput string
	// ConsoleWriter overrides ConsoleOutput. Useful for tests and runtime stdio.
	ConsoleWriter io.Writer

	// FileLevel controls file logging (default: "silent").
	FileLevel string
	// FileFormat controls file format: "json" or "text". Empty means "json".
	FileFormat string
	// FilePath is required when FileLevel is not silent.
	FilePath string
	// Max megabytes before rotation (default 10).
	MaxSizeMB int
	// Max days to retain old log files (default 7).
	MaxAgeDays int
	// Max number of old log files to keep (default 3).
	MaxBackups int
	// Compress rotated log files (default true when set by caller).
	Compress bool
}

// Logger wraps a slog.Logger, mutable level controls, and an optional closer.
type Logger struct {
	*slog.Logger
	ConsoleLevel *slog.LevelVar
	FileLevel    *slog.LevelVar
	closer       io.Closer
}

// Close closes the underlying file writer if any.
func (l *Logger) Close() error {
	if l.closer != nil {
		return l.closer.Close()
	}
	return nil
}

// Setup creates a logger and sets it as the slog default.
func Setup(cfg Config) *Logger {
	logger := New(cfg)
	slog.SetDefault(logger.Logger)
	return logger
}

// New creates a logger without changing slog.Default.
func New(cfg Config) *Logger {
	consoleLevel := newLevelVar(cfg.ConsoleLevel, slog.LevelWarn)
	fileLevel := newLevelVar(cfg.FileLevel, silentLevel)

	var handlers []slog.Handler
	var closer io.Closer

	consoleWriter := resolveConsoleWriter(cfg)
	if consoleLevel.Level() < silentLevel && consoleWriter != io.Discard {
		handlers = append(handlers, newHandler(consoleWriter, resolveConsoleFormat(cfg, consoleWriter), consoleLevel))
	}

	if fileLevel.Level() < silentLevel && strings.TrimSpace(cfg.FilePath) != "" {
		lj := &lumberjack.Logger{
			Filename:   cfg.FilePath,
			MaxSize:    positiveOr(cfg.MaxSizeMB, 10),
			MaxAge:     positiveOr(cfg.MaxAgeDays, 7),
			MaxBackups: positiveOr(cfg.MaxBackups, 3),
			Compress:   cfg.Compress,
		}
		closer = lj
		handlers = append(handlers, newHandler(lj, resolveFileFormat(cfg.FileFormat), fileLevel))
	}

	var handler slog.Handler
	switch len(handlers) {
	case 0:
		handler = discardHandler{}
	case 1:
		handler = handlers[0]
	default:
		handler = &multiHandler{handlers: handlers}
	}

	base := slog.New(handler)
	if component := strings.TrimSpace(cfg.Component); component != "" {
		base = base.With("component", component)
	}
	return &Logger{Logger: base, ConsoleLevel: consoleLevel, FileLevel: fileLevel, closer: closer}
}

// ApplyEnv overlays Fujin-style logging knobs from one env prefix.
//
// For prefix TABULA_RUNTIME, supported keys are:
//
//   - TABULA_RUNTIME_LOG_LEVEL
//   - TABULA_RUNTIME_LOG_FORMAT or TABULA_RUNTIME_LOG_TYPE
//   - TABULA_RUNTIME_LOG_OUTPUT
//   - TABULA_RUNTIME_LOG_FILE
//   - TABULA_RUNTIME_FILE_LOG_LEVEL
//   - TABULA_RUNTIME_FILE_LOG_FORMAT or TABULA_RUNTIME_FILE_LOG_TYPE
func ApplyEnv(cfg Config, prefix string) Config {
	prefix = strings.Trim(strings.TrimSpace(prefix), "_")
	if prefix == "" {
		return cfg
	}
	if value := strings.TrimSpace(os.Getenv(prefix + "_LOG_LEVEL")); value != "" {
		cfg.ConsoleLevel = value
	}
	if value := firstEnv(prefix+"_LOG_FORMAT", prefix+"_LOG_TYPE"); value != "" {
		cfg.ConsoleFormat = value
	}
	if value := strings.TrimSpace(os.Getenv(prefix + "_LOG_OUTPUT")); value != "" {
		cfg.ConsoleOutput = value
		cfg.ConsoleWriter = nil
	}
	if value := strings.TrimSpace(os.Getenv(prefix + "_LOG_FILE")); value != "" {
		cfg.FilePath = value
	}
	if value := strings.TrimSpace(os.Getenv(prefix + "_FILE_LOG_LEVEL")); value != "" {
		cfg.FileLevel = value
	}
	if value := firstEnv(prefix+"_FILE_LOG_FORMAT", prefix+"_FILE_LOG_TYPE"); value != "" {
		cfg.FileFormat = value
	}
	return cfg
}

// silentLevel is above slog.LevelError so nothing passes the filter.
const silentLevel = slog.Level(12)

func newLevelVar(s string, fallback slog.Level) *slog.LevelVar {
	level := &slog.LevelVar{}
	level.Set(parseLevel(s, fallback))
	return level
}

func parseLevel(s string, fallback slog.Level) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "silent", "off", "none":
		return silentLevel
	default:
		return fallback
	}
}

func newHandler(w io.Writer, format string, level slog.Leveler) slog.Handler {
	switch format {
	case formatJSON:
		return slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	default:
		return slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	}
}

func resolveConsoleWriter(cfg Config) io.Writer {
	if cfg.ConsoleWriter != nil {
		return cfg.ConsoleWriter
	}
	switch strings.ToLower(strings.TrimSpace(cfg.ConsoleOutput)) {
	case outputStderr:
		return os.Stderr
	case outputDiscard:
		return io.Discard
	default:
		return os.Stdout
	}
}

func resolveConsoleFormat(cfg Config, w io.Writer) string {
	switch strings.ToLower(strings.TrimSpace(cfg.ConsoleFormat)) {
	case formatJSON:
		return formatJSON
	case formatText:
		return formatText
	}
	if f, ok := w.(*os.File); ok && isTTY(f) {
		return formatText
	}
	return formatJSON
}

func resolveFileFormat(format string) string {
	if strings.EqualFold(strings.TrimSpace(format), formatText) {
		return formatText
	}
	return formatJSON
}

func positiveOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

// isTTY reports whether f is a terminal.
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// multiHandler fans out log records to multiple handlers.
type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: hs}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: hs}
}

// discardHandler drops all log records.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (d discardHandler) WithAttrs([]slog.Attr) slog.Handler      { return d }
func (d discardHandler) WithGroup(string) slog.Handler           { return d }

// Package logging provides structured logging setup with dual output
// (console + file) and automatic format detection.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Config controls how logging is initialized.
type Config struct {
	// Console output level (default: "warn"). Set to "silent" to disable.
	ConsoleLevel string
	// File output level (default: "silent" — no file logging).
	FileLevel string
	// Path for log file. Required if FileLevel is not silent.
	FilePath string
	// Max megabytes before rotation (default 10).
	MaxSizeMB int
	// Max days to retain old log files (default 7).
	MaxAgeDays int
	// Max number of old log files to keep (default 3).
	MaxBackups int
	// Compress rotated log files (default true).
	Compress bool
}

// Logger wraps a slog.Logger and an optional closer for the file writer.
type Logger struct {
	*slog.Logger
	closer io.Closer
}

// Close closes the underlying file writer if any.
func (l *Logger) Close() error {
	if l.closer != nil {
		return l.closer.Close()
	}
	return nil
}

// Setup creates a structured logger and sets it as the slog default.
//
// Output strategy:
//   - Console: always stdout. Text format if TTY, JSON if not.
//   - File: JSON with lumberjack rotation, only if FilePath is set and FileLevel != silent.
//   - Both handlers run independently with their own levels.
func Setup(cfg Config) *Logger {
	consoleLevel := parseLevel(cfg.ConsoleLevel, slog.LevelWarn)
	fileLevel := parseLevel(cfg.FileLevel, silentLevel)

	var handlers []slog.Handler
	var closer io.Closer

	// Console handler
	if consoleLevel < silentLevel {
		if isTTY(os.Stdout) {
			handlers = append(handlers, slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: consoleLevel}))
		} else {
			handlers = append(handlers, slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: consoleLevel}))
		}
	}

	// File handler
	if fileLevel < silentLevel && cfg.FilePath != "" {
		maxSize := cfg.MaxSizeMB
		if maxSize <= 0 {
			maxSize = 10
		}
		maxAge := cfg.MaxAgeDays
		if maxAge <= 0 {
			maxAge = 7
		}
		maxBackups := cfg.MaxBackups
		if maxBackups <= 0 {
			maxBackups = 3
		}

		lj := &lumberjack.Logger{
			Filename:   cfg.FilePath,
			MaxSize:    maxSize,
			MaxAge:     maxAge,
			MaxBackups: maxBackups,
			Compress:   cfg.Compress,
		}
		closer = lj
		handlers = append(handlers, slog.NewJSONHandler(lj, &slog.HandlerOptions{Level: fileLevel}))
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

	logger := slog.New(handler)
	slog.SetDefault(logger)

	return &Logger{Logger: logger, closer: closer}
}

// silentLevel is above slog.LevelError so nothing passes the filter.
const silentLevel = slog.Level(12)

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

func (m *multiHandler) Enabled(_ context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(context.Background(), level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			_ = h.Handle(ctx, r)
		}
	}
	return nil
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

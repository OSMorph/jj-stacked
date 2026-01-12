// Package logger provides structured logging using Go 1.21's log/slog.
package logger

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Logger provides structured logging capabilities.
// AIDEV-NOTE: This wraps slog to provide a consistent interface across the application.
// Debug logging is controlled by the debug flag or JJ_STACK_DEBUG env var.
type Logger struct {
	logger   *slog.Logger
	levelVar *slog.LevelVar
}

// Options configures the logger behavior.
type Options struct {
	// Debug enables debug-level logging
	Debug bool
	// Format specifies the log format ("json" or "text")
	Format string
	// Output specifies where to write logs (defaults to stderr)
	Output io.Writer
}

// New creates a new Logger with the given options.
// AIDEV-NOTE: Logs always go to stderr to avoid polluting stdout which is used for normal CLI output.
func New(opts Options) *Logger {
	if opts.Output == nil {
		opts.Output = os.Stderr
	}

	levelVar := &slog.LevelVar{}
	if opts.Debug {
		levelVar.Set(slog.LevelDebug)
	} else {
		levelVar.Set(slog.LevelInfo)
	}

	var handler slog.Handler
	handlerOpts := &slog.HandlerOptions{
		Level: levelVar,
	}

	if strings.ToLower(opts.Format) == "json" {
		handler = slog.NewJSONHandler(opts.Output, handlerOpts)
	} else {
		handler = slog.NewTextHandler(opts.Output, handlerOpts)
	}

	return &Logger{
		logger:   slog.New(handler),
		levelVar: levelVar,
	}
}

// NewFromEnv creates a Logger configured from environment variables.
// Checks JJ_STACK_DEBUG for debug mode and JJ_STACK_LOG_FORMAT for output format.
func NewFromEnv() *Logger {
	debug := os.Getenv("JJ_STACK_DEBUG") != ""
	format := os.Getenv("JJ_STACK_LOG_FORMAT")

	return New(Options{
		Debug:  debug,
		Format: format,
	})
}

// SetDebug dynamically enables or disables debug logging.
func (l *Logger) SetDebug(enabled bool) {
	if enabled {
		l.levelVar.Set(slog.LevelDebug)
	} else {
		l.levelVar.Set(slog.LevelInfo)
	}
}

// Debug logs a debug message with optional key-value pairs.
func (l *Logger) Debug(msg string, args ...any) {
	l.logger.Debug(msg, args...)
}

// Info logs an info message with optional key-value pairs.
func (l *Logger) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)
}

// Warn logs a warning message with optional key-value pairs.
func (l *Logger) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)
}

// Error logs an error message with optional key-value pairs.
func (l *Logger) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)
}

// With returns a new Logger with the given attributes added to all log entries.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{
		logger:   l.logger.With(args...),
		levelVar: l.levelVar,
	}
}

// Slog returns the underlying *slog.Logger for advanced use cases.
func (l *Logger) Slog() *slog.Logger {
	return l.logger
}

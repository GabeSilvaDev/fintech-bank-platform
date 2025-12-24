// ═══════════════════════════════════════════════════════════════════════════
// Package logger - Structured logging using zerolog
// ═══════════════════════════════════════════════════════════════════════════

package logger

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Logger wraps zerolog.Logger with additional functionality
type Logger struct {
	zerolog.Logger
}

// Config holds logger configuration
type Config struct {
	Level      string
	Pretty     bool
	TimeFormat string
	Output     io.Writer
}

// DefaultConfig returns default logger configuration
func DefaultConfig() Config {
	return Config{
		Level:      "info",
		Pretty:     false,
		TimeFormat: time.RFC3339,
		Output:     os.Stdout,
	}
}

// New creates a new logger with the given configuration
func New(cfg Config) *Logger {
	if cfg.Output == nil {
		cfg.Output = os.Stdout
	}

	if cfg.TimeFormat == "" {
		cfg.TimeFormat = time.RFC3339
	}

	zerolog.TimeFieldFormat = cfg.TimeFormat

	var output io.Writer = cfg.Output
	if cfg.Pretty {
		output = zerolog.ConsoleWriter{
			Out:        cfg.Output,
			TimeFormat: cfg.TimeFormat,
		}
	}

	level := parseLevel(cfg.Level)
	zl := zerolog.New(output).Level(level).With().Timestamp().Logger()

	return &Logger{Logger: zl}
}

// NewDefault creates a logger with default configuration
func NewDefault() *Logger {
	return New(DefaultConfig())
}

// NewDevelopment creates a logger for development (pretty output)
func NewDevelopment() *Logger {
	cfg := DefaultConfig()
	cfg.Pretty = true
	cfg.Level = "debug"
	return New(cfg)
}

// NewProduction creates a logger for production (JSON output)
func NewProduction() *Logger {
	cfg := DefaultConfig()
	cfg.Pretty = false
	cfg.Level = "info"
	return New(cfg)
}

// WithField adds a field to the logger
func (l *Logger) WithField(key string, value interface{}) *Logger {
	return &Logger{Logger: l.Logger.With().Interface(key, value).Logger()}
}

// WithFields adds multiple fields to the logger
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	ctx := l.Logger.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}
	return &Logger{Logger: ctx.Logger()}
}

// WithError adds an error to the logger
func (l *Logger) WithError(err error) *Logger {
	return &Logger{Logger: l.Logger.With().Err(err).Logger()}
}

// WithRequestID adds request ID to the logger
func (l *Logger) WithRequestID(requestID string) *Logger {
	return &Logger{Logger: l.Logger.With().Str("request_id", requestID).Logger()}
}

// WithService adds service name to the logger
func (l *Logger) WithService(service string) *Logger {
	return &Logger{Logger: l.Logger.With().Str("service", service).Logger()}
}

// parseLevel converts string level to zerolog.Level
func parseLevel(level string) zerolog.Level {
	switch level {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	case "panic":
		return zerolog.PanicLevel
	case "disabled":
		return zerolog.Disabled
	default:
		return zerolog.InfoLevel
	}
}

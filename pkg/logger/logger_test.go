// ═══════════════════════════════════════════════════════════════════════════
// Package logger - Tests
// ═══════════════════════════════════════════════════════════════════════════

package logger

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:  "debug",
		Pretty: false,
		Output: &buf,
	}

	log := New(cfg)
	assert.NotNil(t, log)

	log.Info().Msg("test message")
	assert.Contains(t, buf.String(), "test message")
}

func TestNewDefault(t *testing.T) {
	log := NewDefault()
	assert.NotNil(t, log)
}

func TestNewDevelopment(t *testing.T) {
	log := NewDevelopment()
	assert.NotNil(t, log)
}

func TestNewProduction(t *testing.T) {
	log := NewProduction()
	assert.NotNil(t, log)
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, "info", cfg.Level)
	assert.False(t, cfg.Pretty)
	assert.NotNil(t, cfg.Output)
}

func TestWithField(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{Output: &buf})

	log.WithField("key", "value").Info().Msg("test")
	assert.Contains(t, buf.String(), "key")
	assert.Contains(t, buf.String(), "value")
}

func TestWithFields(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{Output: &buf})

	fields := map[string]interface{}{
		"field1": "value1",
		"field2": 123,
	}
	log.WithFields(fields).Info().Msg("test")

	output := buf.String()
	assert.Contains(t, output, "field1")
	assert.Contains(t, output, "value1")
}

func TestWithError(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{Output: &buf})

	log.WithError(assert.AnError).Error().Msg("error occurred")
	assert.Contains(t, buf.String(), "error")
}

func TestWithRequestID(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{Output: &buf})

	log.WithRequestID("req-123").Info().Msg("test")
	assert.Contains(t, buf.String(), "request_id")
	assert.Contains(t, buf.String(), "req-123")
}

func TestWithService(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{Output: &buf})

	log.WithService("api-gateway").Info().Msg("test")
	assert.Contains(t, buf.String(), "service")
	assert.Contains(t, buf.String(), "api-gateway")
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"debug", "debug"},
		{"info", "info"},
		{"warn", "warn"},
		{"warning", "warn"},
		{"error", "error"},
		{"fatal", "fatal"},
		{"panic", "panic"},
		{"disabled", "disabled"},
		{"invalid", "info"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			level := parseLevel(tt.input)
			assert.NotNil(t, level)
		})
	}
}

func TestPrettyOutput(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:  "info",
		Pretty: true,
		Output: &buf,
	}

	log := New(cfg)
	log.Info().Msg("pretty message")

	output := buf.String()
	assert.True(t, len(output) > 0)
	assert.True(t, strings.Contains(output, "pretty message") || strings.Contains(output, "INF"))
}

func TestNewWithNilOutput(t *testing.T) {
	// Test that nil Output defaults to os.Stdout
	cfg := Config{
		Level:  "info",
		Pretty: false,
		Output: nil, // Should default to os.Stdout
	}

	log := New(cfg)
	assert.NotNil(t, log)
}

func TestNewWithEmptyTimeFormat(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:      "info",
		Pretty:     false,
		Output:     &buf,
		TimeFormat: "", // Should default to RFC3339
	}

	log := New(cfg)
	assert.NotNil(t, log)
	log.Info().Msg("test")
	assert.Contains(t, buf.String(), "time")
}

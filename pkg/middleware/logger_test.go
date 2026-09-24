package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fintech-bank-platform/pkg/logger"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func captureLog(t *testing.T, status int, body string) map[string]interface{} {
	return captureLogWithContext(t, context.Background(), status, body)
}

func captureLogWithContext(t *testing.T, ctx context.Context, status int, body string) map[string]interface{} {
	var buf bytes.Buffer
	log := logger.New(logger.Config{Level: "debug", Output: &buf})

	handler := Logger(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", nil)
	req.RemoteAddr = "10.0.0.7:5555"
	req = req.WithContext(context.WithValue(ctx, RequestIDKey, "req-1"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	var entry map[string]interface{}
	assert.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	return entry
}

func TestLoggerMiddlewareLogsRequestAtInfo(t *testing.T) {
	entry := captureLog(t, http.StatusCreated, "ok")

	assert.Equal(t, "info", entry["level"])
	assert.Equal(t, "req-1", entry["request_id"])
	assert.Equal(t, "POST", entry["method"])
	assert.Equal(t, "/api/v1/accounts", entry["path"])
	assert.Equal(t, "10.0.0.7:5555", entry["remote_addr"])
	assert.Equal(t, float64(201), entry["status"])
	assert.Equal(t, float64(2), entry["bytes"])
	assert.Contains(t, entry, "duration")
	assert.Equal(t, "request completed", entry["message"])
}

func TestLoggerMiddlewareLogsServerErrorsAtError(t *testing.T) {
	entry := captureLog(t, http.StatusInternalServerError, "")

	assert.Equal(t, "error", entry["level"])
	assert.Equal(t, float64(500), entry["status"])
}

func TestLoggerMiddlewareAddsTraceIDsWhenSpanIsPresent(t *testing.T) {
	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	}))

	entry := captureLogWithContext(t, ctx, http.StatusOK, "")

	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", entry["otel_trace_id"])
	assert.Equal(t, "00f067aa0ba902b7", entry["otel_span_id"])
	assert.Equal(t, "req-1", entry["request_id"])
}

func TestLoggerMiddlewareOmitsTraceIDsWithoutSpan(t *testing.T) {
	entry := captureLog(t, http.StatusOK, "")

	assert.NotContains(t, entry, "otel_trace_id")
	assert.NotContains(t, entry, "otel_span_id")
}

func TestLoggerMiddlewareLogsResolvedClientIP(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Config{Level: "debug", Output: &buf})
	handler := chiMiddleware.ClientIPFromXFFTrustedProxies(1)(Logger(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "10.0.0.7:5555"
	req.Header.Set("X-Forwarded-For", "198.51.100.23")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	var entry map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	assert.Equal(t, "198.51.100.23", entry["client_ip"])
	assert.Equal(t, "10.0.0.7:5555", entry["remote_addr"])
}

func TestLoggerMiddlewareOmitsClientIPWhenUnresolved(t *testing.T) {
	entry := captureLog(t, http.StatusOK, "")

	assert.NotContains(t, entry, "client_ip")
}

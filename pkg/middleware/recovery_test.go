package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func testLogger() (*logger.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return logger.New(logger.Config{Level: "debug", Output: &buf}), &buf
}

func TestRecoveryMiddlewareHandlesPanic(t *testing.T) {
	log, buf := testLogger()
	handler := Recovery(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/1", nil)
	req = req.WithContext(context.WithValue(req.Context(), RequestIDKey, "req-1"))
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	var body response.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.False(t, body.Success)
	require.NotNil(t, body.Error)
	assert.Equal(t, "INTERNAL_ERROR", body.Error.Code)

	logged := buf.String()
	assert.Contains(t, logged, "handler panicked")
	assert.Contains(t, logged, "test panic")
	assert.Contains(t, logged, "req-1")
	assert.Contains(t, logged, "/accounts/1")
	assert.Contains(t, logged, "stack")
}

func TestRecoveryMiddlewareLogsTraceIDsWhenSpanPresent(t *testing.T) {
	log, buf := testLogger()
	handler := Recovery(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	logged := buf.String()
	assert.Contains(t, logged, "4bf92f3577b34da6a3ce929d0e0e4736")
	assert.Contains(t, logged, "00f067aa0ba902b7")
}

func TestRecoveryMiddlewarePassesThroughNormally(t *testing.T) {
	log, _ := testLogger()
	handler := Recovery(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "OK", rec.Body.String())
}

func TestRecoveryMiddlewareHandlesNilPanic(t *testing.T) {
	log, _ := testLogger()
	handler := Recovery(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(nil)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		handler.ServeHTTP(rec, req)
	})
}

func TestRecoveryMiddlewareUsesDefaultLoggerWhenNil(t *testing.T) {
	handler := Recovery(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		handler.ServeHTTP(rec, req)
	})
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestRecoveryMiddlewareDoesNotWriteTwiceWhenHeaderAlreadySent(t *testing.T) {
	log, _ := testLogger()
	handler := Recovery(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		panic("after write")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestRecoveryMiddlewareRepanicsErrAbortHandler(t *testing.T) {
	log, _ := testLogger()
	handler := Recovery(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		handler.ServeHTTP(rec, req)
	})
}

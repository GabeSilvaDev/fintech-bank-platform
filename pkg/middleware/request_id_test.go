package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetRequestIDWithValue(t *testing.T) {
	ctx := context.WithValue(context.Background(), RequestIDKey, "test-request-id")

	result := GetRequestID(ctx)

	assert.Equal(t, "test-request-id", result)
}

func TestGetRequestIDWithoutValue(t *testing.T) {
	ctx := context.Background()

	result := GetRequestID(ctx)

	assert.Empty(t, result)
}

func TestGetRequestIDWithWrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), RequestIDKey, 12345)

	result := GetRequestID(ctx)

	assert.Empty(t, result)
}

func TestRequestIDMiddlewareGeneratesID(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	requestID := rec.Header().Get(RequestIDHeader)
	assert.NotEmpty(t, requestID)
	assert.Regexp(t, `^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`, requestID)
}

func TestRequestIDMiddlewarePreservesProvidedID(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "custom-id-12345")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "custom-id-12345", rec.Header().Get(RequestIDHeader))
}

func TestRequestIDMiddlewareSetsContext(t *testing.T) {
	var capturedRequestID string

	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequestID = GetRequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "context-test-id")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "context-test-id", capturedRequestID)
}

func TestRequestIDMiddlewareReplacesMalformedIDs(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, provided := range []string{
		strings.Repeat("a", 65),
		"has space",
		"semi;colon",
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(RequestIDHeader, provided)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		requestID := rec.Header().Get(RequestIDHeader)
		assert.NotEqual(t, provided, requestID)
		assert.Regexp(t, `^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`, requestID)
	}
}

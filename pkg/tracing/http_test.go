package tracing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const remoteTraceParent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

func attributes(span sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
	values := make(map[attribute.Key]attribute.Value)
	for _, kv := range span.Attributes() {
		values[kv.Key] = kv.Value
	}

	return values
}

func serve(handler http.HandlerFunc, target string, header http.Header) *httptest.ResponseRecorder {
	return serveWith(Middleware, handler, http.MethodGet, target, header)
}

func serveWith(middleware func(http.Handler) http.Handler, handler http.HandlerFunc, method, target string, header http.Header) *httptest.ResponseRecorder {
	router := chi.NewRouter()
	router.Use(middleware)
	router.Get("/accounts/{id}", handler)
	router.Get("/health", handler)
	router.Get("/metrics", handler)

	req := httptest.NewRequest(method, target, nil)
	for key, values := range header {
		req.Header[key] = values
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec
}

func TestMiddlewareNamesSpanAfterRoutePattern(t *testing.T) {
	recorder := useRecorder(t)

	rec := serve(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}, "/accounts/123", nil)

	assert.Equal(t, http.StatusCreated, rec.Code)
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	span := spans[0]
	assert.Equal(t, "GET /accounts/{id}", span.Name())
	assert.Equal(t, trace.SpanKindServer, span.SpanKind())
	assert.Equal(t, codes.Unset, span.Status().Code)

	attrs := attributes(span)
	assert.Equal(t, "GET", attrs["http.request.method"].AsString())
	assert.Equal(t, "/accounts/123", attrs["url.path"].AsString())
	assert.Equal(t, "/accounts/{id}", attrs["http.route"].AsString())
	assert.Equal(t, int64(http.StatusCreated), attrs["http.response.status_code"].AsInt64())
}

func TestMiddlewareMarksServerErrors(t *testing.T) {
	recorder := useRecorder(t)

	serve(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}, "/accounts/123", nil)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Equal(t, int64(http.StatusInternalServerError), attributes(spans[0])["http.response.status_code"].AsInt64())
}

func TestMiddlewareRecordsStatusOKWhenHandlerWritesNothing(t *testing.T) {
	recorder := useRecorder(t)

	serve(func(w http.ResponseWriter, r *http.Request) {}, "/accounts/123", nil)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, int64(http.StatusOK), attributes(spans[0])["http.response.status_code"].AsInt64())
}

func TestMiddlewareKeepsMethodNameWhenNoRouteMatches(t *testing.T) {
	recorder := useRecorder(t)

	rec := serve(func(w http.ResponseWriter, r *http.Request) {}, "/does-not-exist", nil)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "GET", spans[0].Name())
	assert.Equal(t, codes.Unset, spans[0].Status().Code)

	attrs := attributes(spans[0])
	_, hasRoute := attrs["http.route"]
	assert.False(t, hasRoute)
	assert.Equal(t, int64(http.StatusNotFound), attrs["http.response.status_code"].AsInt64())
}

func TestMiddlewareContinuesRemoteParent(t *testing.T) {
	recorder := useRecorder(t)

	var handlerTraceID string
	serve(func(w http.ResponseWriter, r *http.Request) {
		handlerTraceID, _, _ = IDs(r.Context())
	}, "/accounts/123", http.Header{"Traceparent": {remoteTraceParent}})

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	span := spans[0]
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", span.SpanContext().TraceID().String())
	assert.Equal(t, "00f067aa0ba902b7", span.Parent().SpanID().String())
	assert.True(t, span.Parent().IsRemote())
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", handlerTraceID)
}

func TestTransportInjectsTraceParentAndRecordsClientSpan(t *testing.T) {
	recorder := useRecorder(t)

	var gotTraceParent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceParent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, parent := Tracer().Start(context.Background(), "parent")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/accounts/123", nil)
	require.NoError(t, err)

	resp, err := (&http.Client{Transport: Transport(nil)}).Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	parent.End()

	assert.Empty(t, req.Header.Get("traceparent"))
	assert.Contains(t, gotTraceParent, parent.SpanContext().TraceID().String())

	spans := recorder.Ended()
	require.Len(t, spans, 2)
	client := spans[0]
	assert.Equal(t, "GET "+req.URL.Host, client.Name())
	assert.Equal(t, trace.SpanKindClient, client.SpanKind())
	assert.Equal(t, parent.SpanContext().SpanID(), client.Parent().SpanID())
	assert.Contains(t, gotTraceParent, client.SpanContext().SpanID().String())
	assert.Equal(t, codes.Unset, client.Status().Code)

	attrs := attributes(client)
	assert.Equal(t, "GET", attrs["http.request.method"].AsString())
	assert.Equal(t, "/accounts/123", attrs["url.path"].AsString())
	assert.Equal(t, int64(http.StatusOK), attrs["http.response.status_code"].AsInt64())
}

func TestTransportMarksServerErrors(t *testing.T) {
	recorder := useRecorder(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	resp, err := (&http.Client{Transport: Transport(http.DefaultTransport)}).Get(server.URL)
	require.NoError(t, err)
	resp.Body.Close()

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Equal(t, int64(http.StatusServiceUnavailable), attributes(spans[0])["http.response.status_code"].AsInt64())
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestTransportRecordsTransportErrors(t *testing.T) {
	recorder := useRecorder(t)

	boom := errors.New("connection refused")
	base := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, boom
	})

	req := httptest.NewRequest(http.MethodPost, "http://account-service:8081/accounts", nil)
	resp, err := Transport(base).RoundTrip(req)

	assert.Nil(t, resp)
	assert.ErrorIs(t, err, boom)
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "POST account-service:8081", spans[0].Name())
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Equal(t, "connection refused", spans[0].Status().Description)
	require.Len(t, spans[0].Events(), 1)
	assert.Equal(t, "exception", spans[0].Events()[0].Name)
}

func TestMiddlewareCollapsesNonStandardMethods(t *testing.T) {
	recorder := useRecorder(t)

	called := false
	rec := serveWith(Middleware, func(w http.ResponseWriter, r *http.Request) {
		called = true
	}, "FOO", "/accounts/123", nil)

	assert.False(t, called)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.NotContains(t, spans[0].Name(), "FOO")
	assert.Regexp(t, `^HTTP( |$)`, spans[0].Name())

	attrs := attributes(spans[0])
	assert.Equal(t, "_OTHER", attrs["http.request.method"].AsString())
	assert.Equal(t, "FOO", attrs["http.request.method_original"].AsString())
}

func TestMiddlewareKeepsStandardMethodWithoutOriginal(t *testing.T) {
	recorder := useRecorder(t)

	serve(func(w http.ResponseWriter, r *http.Request) {}, "/accounts/123", nil)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	_, hasOriginal := attributes(spans[0])["http.request.method_original"]
	assert.False(t, hasOriginal)
}

func TestTransportCollapsesNonStandardMethods(t *testing.T) {
	recorder := useRecorder(t)

	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
	})

	req := httptest.NewRequest("FOO", "http://account-service:8081/accounts", nil)
	resp, err := Transport(base).RoundTrip(req)
	require.NoError(t, err)
	resp.Body.Close()

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "HTTP account-service:8081", spans[0].Name())
	attrs := attributes(spans[0])
	assert.Equal(t, "_OTHER", attrs["http.request.method"].AsString())
	assert.Equal(t, "FOO", attrs["http.request.method_original"].AsString())
}

func TestMiddlewareSkipsHealthChecksAndScrapes(t *testing.T) {
	for _, middleware := range []func(http.Handler) http.Handler{Middleware} {
		for _, path := range []string{"/health", "/metrics"} {
			recorder := useRecorder(t)

			var traced bool
			rec := serveWith(middleware, func(w http.ResponseWriter, r *http.Request) {
				_, _, traced = IDs(r.Context())
				w.WriteHeader(http.StatusOK)
			}, http.MethodGet, path, http.Header{"Traceparent": {remoteTraceParent}})

			assert.Equal(t, http.StatusOK, rec.Code, path)
			assert.False(t, traced, path)
			assert.Empty(t, recorder.Ended(), path)
		}
	}
}

func TestMiddlewareTracesPathsThatOnlyStartLikeHealth(t *testing.T) {
	recorder := useRecorder(t)

	serveWith(Middleware, func(w http.ResponseWriter, r *http.Request) {}, http.MethodGet, "/healthz", nil)

	assert.Len(t, recorder.Ended(), 1)
}

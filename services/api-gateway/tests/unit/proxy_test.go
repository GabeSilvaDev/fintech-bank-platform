package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/fintech-bank-platform/api-gateway/internal/app/handlers"
	"github.com/fintech-bank-platform/api-gateway/tests"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func proxyRouter(upstream string) http.Handler {
	target, _ := url.Parse(upstream)
	proxy := http.StripPrefix("/api/v1", handlers.NewReadProxy(target))
	r := chi.NewRouter()
	r.Get("/api/v1/accounts/{id}", proxy.ServeHTTP)
	r.Get("/api/v1/users/{user_id}/accounts", proxy.ServeHTTP)
	return r
}

func TestReadProxyForwardsPathAndRequestID(t *testing.T) {
	var gotPath, gotRequestID string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRequestID = r.Header.Get(middleware.RequestIDHeader)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{"account_id":"abc"}}`))
	}))
	defer upstream.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/abc", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.RequestIDKey, "req-9"))
	rec := httptest.NewRecorder()
	proxyRouter(upstream.URL).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/accounts/abc", gotPath)
	assert.Equal(t, "req-9", gotRequestID)
	assert.Equal(t, "abc", tests.FromJson(rec.Body.String())["data"].(map[string]interface{})["account_id"])
}

func TestReadProxyKeepsASingleRequestIDHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(middleware.RequestIDHeader, r.Header.Get(middleware.RequestIDHeader))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
	}))
	defer upstream.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/abc", nil)
	req.Header.Set(middleware.RequestIDHeader, "req-9")
	rec := httptest.NewRecorder()
	middleware.RequestID(proxyRouter(upstream.URL)).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"req-9"}, rec.Header().Values(middleware.RequestIDHeader))
}

func TestReadProxyPassesUpstreamStatusThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"ACCOUNT_NOT_FOUND"}}`))
	}))
	defer upstream.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/u1/accounts", nil)
	rec := httptest.NewRecorder()
	proxyRouter(upstream.URL).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "ACCOUNT_NOT_FOUND")
}

func TestReadProxyAnswers502WhenUpstreamIsDown(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	upstream.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/abc", nil)
	rec := httptest.NewRecorder()
	proxyRouter(upstream.URL).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Equal(t, "UPSTREAM_UNAVAILABLE", tests.FromJson(rec.Body.String())["error"].(map[string]interface{})["code"])
}

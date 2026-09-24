package unit

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/fintech-bank-platform/api-gateway/internal/app/handlers"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/auth"
	"github.com/fintech-bank-platform/api-gateway/tests"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func actingAs(ctx context.Context, userID uuid.UUID) context.Context {
	return auth.WithUserID(ctx, userID)
}

func assertForbiddenError(t *testing.T, err error) {
	t.Helper()
	appErr := assertAppError(t, err, http.StatusForbidden, "FORBIDDEN")
	assert.Equal(t, "access to this resource is not allowed", appErr.Message)
}

func TestAuthorizeAccountAllowsOnlyTheOwner(t *testing.T) {
	userID := uuid.New()
	owners := tests.NewFakeOwners()
	own := owners.Own(tests.UUID(), userID)
	foreign := owners.Own(tests.UUID(), uuid.New())
	guard := handlers.NewAccessGuard(owners)
	ctx := actingAs(context.Background(), userID)

	assert.NoError(t, guard.AuthorizeAccount(ctx, own))
	assertForbiddenError(t, guard.AuthorizeAccount(ctx, foreign))
	assertForbiddenError(t, guard.AuthorizeAccount(ctx, "not-a-uuid"))
	assertForbiddenError(t, guard.AuthorizeAccount(context.Background(), own))

	appErr := assertAppError(t, guard.AuthorizeAccount(ctx, tests.UUID()), http.StatusNotFound, "ACCOUNT_NOT_FOUND")
	assert.Equal(t, "account not found", appErr.Message)

	owners.Err = apperrors.New("UPSTREAM_UNAVAILABLE", "account service is unavailable", http.StatusBadGateway)
	assertAppError(t, guard.AuthorizeAccount(ctx, own), http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
}

func TestResolveUserDefaultsToTheCaller(t *testing.T) {
	userID := uuid.New()
	guard := handlers.NewAccessGuard(tests.NewFakeOwners())
	ctx := actingAs(context.Background(), userID)

	resolved, err := guard.ResolveUser(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, userID.String(), resolved)

	resolved, err = guard.ResolveUser(ctx, strings.ToUpper(userID.String()))
	require.NoError(t, err)
	assert.Equal(t, userID.String(), resolved)

	_, err = guard.ResolveUser(ctx, tests.UUID())
	assertForbiddenError(t, err)

	_, err = guard.ResolveUser(ctx, "nope")
	assertForbiddenError(t, err)

	_, err = guard.ResolveUser(context.Background(), "")
	assertForbiddenError(t, err)
}

func guardedRouter(guard *handlers.AccessGuard, userID *uuid.UUID) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if userID != nil {
				req = req.WithContext(actingAs(req.Context(), *userID))
			}
			next.ServeHTTP(w, req)
		})
	})
	reached := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.With(guard.RequireSelf("user_id")).Get("/users/{user_id}", reached)
	r.With(guard.RequireAccountOwner("id")).Get("/accounts/{id}", reached)
	r.With(guard.RequireAccountOwner("account_id")).Get("/accounts/{account_id}/items", reached)
	return r
}

func serve(handler http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestRequireSelfCompareTheParsedUserIDs(t *testing.T) {
	userID := uuid.New()
	router := guardedRouter(handlers.NewAccessGuard(tests.NewFakeOwners()), &userID)

	assert.Equal(t, http.StatusNoContent, serve(router, "/users/"+userID.String()).Code)
	assert.Equal(t, http.StatusNoContent, serve(router, "/users/"+strings.ToUpper(userID.String())).Code)
	assert.Equal(t, http.StatusForbidden, serve(router, "/users/"+tests.UUID()).Code)
	assert.Equal(t, http.StatusForbidden, serve(router, "/users/nope").Code)
	assert.Equal(t, "FORBIDDEN", errorCode(tests.FromJson(serve(router, "/users/nope").Body.String())))
}

func TestRequireSelfNeedsAnAuthenticatedCaller(t *testing.T) {
	router := guardedRouter(handlers.NewAccessGuard(tests.NewFakeOwners()), nil)

	assert.Equal(t, http.StatusForbidden, serve(router, "/users/"+tests.UUID()).Code)
}

func assertInvalidAccountID(t *testing.T, rec *httptest.ResponseRecorder, param string) {
	t.Helper()
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	errorBody := tests.FromJson(rec.Body.String())["error"].(map[string]interface{})
	assert.Equal(t, "VALIDATION_ERROR", errorBody["code"])
	assert.Equal(t, "request validation failed", errorBody["message"])
	assert.Equal(t, map[string]interface{}{param: "uuid"}, errorBody["details"])
}

func TestRequireAccountOwnerRefusesUnknownAccountsAndInvalidIDs(t *testing.T) {
	userID := uuid.New()
	owners := tests.NewFakeOwners()
	own := owners.Own(tests.UUID(), userID)
	foreign := owners.Own(tests.UUID(), uuid.New())
	router := guardedRouter(handlers.NewAccessGuard(owners), &userID)
	encode := func(accountID string) string { return "%" + fmt.Sprintf("%x", accountID[0]) + accountID[1:] }

	assert.Equal(t, http.StatusNoContent, serve(router, "/accounts/"+own).Code)
	assert.Equal(t, http.StatusNoContent, serve(router, "/accounts/"+strings.ToUpper(own)).Code)
	assert.Equal(t, http.StatusForbidden, serve(router, "/accounts/"+foreign).Code)
	assert.Equal(t, http.StatusForbidden, serve(router, "/accounts/"+encode(foreign)).Code)
	unknown := serve(router, "/accounts/"+tests.UUID())
	assert.Equal(t, http.StatusNotFound, unknown.Code)
	assert.Equal(t, "ACCOUNT_NOT_FOUND", errorCode(tests.FromJson(unknown.Body.String())))
	assert.Len(t, owners.Lookups, 5)

	assertInvalidAccountID(t, serve(router, "/accounts/"+encode(own)), "id")
	assert.Len(t, owners.Lookups, 6)

	for _, invalid := range []string{"not-a-uuid", "not%2Da-uuid", "%25" + own} {
		assertInvalidAccountID(t, serve(router, "/accounts/"+invalid), "id")
	}
	undecodable := httptest.NewRequest(http.MethodGet, "/accounts/x", nil)
	undecodable.URL.RawPath = "/accounts/%zz"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, undecodable)
	assertInvalidAccountID(t, rec, "id")
	assert.Len(t, owners.Lookups, 6)

	owners.Err = apperrors.New("UPSTREAM_UNAVAILABLE", "account service is unavailable", http.StatusBadGateway)
	assert.Equal(t, http.StatusBadGateway, serve(router, "/accounts/"+own).Code)
}

func TestRequireAccountOwnerNamesTheRouteParameter(t *testing.T) {
	userID := uuid.New()
	owners := tests.NewFakeOwners()
	router := guardedRouter(handlers.NewAccessGuard(owners), &userID)

	assertInvalidAccountID(t, serve(router, "/accounts/nope/items"), "account_id")
	assert.Equal(t, http.StatusNoContent, serve(router, "/accounts/"+owners.Own(tests.UUID(), userID)+"/items").Code)
	assert.Empty(t, owners.Lookups[1:])
}

func TestRequireAccountOwnerNeedsAnAuthenticatedCaller(t *testing.T) {
	owners := tests.NewFakeOwners()
	router := guardedRouter(handlers.NewAccessGuard(owners), nil)

	assert.Equal(t, http.StatusForbidden, serve(router, "/accounts/"+owners.Own(tests.UUID(), uuid.New())).Code)
	assert.Empty(t, owners.Lookups)
}

type guardedUpstream struct {
	status  int
	body    string
	headers http.Header
	handler http.HandlerFunc
}

func guardedReadRouter(t *testing.T, upstream *guardedUpstream, owners *tests.FakeOwners, userID *uuid.UUID) http.Handler {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.headers = r.Header.Clone()
		if upstream.handler != nil {
			upstream.handler(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(middleware.RequestIDHeader, "upstream-id")
		w.WriteHeader(upstream.status)
		_, _ = w.Write([]byte(upstream.body))
	}))
	t.Cleanup(server.Close)
	return guardedReadRouterFor(server.URL, owners, userID)
}

func guardedReadRouterFor(upstream string, owners *tests.FakeOwners, userID *uuid.UUID) http.Handler {
	target, _ := url.Parse(upstream)
	guard := handlers.NewAccessGuard(owners)
	proxy := http.StripPrefix("/api/v1", handlers.NewGuardedReadProxy(target, "transaction service", guard, "account_id", "counterparty_id"))
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if userID != nil {
				req = req.WithContext(actingAs(req.Context(), *userID))
			}
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/api/v1/transactions/{id}", proxy.ServeHTTP)
	return r
}

func guardedGet(router http.Handler) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions/t1", nil)
	req.Header.Set(middleware.RequestIDHeader, "req-guarded")
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("Accept-Encoding", "br")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestGuardedReadReturnsTheUpstreamBodyToTheOwner(t *testing.T) {
	userID := uuid.New()
	owners := tests.NewFakeOwners()
	own := owners.Own(tests.UUID(), userID)
	body := `{"success":true,"data":{"transaction_id":"t1","account_id":"` + own + `"}}`
	upstream := &guardedUpstream{status: http.StatusOK, body: body}

	rec := guardedGet(guardedReadRouter(t, upstream, owners, &userID))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, body, rec.Body.String())
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, []string{"req-guarded"}, rec.Header().Values(middleware.RequestIDHeader))
	assert.Equal(t, "req-guarded", upstream.headers.Get(middleware.RequestIDHeader))
	assert.Empty(t, upstream.headers.Values("Authorization"))
	assert.NotContains(t, upstream.headers.Get("Accept-Encoding"), "br")
}

func TestGuardedReadReturnsTheBodyToTheCounterparty(t *testing.T) {
	userID := uuid.New()
	owners := tests.NewFakeOwners()
	own := owners.Own(tests.UUID(), userID)
	body := `{"success":true,"data":{"account_id":"` + tests.UUID() + `","counterparty_id":"` + own + `"}}`

	rec := guardedGet(guardedReadRouter(t, &guardedUpstream{status: http.StatusOK, body: body}, owners, &userID))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, body, rec.Body.String())
	assert.Len(t, owners.Lookups, 2)
}

func TestGuardedReadForbidsEveryoneElse(t *testing.T) {
	userID := uuid.New()
	owners := tests.NewFakeOwners()
	foreign := owners.Own(tests.UUID(), uuid.New())
	bodies := []string{
		`{"success":true,"data":{"account_id":"` + foreign + `","counterparty_id":"` + tests.UUID() + `"}}`,
		`{"success":true,"data":{"account_id":"not-a-uuid","counterparty_id":7}}`,
		`{"success":true}`,
	}

	for _, body := range bodies {
		rec := guardedGet(guardedReadRouter(t, &guardedUpstream{status: http.StatusOK, body: body}, owners, &userID))

		assert.Equal(t, http.StatusForbidden, rec.Code, body)
		assert.Equal(t, "FORBIDDEN", errorCode(tests.FromJson(rec.Body.String())))
		assert.NotContains(t, rec.Body.String(), foreign)
		assert.Equal(t, []string{"req-guarded"}, rec.Header().Values(middleware.RequestIDHeader))
	}
}

func TestGuardedReadNeedsAnAuthenticatedCaller(t *testing.T) {
	owners := tests.NewFakeOwners()
	body := `{"success":true,"data":{"account_id":"` + owners.Own(tests.UUID(), uuid.New()) + `"}}`

	rec := guardedGet(guardedReadRouter(t, &guardedUpstream{status: http.StatusOK, body: body}, owners, nil))

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, owners.Lookups)
}

func TestGuardedReadPassesOtherUpstreamAnswersThrough(t *testing.T) {
	userID := uuid.New()
	owners := tests.NewFakeOwners()
	upstream := &guardedUpstream{status: http.StatusNotFound, body: `{"success":false,"error":{"code":"TRANSACTION_NOT_FOUND","message":"transaction not found"}}`}

	rec := guardedGet(guardedReadRouter(t, upstream, owners, &userID))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, upstream.body, rec.Body.String())
	assert.Empty(t, owners.Lookups)
}

func TestGuardedReadReportsOwnerLookupFailures(t *testing.T) {
	userID := uuid.New()
	owners := tests.NewFakeOwners()
	owners.Err = apperrors.New("UPSTREAM_UNAVAILABLE", "account service is unavailable", http.StatusBadGateway)
	body := `{"success":true,"data":{"account_id":"` + tests.UUID() + `"}}`

	rec := guardedGet(guardedReadRouter(t, &guardedUpstream{status: http.StatusOK, body: body}, owners, &userID))

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Equal(t, "account service is unavailable", tests.FromJson(rec.Body.String())["error"].(map[string]interface{})["message"])
}

func TestGuardedReadReportsUnusableUpstreamBodies(t *testing.T) {
	userID := uuid.New()
	upstreams := map[string]*guardedUpstream{
		"invalid json": {status: http.StatusOK, body: `{"success":`},
		"too large":    {status: http.StatusOK, body: `{"success":true,"data":{"note":"` + strings.Repeat("a", 1<<20) + `"}}`},
		"truncated": {handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "100")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true`))
		}},
	}

	for name, upstream := range upstreams {
		t.Run(name, func(t *testing.T) {
			rec := guardedGet(guardedReadRouter(t, upstream, tests.NewFakeOwners(), &userID))

			assert.Equal(t, http.StatusBadGateway, rec.Code)
			errorBody := tests.FromJson(rec.Body.String())["error"].(map[string]interface{})
			assert.Equal(t, "UPSTREAM_UNAVAILABLE", errorBody["code"])
			assert.Equal(t, "transaction service is unavailable", errorBody["message"])
		})
	}
}

func TestGuardedReadAnswers502WhenTheServiceIsDown(t *testing.T) {
	userID := uuid.New()
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()

	rec := guardedGet(guardedReadRouterFor(server.URL, tests.NewFakeOwners(), &userID))

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Equal(t, "transaction service is unavailable", tests.FromJson(rec.Body.String())["error"].(map[string]interface{})["message"])
}

func TestGuardedReadInspectsEverySuccessfulAnswer(t *testing.T) {
	userID := uuid.New()
	owners := tests.NewFakeOwners()
	own := owners.Own(tests.UUID(), userID)
	foreign := owners.Own(tests.UUID(), uuid.New())
	ownBody := `{"success":true,"data":{"account_id":"` + own + `"}}`

	rec := guardedGet(guardedReadRouter(t, &guardedUpstream{status: http.StatusNonAuthoritativeInfo, body: ownBody}, owners, &userID))
	assert.Equal(t, http.StatusNonAuthoritativeInfo, rec.Code)
	assert.Equal(t, ownBody, rec.Body.String())

	for _, status := range []int{http.StatusCreated, http.StatusAccepted, http.StatusPartialContent, 299} {
		body := `{"success":true,"data":{"account_id":"` + foreign + `"}}`
		rec := guardedGet(guardedReadRouter(t, &guardedUpstream{status: status, body: body}, owners, &userID))

		assert.Equal(t, http.StatusForbidden, rec.Code, status)
		assert.NotContains(t, rec.Body.String(), foreign)
	}

	redirect := guardedGet(guardedReadRouter(t, &guardedUpstream{status: http.StatusMultipleChoices, body: `{"success":false}`}, owners, &userID))
	assert.Equal(t, http.StatusMultipleChoices, redirect.Code)
}

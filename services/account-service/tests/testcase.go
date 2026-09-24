package tests

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/handlers"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/account-service/internal/contracts"
	appHttp "github.com/fintech-bank-platform/account-service/internal/infrastructure/http"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/suite"
)

type TestCase struct {
	suite.Suite
	Router     *chi.Mux
	Accounts   *FakeAccountRepo
	Customers  *FakeCustomerRepo
	Operations *FakeOperationRepo
	Service    *services.AccountService
	Identities *FakeIdentityRepo
	Hasher     *FakeHasher
	Failures   *FakeLoginFailureRepo
	Verifier   *services.IdentityService
	Tokens     *FakeRefreshTokenRepo
	Clock      *FakeClock
	PingErr    error
	headers    map[string]string
}

func (tc *TestCase) SetupTest() {
	tc.Accounts = NewFakeAccountRepo()
	tc.Customers = NewFakeCustomerRepo()
	tc.Operations = NewFakeOperationRepo()
	tc.Service = services.NewAccountService(tc.Accounts, tc.Customers, tc.Operations, FakeClock{T: time.Now().UTC()}, func() string { return "00000001" })
	tc.Identities = NewFakeIdentityRepo()
	tc.Hasher = &FakeHasher{}
	tc.Failures = NewFakeLoginFailureRepo()
	tc.Clock = &FakeClock{T: time.Now().UTC()}
	identityService, err := services.NewIdentityService(tc.Identities, tc.Failures, tc.Hasher, tc.Clock, contracts.LockoutConfig{})
	tc.Require().NoError(err)
	tc.Verifier = identityService
	tc.Tokens = NewFakeRefreshTokenRepo()
	tc.PingErr = nil
	tc.headers = map[string]string{}

	tc.Router = chi.NewRouter()
	appHttp.SetupRouter(tc.Router, appHttp.Dependencies{
		Reads:      handlers.NewReadHandler(tc.Service),
		Identities: handlers.NewIdentityHandler(identityService),
		Sessions:   handlers.NewSessionHandler(services.NewSessionService(tc.Tokens, tc.Clock, contracts.SessionConfig{RefreshTokenTTL: time.Hour})),
		Ping:       func(context.Context) error { return tc.PingErr },
		Logger:     logger.New(logger.Config{Output: io.Discard}),
	})
}

func (tc *TestCase) WithHeader(key, value string) *TestCase {
	tc.headers[key] = value
	return tc
}

func (tc *TestCase) Get(uri string) *TestResponse {
	req := httptest.NewRequest(http.MethodGet, uri, nil)
	for key, value := range tc.headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)
	return newTestResponse(tc.T(), rec)
}

func (tc *TestCase) Post(uri, body string) *TestResponse {
	req := httptest.NewRequest(http.MethodPost, uri, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for key, value := range tc.headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)
	return newTestResponse(tc.T(), rec)
}

package tests

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/handlers"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
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
	PingErr    error
	headers    map[string]string
}

func (tc *TestCase) SetupTest() {
	tc.Accounts = NewFakeAccountRepo()
	tc.Customers = NewFakeCustomerRepo()
	tc.Operations = NewFakeOperationRepo()
	tc.Service = services.NewAccountService(tc.Accounts, tc.Customers, tc.Operations, FakeClock{T: time.Now().UTC()}, func() string { return "00000001" })
	tc.PingErr = nil
	tc.headers = map[string]string{}

	tc.Router = chi.NewRouter()
	appHttp.SetupRouter(tc.Router, appHttp.Dependencies{
		Reads:  handlers.NewReadHandler(tc.Service),
		Ping:   func(context.Context) error { return tc.PingErr },
		Logger: logger.New(logger.Config{Output: io.Discard}),
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

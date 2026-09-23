package tests

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/fintech-bank-platform/notification-service/internal/app/handlers"
	appHttp "github.com/fintech-bank-platform/notification-service/internal/infrastructure/http"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/suite"
)

type TestCase struct {
	suite.Suite
	Router  *chi.Mux
	History *FakeHistory
	PingErr error
	headers map[string]string
}

func (tc *TestCase) SetupTest() {
	tc.History = &FakeHistory{}
	tc.PingErr = nil
	tc.headers = map[string]string{}

	log := logger.New(logger.Config{Output: io.Discard})
	tc.Router = chi.NewRouter()
	appHttp.SetupRouter(tc.Router, appHttp.Dependencies{
		History: handlers.NewHistoryHandler(tc.History),
		Ping:    func(context.Context) error { return tc.PingErr },
		Logger:  log,
	})
}

func (tc *TestCase) WithHeader(key, value string) *TestCase {
	tc.headers[key] = value
	return tc
}

func (tc *TestCase) Get(uri string) *TestResponse {
	return tc.serve(httptest.NewRequest(http.MethodGet, uri, nil))
}

func (tc *TestCase) serve(req *http.Request) *TestResponse {
	for key, value := range tc.headers {
		req.Header.Set(key, value)
	}
	tc.headers = map[string]string{}
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)
	return newTestResponse(tc.T(), rec)
}

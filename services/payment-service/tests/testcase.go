package tests

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/handlers"
	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	appHttp "github.com/fintech-bank-platform/payment-service/internal/infrastructure/http"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

const WebhookSecret = "s3cret"

type TestCase struct {
	suite.Suite
	Router    *chi.Mux
	Repo      *FakePaymentRepo
	Gateway   *FakeGateway
	Publisher *FakePublisher
	Service   *services.PaymentService
	Clock     FakeClock
	PingErr   error
	headers   map[string]string
}

func (tc *TestCase) SetupTest() {
	tc.Repo = NewFakePaymentRepo()
	tc.Gateway = &FakeGateway{}
	tc.Publisher = &FakePublisher{}
	tc.Clock = FakeClock{T: time.Now().UTC()}
	tc.Service = services.NewPaymentService(tc.Repo, tc.Gateway, tc.Clock, uuid.New)
	tc.PingErr = nil
	tc.headers = map[string]string{}

	log := logger.New(logger.Config{Output: io.Discard})
	tc.Router = chi.NewRouter()
	appHttp.SetupRouter(tc.Router, appHttp.Dependencies{
		Reads:    handlers.NewReadHandler(tc.Service),
		Webhooks: handlers.NewWebhookHandler(tc.Publisher, WebhookSecret, 5*time.Minute, tc.Clock, log),
		Ping:     func(context.Context) error { return tc.PingErr },
		Logger:   log,
	})
}

func (tc *TestCase) WithHeader(key, value string) *TestCase {
	tc.headers[key] = value
	return tc
}

func (tc *TestCase) Get(uri string) *TestResponse {
	return tc.serve(httptest.NewRequest(http.MethodGet, uri, nil))
}

func (tc *TestCase) PostRaw(uri string, body []byte) *TestResponse {
	return tc.serve(httptest.NewRequest(http.MethodPost, uri, bytes.NewReader(body)))
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

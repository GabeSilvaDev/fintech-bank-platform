package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	"github.com/fintech-bank-platform/payment-service/internal/infrastructure/gateway"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type callback struct {
	body   []byte
	header http.Header
}

type provider struct {
	server   *httptest.Server
	mu       sync.Mutex
	calls    []callback
	statuses []int
}

func newProvider(t *testing.T, statuses ...int) *provider {
	p := &provider{statuses: statuses}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		status := http.StatusOK
		if len(p.calls) < len(p.statuses) {
			status = p.statuses[len(p.calls)]
		}
		p.calls = append(p.calls, callback{body: body, header: r.Header.Clone()})
		p.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(p.server.Close)
	return p
}

type scheduler struct {
	delays []time.Duration
	jobs   []func()
}

func (s *scheduler) schedule(d time.Duration, job func()) {
	s.delays = append(s.delays, d)
	s.jobs = append(s.jobs, job)
}

func simulator(url string, attempts int, logs io.Writer) (*gateway.Simulator, *scheduler) {
	sched := &scheduler{}
	sim := gateway.NewSimulator(gateway.Config{WebhookURL: url, Secret: "s3cret", Delay: 2 * time.Second, Attempts: attempts, RetryDelay: time.Millisecond, Schedule: sched.schedule}, logger.New(logger.Config{Output: logs}))
	return sim, sched
}

func sandboxPayment(method models.Method, mutate func(*models.Payment)) *models.Payment {
	payment := &models.Payment{ID: uuid.New(), AccountID: uuid.New(), Method: method, Status: models.StatusDebited, AmountCents: 1000, Currency: "BRL", Recipient: "Ana"}
	switch method {
	case models.MethodPix:
		payment.PixKey = "ana@example.com"
	case models.MethodTED:
		payment.TED = &models.TEDDetails{BankCode: "341", Branch: "0001", Account: "123456", Document: "52998224725"}
	case models.MethodBoleto:
		payment.BoletoCode = "34191790010100000012334567812309811000000015000"
	}
	if mutate != nil {
		mutate(payment)
	}
	return payment
}

func TestSimulatorSettlesPixImmediately(t *testing.T) {
	sim, sched := simulator("http://unused", 1, io.Discard)
	payment := sandboxPayment(models.MethodPix, nil)

	submission, err := sim.Submit(context.Background(), payment)

	assert.NoError(t, err)
	assert.Equal(t, models.SubmissionSettled, submission.Status)
	assert.Equal(t, "pix_"+payment.ID.String(), submission.ExternalID)
	assert.Empty(t, sched.jobs)
}

func TestSimulatorRejectsSandboxPixKeys(t *testing.T) {
	sim, sched := simulator("http://unused", 1, io.Discard)

	submission, err := sim.Submit(context.Background(), sandboxPayment(models.MethodPix, func(p *models.Payment) { p.PixKey = "Reject@Reject.Test" }))

	assert.NoError(t, err)
	assert.Equal(t, models.Submission{Status: models.SubmissionRejected, Reason: "pix_key_not_found"}, submission)
	assert.Empty(t, sched.jobs)
}

func TestSimulatorIsIdempotentPerPayment(t *testing.T) {
	prov := newProvider(t)
	sim, sched := simulator(prov.server.URL, 1, io.Discard)
	payment := sandboxPayment(models.MethodTED, nil)

	first, _ := sim.Submit(context.Background(), payment)
	second, _ := sim.Submit(context.Background(), payment)

	assert.Equal(t, first, second)
	assert.Equal(t, "ted_"+payment.ID.String(), first.ExternalID)
	assert.Len(t, sched.jobs, 2)
	sched.jobs[1]()
	assert.Len(t, prov.calls, 1)
	var body map[string]string
	assert.NoError(t, json.Unmarshal(prov.calls[0].body, &body))
	assert.Equal(t, map[string]string{"external_id": first.ExternalID, "status": "settled", "reason": ""}, body)

	restarted, _ := simulator("http://unused", 1, io.Discard)
	again, _ := restarted.Submit(context.Background(), payment)
	assert.Equal(t, first, again)

	pix, pixSched := simulator("http://unused", 1, io.Discard)
	settled := sandboxPayment(models.MethodPix, nil)
	firstPix, _ := pix.Submit(context.Background(), settled)
	secondPix, _ := pix.Submit(context.Background(), settled)
	assert.Equal(t, firstPix, secondPix)
	assert.Empty(t, pixSched.jobs)
}

func TestSimulatorDeliversSignedCallbacks(t *testing.T) {
	cases := []struct {
		payment *models.Payment
		prefix  string
		status  string
		reason  string
	}{
		{sandboxPayment(models.MethodTED, nil), "ted_", "settled", ""},
		{sandboxPayment(models.MethodTED, func(p *models.Payment) { p.TED.BankCode = "999" }), "ted_", "rejected", "invalid_destination"},
		{sandboxPayment(models.MethodBoleto, nil), "boleto_", "settled", ""},
		{sandboxPayment(models.MethodBoleto, func(p *models.Payment) { p.BoletoCode = "99990000040000000000000000000018111000000005000" }), "boleto_", "rejected", "boleto_not_found"},
	}
	for _, c := range cases {
		prov := newProvider(t)
		sim, sched := simulator(prov.server.URL, 1, io.Discard)

		submission, err := sim.Submit(context.Background(), c.payment)
		assert.NoError(t, err)
		assert.Equal(t, models.SubmissionPending, submission.Status)
		assert.Equal(t, c.prefix+c.payment.ID.String(), submission.ExternalID)
		assert.Equal(t, []time.Duration{2 * time.Second}, sched.delays)

		sched.jobs[0]()

		assert.Len(t, prov.calls, 1)
		call := prov.calls[0]
		assert.Equal(t, "application/json", call.header.Get("Content-Type"))
		assert.NoError(t, services.Verify("s3cret", call.header.Get("X-Timestamp"), call.body, call.header.Get("X-Signature"), time.Now(), time.Minute))
		var body map[string]string
		assert.NoError(t, json.Unmarshal(call.body, &body))
		assert.Equal(t, map[string]string{"external_id": submission.ExternalID, "status": c.status, "reason": c.reason}, body)
	}
}

func TestSimulatorRetriesCallbacks(t *testing.T) {
	prov := newProvider(t, http.StatusInternalServerError, http.StatusBadGateway)
	logs := &bytes.Buffer{}
	sim, sched := simulator(prov.server.URL, 3, logs)
	_, _ = sim.Submit(context.Background(), sandboxPayment(models.MethodTED, nil))

	sched.jobs[0]()

	assert.Len(t, prov.calls, 3)
	assert.Contains(t, logs.String(), "webhook delivery attempt failed")
	assert.NotContains(t, logs.String(), "webhook delivery failed\"")

	prov = newProvider(t, http.StatusInternalServerError, http.StatusInternalServerError)
	logs.Reset()
	sim, sched = simulator(prov.server.URL, 2, logs)
	_, _ = sim.Submit(context.Background(), sandboxPayment(models.MethodBoleto, nil))
	sched.jobs[0]()
	assert.Len(t, prov.calls, 2)
	assert.Contains(t, logs.String(), "webhook delivery failed")

	closed := newProvider(t)
	closed.server.Close()
	logs.Reset()
	sim, sched = simulator(closed.server.URL, 1, logs)
	_, _ = sim.Submit(context.Background(), sandboxPayment(models.MethodTED, nil))
	sched.jobs[0]()
	assert.Contains(t, logs.String(), "webhook delivery failed")

	logs.Reset()
	sim, sched = simulator("://bad-url", 1, logs)
	_, _ = sim.Submit(context.Background(), sandboxPayment(models.MethodTED, nil))
	sched.jobs[0]()
	assert.Contains(t, logs.String(), "webhook delivery failed")
}

func TestSimulatorDefaults(t *testing.T) {
	prov := newProvider(t)
	sim := gateway.NewSimulator(gateway.Config{WebhookURL: prov.server.URL, Secret: "s3cret", Delay: time.Millisecond}, logger.New(logger.Config{Output: io.Discard}))

	_, err := sim.Submit(context.Background(), sandboxPayment(models.MethodTED, nil))
	assert.NoError(t, err)
	assert.Eventually(t, func() bool {
		prov.mu.Lock()
		defer prov.mu.Unlock()
		return len(prov.calls) == 1
	}, 2*time.Second, 10*time.Millisecond)
}

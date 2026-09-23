package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/google/uuid"
)

const (
	rejectedPixDomain = "@reject.test"
	rejectedBank      = "999"
)

type Config struct {
	WebhookURL string
	Secret     string
	Delay      time.Duration
	Attempts   int
	RetryDelay time.Duration
	Client     *http.Client
	Schedule   func(time.Duration, func())
}

type Simulator struct {
	cfg         Config
	log         *logger.Logger
	mu          sync.Mutex
	submissions map[uuid.UUID]models.Submission
}

func NewSimulator(cfg Config, log *logger.Logger) *Simulator {
	if cfg.Attempts <= 0 {
		cfg.Attempts = 3
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = time.Second
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 5 * time.Second}
	}
	if cfg.Schedule == nil {
		cfg.Schedule = func(delay time.Duration, job func()) { time.AfterFunc(delay, job) }
	}
	return &Simulator{cfg: cfg, log: log, submissions: map[uuid.UUID]models.Submission{}}
}

func (s *Simulator) Submit(_ context.Context, payment *models.Payment) (models.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	submission, ok := s.submissions[payment.ID]
	if !ok {
		submission = decide(payment)
		s.submissions[payment.ID] = submission
	}
	if submission.Status == models.SubmissionPending {
		status, reason := settlement(payment)
		externalID := submission.ExternalID
		s.cfg.Schedule(s.cfg.Delay, func() { s.deliver(externalID, status, reason) })
	}
	return submission, nil
}

func decide(payment *models.Payment) models.Submission {
	switch payment.Method {
	case models.MethodPix:
		if strings.HasSuffix(strings.ToLower(payment.PixKey), rejectedPixDomain) {
			return models.Submission{Status: models.SubmissionRejected, Reason: "pix_key_not_found"}
		}
		return models.Submission{ExternalID: "pix_" + payment.ID.String(), Status: models.SubmissionSettled}
	case models.MethodTED:
		return models.Submission{ExternalID: "ted_" + payment.ID.String(), Status: models.SubmissionPending}
	}
	return models.Submission{ExternalID: "boleto_" + payment.ID.String(), Status: models.SubmissionPending}
}

func settlement(payment *models.Payment) (models.SubmissionStatus, string) {
	if payment.Method == models.MethodTED && payment.TED != nil && payment.TED.BankCode == rejectedBank {
		return models.SubmissionRejected, "invalid_destination"
	}
	if payment.Method == models.MethodBoleto && strings.HasPrefix(payment.BoletoCode, rejectedBank) {
		return models.SubmissionRejected, "boleto_not_found"
	}
	return models.SubmissionSettled, ""
}

func (s *Simulator) deliver(externalID string, status models.SubmissionStatus, reason string) {
	body, _ := json.Marshal(map[string]string{"external_id": externalID, "status": string(status), "reason": reason})
	for attempt := 1; attempt <= s.cfg.Attempts; attempt++ {
		err := s.post(body)
		if err == nil {
			return
		}
		s.log.Warn().Err(err).Str("external_id", externalID).Int("attempt", attempt).Msg("webhook delivery attempt failed")
		if attempt < s.cfg.Attempts {
			time.Sleep(s.cfg.RetryDelay)
		}
	}
	s.log.Error().Str("external_id", externalID).Msg("webhook delivery failed")
}

func (s *Simulator) post(body []byte) error {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequest(http.MethodPost, s.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Signature", services.Sign(s.cfg.Secret, timestamp, body))
	resp, err := s.cfg.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("webhook answered %d", resp.StatusCode)
	}
	return nil
}

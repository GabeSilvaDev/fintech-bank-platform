package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/domain"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

type PaymentReader interface {
	Get(ctx context.Context, id uuid.UUID) (*models.Payment, error)
	ListByAccount(ctx context.Context, accountID uuid.UUID, limit int) ([]*models.Payment, error)
}

type ReadHandler struct {
	payments PaymentReader
}

func NewReadHandler(payments PaymentReader) *ReadHandler {
	return &ReadHandler{payments: payments}
}

type tedResponse struct {
	BankCode string `json:"bank_code"`
	Branch   string `json:"branch"`
	Account  string `json:"account"`
	Document string `json:"document"`
}

type paymentResponse struct {
	PaymentID      string       `json:"payment_id"`
	AccountID      string       `json:"account_id"`
	PaymentMethod  string       `json:"payment_method"`
	Status         string       `json:"status"`
	Amount         float64      `json:"amount"`
	Currency       string       `json:"currency"`
	Recipient      string       `json:"recipient"`
	PixKey         string       `json:"pix_key,omitempty"`
	BoletoCode     string       `json:"boleto_code,omitempty"`
	TED            *tedResponse `json:"ted,omitempty"`
	Description    string       `json:"description,omitempty"`
	IdempotencyKey string       `json:"idempotency_key"`
	ExternalID     string       `json:"external_id,omitempty"`
	FailureReason  string       `json:"failure_reason,omitempty"`
	BalanceAfter   *float64     `json:"balance_after,omitempty"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
	CompletedAt    *time.Time   `json:"completed_at,omitempty"`
}

func (h *ReadHandler) GetPayment(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"), "id")
	if err != nil {
		response.FromError(w, err)
		return
	}
	payment, err := h.payments.Get(r.Context(), id)
	if err != nil {
		response.FromError(w, mapReadError(err))
		return
	}
	response.OK(w, toResponse(payment))
}

func (h *ReadHandler) ListAccountPayments(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseID(chi.URLParam(r, "account_id"), "account_id")
	if err != nil {
		response.FromError(w, err)
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		response.FromError(w, err)
		return
	}
	payments, err := h.payments.ListByAccount(r.Context(), accountID, limit)
	if err != nil {
		response.FromError(w, mapReadError(err))
		return
	}
	items := make([]paymentResponse, 0, len(payments))
	for _, payment := range payments {
		items = append(items, toResponse(payment))
	}
	response.OK(w, items)
}

func parseID(raw, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail(field, "uuid")
	}
	return id, nil
}

func parseLimit(raw string) (int, error) {
	if raw == "" {
		return defaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxLimit {
		return 0, apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("limit", "range")
	}
	return limit, nil
}

func mapReadError(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return apperrors.NotFound("PAYMENT_NOT_FOUND", "payment not found")
	}
	return err
}

func toResponse(payment *models.Payment) paymentResponse {
	out := paymentResponse{
		PaymentID:      payment.ID.String(),
		AccountID:      payment.AccountID.String(),
		PaymentMethod:  string(payment.Method),
		Status:         string(payment.Status),
		Amount:         domain.FromCents(payment.AmountCents),
		Currency:       payment.Currency,
		Recipient:      payment.Recipient,
		PixKey:         payment.PixKey,
		BoletoCode:     payment.BoletoCode,
		Description:    payment.Description,
		IdempotencyKey: payment.IdempotencyKey,
		ExternalID:     payment.ExternalID,
		FailureReason:  payment.FailureReason,
		CreatedAt:      payment.CreatedAt,
		UpdatedAt:      payment.UpdatedAt,
		CompletedAt:    payment.CompletedAt,
	}
	if payment.TED != nil {
		out.TED = &tedResponse{BankCode: payment.TED.BankCode, Branch: payment.TED.Branch, Account: payment.TED.Account, Document: maskDocument(payment.TED.Document)}
	}
	if payment.BalanceAfterCents != nil {
		balance := domain.FromCents(*payment.BalanceAfterCents)
		out.BalanceAfter = &balance
	}
	return out
}

func maskDocument(document string) string {
	if len(document) <= 2 {
		return strings.Repeat("*", len(document))
	}
	return strings.Repeat("*", len(document)-2) + document[len(document)-2:]
}

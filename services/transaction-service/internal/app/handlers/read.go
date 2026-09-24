package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/fintech-bank-platform/pkg/domain"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

type TransactionReader interface {
	Get(ctx context.Context, id uuid.UUID) (*models.Transaction, error)
	ListByAccount(ctx context.Context, accountID uuid.UUID, limit int) ([]*models.Transaction, error)
}

type ReadHandler struct {
	txns TransactionReader
}

func NewReadHandler(txns TransactionReader) *ReadHandler {
	return &ReadHandler{txns: txns}
}

type transactionResponse struct {
	TransactionID    string     `json:"transaction_id"`
	Type             string     `json:"type"`
	Status           string     `json:"status"`
	AccountID        string     `json:"account_id"`
	CounterpartyID   string     `json:"counterparty_id,omitempty"`
	Amount           float64    `json:"amount"`
	Currency         string     `json:"currency"`
	Description      string     `json:"description,omitempty"`
	IdempotencyKey   string     `json:"idempotency_key"`
	FailureReason    string     `json:"failure_reason,omitempty"`
	FromBalanceAfter *float64   `json:"from_balance_after,omitempty"`
	ToBalanceAfter   *float64   `json:"to_balance_after,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

func (h *ReadHandler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"), "id")
	if err != nil {
		response.FromError(w, err)
		return
	}
	tx, err := h.txns.Get(r.Context(), id)
	if err != nil {
		response.FromError(w, mapReadError(err))
		return
	}
	response.OK(w, toDetailResponse(tx))
}

func (h *ReadHandler) ListAccountTransactions(w http.ResponseWriter, r *http.Request) {
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
	txns, err := h.txns.ListByAccount(r.Context(), accountID, limit)
	if err != nil {
		response.FromError(w, mapReadError(err))
		return
	}
	items := make([]transactionResponse, 0, len(txns))
	for _, tx := range txns {
		items = append(items, toListResponse(tx, accountID))
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
		return apperrors.NotFound("TRANSACTION_NOT_FOUND", "transaction not found")
	}
	return err
}

func baseResponse(tx *models.Transaction) transactionResponse {
	out := transactionResponse{
		TransactionID:  tx.ID.String(),
		Type:           string(tx.Type),
		Status:         string(tx.Status),
		AccountID:      tx.AccountID.String(),
		Amount:         domain.FromCents(tx.AmountCents),
		Currency:       tx.Currency,
		Description:    tx.Description,
		IdempotencyKey: tx.IdempotencyKey,
		FailureReason:  tx.FailureReason,
		CreatedAt:      tx.CreatedAt,
		UpdatedAt:      tx.UpdatedAt,
		CompletedAt:    tx.CompletedAt,
	}
	if tx.CounterpartyID != nil {
		out.CounterpartyID = tx.CounterpartyID.String()
	}
	return out
}

func toDetailResponse(tx *models.Transaction) transactionResponse {
	return baseResponse(tx)
}

func toListResponse(tx *models.Transaction, viewedAccount uuid.UUID) transactionResponse {
	out := baseResponse(tx)
	if tx.Type == models.TypeTransfer {
		if viewedAccount == tx.AccountID && tx.FromBalanceCents != nil {
			value := domain.FromCents(*tx.FromBalanceCents)
			out.FromBalanceAfter = &value
		}
		if tx.CounterpartyID != nil && viewedAccount == *tx.CounterpartyID && tx.ToBalanceCents != nil {
			value := domain.FromCents(*tx.ToBalanceCents)
			out.ToBalanceAfter = &value
		}
		return out
	}
	if tx.FromBalanceCents != nil {
		value := domain.FromCents(*tx.FromBalanceCents)
		out.FromBalanceAfter = &value
	}
	if tx.ToBalanceCents != nil {
		value := domain.FromCents(*tx.ToBalanceCents)
		out.ToBalanceAfter = &value
	}
	return out
}

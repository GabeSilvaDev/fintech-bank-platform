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
	ListByAccount(ctx context.Context, accountID uuid.UUID, before *time.Time, limit int) (models.Page, error)
}

type ReadHandler struct {
	txns TransactionReader
}

func NewReadHandler(txns TransactionReader) *ReadHandler {
	return &ReadHandler{txns: txns}
}

type transactionResponse struct {
	TransactionID    string         `json:"transaction_id"`
	Type             string         `json:"type"`
	Status           string         `json:"status"`
	AccountID        string         `json:"account_id"`
	CounterpartyID   string         `json:"counterparty_id,omitempty"`
	Amount           domain.Amount  `json:"amount"`
	Currency         string         `json:"currency"`
	Description      string         `json:"description,omitempty"`
	IdempotencyKey   string         `json:"idempotency_key"`
	FailureReason    string         `json:"failure_reason,omitempty"`
	FromBalanceAfter *domain.Amount `json:"from_balance_after,omitempty"`
	ToBalanceAfter   *domain.Amount `json:"to_balance_after,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	CompletedAt      *time.Time     `json:"completed_at,omitempty"`
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
	before, err := parseBefore(r.URL.Query().Get("before"))
	if err != nil {
		response.FromError(w, err)
		return
	}
	page, err := h.txns.ListByAccount(r.Context(), accountID, before, limit)
	if err != nil {
		response.FromError(w, mapReadError(err))
		return
	}
	if page.Scanned == limit && page.Last != nil {
		w.Header().Set("X-Next-Before", page.Last.UTC().Format(time.RFC3339Nano))
	}
	items := make([]transactionResponse, 0, len(page.Items))
	for _, tx := range page.Items {
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

func parseBefore(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("before", "datetime")
	}
	return &parsed, nil
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
		Amount:         domain.AmountFromCents(tx.AmountCents),
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
			value := domain.AmountFromCents(*tx.FromBalanceCents)
			out.FromBalanceAfter = &value
		}
		if tx.CounterpartyID != nil && viewedAccount == *tx.CounterpartyID && tx.ToBalanceCents != nil {
			value := domain.AmountFromCents(*tx.ToBalanceCents)
			out.ToBalanceAfter = &value
		}
		return out
	}
	if tx.FromBalanceCents != nil {
		value := domain.AmountFromCents(*tx.FromBalanceCents)
		out.FromBalanceAfter = &value
	}
	if tx.ToBalanceCents != nil {
		value := domain.AmountFromCents(*tx.ToBalanceCents)
		out.ToBalanceAfter = &value
	}
	return out
}

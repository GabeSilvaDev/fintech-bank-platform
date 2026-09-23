package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/domain"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type AccountReader interface {
	Get(ctx context.Context, accountID uuid.UUID) (*models.Account, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*models.Account, error)
	Owner(ctx context.Context, accountID uuid.UUID) (*models.Account, *models.Customer, error)
}

type ReadHandler struct {
	accounts AccountReader
}

func NewReadHandler(accounts AccountReader) *ReadHandler {
	return &ReadHandler{accounts: accounts}
}

type accountResponse struct {
	AccountID string     `json:"account_id"`
	UserID    string     `json:"user_id"`
	Agency    string     `json:"agency"`
	Number    string     `json:"number"`
	Type      string     `json:"type"`
	Status    string     `json:"status"`
	Currency  string     `json:"currency"`
	Balance   float64    `json:"balance"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
}

func (h *ReadHandler) GetAccount(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseID(chi.URLParam(r, "id"), "id")
	if err != nil {
		response.FromError(w, err)
		return
	}

	account, err := h.accounts.Get(r.Context(), accountID)
	if err != nil {
		response.FromError(w, mapReadError(err))
		return
	}

	response.OK(w, toResponse(account))
}

type ownerResponse struct {
	AccountID string `json:"account_id"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Phone     string `json:"phone,omitempty"`
}

func (h *ReadHandler) GetOwner(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseID(chi.URLParam(r, "id"), "id")
	if err != nil {
		response.FromError(w, err)
		return
	}

	account, customer, err := h.accounts.Owner(r.Context(), accountID)
	if err != nil {
		response.FromError(w, mapReadError(err))
		return
	}

	response.OK(w, ownerResponse{
		AccountID: account.AccountID.String(),
		UserID:    customer.UserID.String(),
		Name:      customer.Name,
		Email:     customer.Email,
		Phone:     customer.Phone,
	})
}

func (h *ReadHandler) ListUserAccounts(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(chi.URLParam(r, "user_id"), "user_id")
	if err != nil {
		response.FromError(w, err)
		return
	}

	accounts, err := h.accounts.ListByUser(r.Context(), userID)
	if err != nil {
		response.FromError(w, mapReadError(err))
		return
	}

	items := make([]accountResponse, 0, len(accounts))
	for _, account := range accounts {
		items = append(items, toResponse(account))
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

func mapReadError(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return apperrors.NotFound("ACCOUNT_NOT_FOUND", "account not found")
	}
	return err
}

func toResponse(account *models.Account) accountResponse {
	return accountResponse{
		AccountID: account.AccountID.String(),
		UserID:    account.UserID.String(),
		Agency:    account.Agency,
		Number:    account.Number,
		Type:      string(account.Type),
		Status:    string(account.Status),
		Currency:  account.Currency,
		Balance:   domain.FromCents(account.BalanceCents),
		CreatedAt: account.CreatedAt,
		UpdatedAt: account.UpdatedAt,
		ClosedAt:  account.ClosedAt,
	}
}

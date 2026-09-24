package handlers

import (
	"net/http"
	"strings"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/response"
)

type createTransactionRequest struct {
	AccountID      string  `json:"account_id" validate:"required,uuid"`
	Type           string  `json:"type" validate:"required,oneof=deposit withdrawal"`
	Amount         float64 `json:"amount" validate:"required,gt=0"`
	Currency       string  `json:"currency" validate:"required,currency"`
	Description    string  `json:"description" validate:"max=255"`
	IdempotencyKey string  `json:"idempotency_key" validate:"required,idempotency_key"`
}

type transferRequest struct {
	FromAccountID  string  `json:"from_account_id" validate:"required,uuid"`
	ToAccountID    string  `json:"to_account_id" validate:"required,uuid,nefield=FromAccountID"`
	Amount         float64 `json:"amount" validate:"required,gt=0"`
	Currency       string  `json:"currency" validate:"required,currency"`
	Description    string  `json:"description" validate:"max=255"`
	IdempotencyKey string  `json:"idempotency_key" validate:"required,idempotency_key"`
}

type TransactionHandler struct {
	publisher contracts.Publisher
}

func NewTransactionHandler(publisher contracts.Publisher) *TransactionHandler {
	return &TransactionHandler{publisher: publisher}
}

func (h *TransactionHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTransactionRequest
	if err := decode(r, &req); err != nil {
		response.FromError(w, err)
		return
	}
	if err := validate(req); err != nil {
		response.FromError(w, err)
		return
	}

	event := events.NewTransactionCommand(events.EventTypes.CreateTransaction, events.CreateTransactionPayload{
		AccountID:      req.AccountID,
		Type:           req.Type,
		Amount:         req.Amount,
		Currency:       strings.ToUpper(req.Currency),
		Description:    req.Description,
		IdempotencyKey: req.IdempotencyKey,
	})

	publish(w, r, h.publisher, events.Topics.TransactionCommands, req.AccountID, event)
}

func (h *TransactionHandler) Transfer(w http.ResponseWriter, r *http.Request) {
	var req transferRequest
	if err := decode(r, &req); err != nil {
		response.FromError(w, err)
		return
	}
	if err := validate(req); err != nil {
		response.FromError(w, err)
		return
	}

	event := events.NewTransactionCommand(events.EventTypes.ProcessTransfer, events.ProcessTransferPayload{
		FromAccountID:  req.FromAccountID,
		ToAccountID:    req.ToAccountID,
		Amount:         req.Amount,
		Currency:       strings.ToUpper(req.Currency),
		Description:    req.Description,
		IdempotencyKey: req.IdempotencyKey,
	})

	publish(w, r, h.publisher, events.Topics.TransactionCommands, req.FromAccountID, event)
}

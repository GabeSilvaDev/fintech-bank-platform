package handlers

import (
	"net/http"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/go-chi/chi/v5"
)

type createAccountRequest struct {
	UserID      string `json:"user_id" validate:"required,uuid"`
	AccountType string `json:"account_type" validate:"required,oneof=checking savings"`
	Name        string `json:"name" validate:"required,min=3,max=120"`
	Email       string `json:"email" validate:"required,email"`
	Document    string `json:"document" validate:"required,cpf|cnpj"`
	Phone       string `json:"phone" validate:"omitempty,phone_br"`
}

type updateAccountRequest struct {
	Name   *string `json:"name" validate:"omitempty,min=3,max=120"`
	Email  *string `json:"email" validate:"omitempty,email"`
	Phone  *string `json:"phone" validate:"omitempty,phone_br"`
	Status *string `json:"status" validate:"omitempty,oneof=active blocked closed"`
}

func (r updateAccountRequest) empty() bool {
	return r.Name == nil && r.Email == nil && r.Phone == nil && r.Status == nil
}

type AccountHandler struct {
	publisher contracts.Publisher
}

func NewAccountHandler(publisher contracts.Publisher) *AccountHandler {
	return &AccountHandler{publisher: publisher}
}

func (h *AccountHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createAccountRequest
	if err := decode(r, &req); err != nil {
		response.FromError(w, err)
		return
	}
	if err := validate(req); err != nil {
		response.FromError(w, err)
		return
	}

	event := events.NewAccountCommand(events.EventTypes.CreateAccount, events.CreateAccountPayload{
		UserID:      req.UserID,
		AccountType: req.AccountType,
		Name:        req.Name,
		Email:       req.Email,
		Document:    req.Document,
		Phone:       req.Phone,
	})

	publish(w, r, h.publisher, events.Topics.AccountCommands, req.UserID, event)
}

func (h *AccountHandler) Update(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "id")
	if err := validateID(accountID); err != nil {
		response.FromError(w, err)
		return
	}

	var req updateAccountRequest
	if err := decode(r, &req); err != nil {
		response.FromError(w, err)
		return
	}
	if req.empty() {
		response.FromError(w, errors.UnprocessableEntity("EMPTY_UPDATE", "at least one field must be provided"))
		return
	}
	if err := validate(req); err != nil {
		response.FromError(w, err)
		return
	}

	event := events.NewAccountCommand(events.EventTypes.UpdateAccount, events.UpdateAccountPayload{
		AccountID: accountID,
		Name:      req.Name,
		Email:     req.Email,
		Phone:     req.Phone,
		Status:    req.Status,
	})

	publish(w, r, h.publisher, events.Topics.AccountCommands, accountID, event)
}

func (h *AccountHandler) Delete(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "id")
	if err := validateID(accountID); err != nil {
		response.FromError(w, err)
		return
	}

	event := events.NewAccountCommand(events.EventTypes.DeleteAccount, events.DeleteAccountPayload{
		AccountID: accountID,
	})

	publish(w, r, h.publisher, events.Topics.AccountCommands, accountID, event)
}

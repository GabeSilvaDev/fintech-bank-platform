package handlers

import (
	"net/http"
	"strings"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/fintech-bank-platform/pkg/validation"
)

type tedRequest struct {
	BankCode string `json:"bank_code" validate:"required,len=3,number"`
	Branch   string `json:"branch" validate:"required,agency_number"`
	Account  string `json:"account" validate:"required,account_number"`
	Document string `json:"document" validate:"required,cpf|cnpj"`
}

type paymentRequest struct {
	AccountID      string        `json:"account_id" validate:"required,uuid"`
	PaymentMethod  string        `json:"payment_method" validate:"required,oneof=pix ted boleto"`
	Amount         domain.Amount `json:"amount"`
	Currency       string        `json:"currency" validate:"required,currency"`
	Recipient      string        `json:"recipient" validate:"required,max=120"`
	PixKey         string        `json:"pix_key" validate:"omitempty,pix_key"`
	BoletoCode     string        `json:"boleto_code" validate:"omitempty,boleto"`
	TED            *tedRequest   `json:"ted" validate:"omitempty"`
	Description    string        `json:"description" validate:"max=255"`
	IdempotencyKey string        `json:"idempotency_key" validate:"required,idempotency_key"`
}

func (r paymentRequest) missingMethodField() string {
	switch r.PaymentMethod {
	case "pix":
		if r.PixKey == "" {
			return "pix_key"
		}
	case "boleto":
		if r.BoletoCode == "" {
			return "boleto_code"
		}
	case "ted":
		if r.TED == nil {
			return "ted"
		}
	}
	return ""
}

func (r paymentRequest) tedDetails() *events.TEDDetails {
	if r.TED == nil {
		return nil
	}
	return &events.TEDDetails{BankCode: r.TED.BankCode, Branch: r.TED.Branch, Account: r.TED.Account, Document: r.TED.Document}
}

type PaymentHandler struct {
	publisher contracts.Publisher
}

func NewPaymentHandler(publisher contracts.Publisher) *PaymentHandler {
	return &PaymentHandler{publisher: publisher}
}

func (h *PaymentHandler) Process(w http.ResponseWriter, r *http.Request) {
	var req paymentRequest
	if err := decode(r, &req); err != nil {
		response.FromError(w, err)
		return
	}
	if req.TED != nil && req.PaymentMethod != "ted" {
		response.FromError(w, errors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("ted", "excluded"))
		return
	}
	if err := validate(req); err != nil {
		response.FromError(w, err)
		return
	}
	if strings.TrimSpace(req.Recipient) == "" {
		response.FromError(w, errors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("recipient", "required"))
		return
	}
	if field := req.missingMethodField(); field != "" {
		response.FromError(w, errors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail(field, "required"))
		return
	}
	if err := validateAmount(req.Amount); err != nil {
		response.FromError(w, err)
		return
	}
	if req.PaymentMethod == "boleto" {
		if boletoCents, ok := validation.BoletoAmountCents(req.BoletoCode); ok && boletoCents > 0 && req.Amount.Cents() != boletoCents {
			response.FromError(w, errors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail("amount", "boleto_amount"))
			return
		}
	}

	event := events.NewPaymentCommand(events.EventTypes.ProcessPayment, events.ProcessPaymentPayload{
		AccountID:      req.AccountID,
		PaymentMethod:  req.PaymentMethod,
		Amount:         req.Amount,
		Currency:       strings.ToUpper(req.Currency),
		Recipient:      req.Recipient,
		PixKey:         req.PixKey,
		BoletoCode:     req.BoletoCode,
		TED:            req.tedDetails(),
		Description:    req.Description,
		IdempotencyKey: req.IdempotencyKey,
	})

	publish(w, r, h.publisher, events.Topics.PaymentCommands, req.AccountID, event)
}

package services

import (
	"strings"
	"unicode/utf8"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/validation"
	"github.com/google/uuid"
)

const (
	maxRecipientRunes = 120
	maxDescriptionLen = 255
)

func parsePayment(cmd events.ProcessPaymentPayload) (*models.Payment, error) {
	accountID, err := uuid.Parse(cmd.AccountID)
	if err != nil {
		return nil, domain.Invalid("invalid_account_id", "account_id must be a uuid")
	}
	method, ok := models.ParseMethod(cmd.PaymentMethod)
	if !ok {
		return nil, domain.Invalid("invalid_payment_method", "payment_method must be pix, ted or boleto")
	}
	if !cmd.Amount.IsPositive() {
		return nil, domain.Invalid("invalid_amount", "amount must be greater than zero")
	}
	cents := cmd.Amount.Cents()
	if !strings.EqualFold(cmd.Currency, models.Currency) {
		return nil, domain.Invalid("unsupported_currency", "only BRL is supported")
	}
	recipient := strings.TrimSpace(cmd.Recipient)
	if recipient == "" || utf8.RuneCountInString(recipient) > maxRecipientRunes {
		return nil, domain.Invalid("invalid_recipient", "recipient must have between 1 and 120 characters")
	}
	if !validation.IsValidIdempotencyKey(cmd.IdempotencyKey) {
		return nil, domain.Invalid("invalid_idempotency_key", "idempotency_key must be 1 to 64 printable ASCII characters with no spaces")
	}
	if utf8.RuneCountInString(cmd.Description) > maxDescriptionLen {
		return nil, domain.Invalid("invalid_description", "description must have at most 255 characters")
	}

	payment := &models.Payment{AccountID: accountID, Method: method, AmountCents: cents, Recipient: recipient, Description: cmd.Description, IdempotencyKey: cmd.IdempotencyKey}
	switch method {
	case models.MethodPix:
		if !validation.IsValidPixKey(cmd.PixKey) {
			return nil, domain.Invalid("invalid_pix_key", "pix_key is not a valid PIX key")
		}
		payment.PixKey = cmd.PixKey
	case models.MethodBoleto:
		encoded, ok := validation.BoletoAmountCents(cmd.BoletoCode)
		if !ok {
			return nil, domain.Invalid("invalid_boleto", "boleto_code is not a valid digitable line")
		}
		if encoded != 0 && encoded != cents {
			return nil, domain.Invalid("boleto_amount_mismatch", "amount differs from the amount encoded in the boleto")
		}
		payment.BoletoCode = cmd.BoletoCode
	default:
		ted, err := parseTED(cmd.TED)
		if err != nil {
			return nil, err
		}
		payment.TED = ted
	}
	return payment, nil
}

func parseTED(ted *events.TEDDetails) (*models.TEDDetails, error) {
	if ted == nil || len(ted.BankCode) != 3 || !isDigits(ted.BankCode) ||
		!validation.IsValidAgencyNumber(ted.Branch) || !validation.IsValidAccountNumber(ted.Account) ||
		!(validation.IsValidCPF(ted.Document) || validation.IsValidCNPJ(ted.Document)) {
		return nil, domain.Invalid("invalid_ted_destination", "ted requires bank_code, branch, account and a CPF or CNPJ document")
	}
	return &models.TEDDetails{BankCode: ted.BankCode, Branch: ted.Branch, Account: ted.Account, Document: ted.Document}, nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

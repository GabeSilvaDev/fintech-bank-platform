package services

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/contracts"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/validation"
	"github.com/google/uuid"
)

const (
	maxNumberAttempts  = 5
	maxBalanceAttempts = 5
)

type NumberGenerator func() string

func RandomNumber() string {
	return fmt.Sprintf("%08d", rand.IntN(100000000))
}

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}

type AccountService struct {
	accounts  contracts.AccountRepository
	customers contracts.CustomerRepository
	clock     contracts.Clock
	number    NumberGenerator
}

func NewAccountService(accounts contracts.AccountRepository, customers contracts.CustomerRepository, clock contracts.Clock, number NumberGenerator) *AccountService {
	return &AccountService{accounts: accounts, customers: customers, clock: clock, number: number}
}

func (s *AccountService) Create(ctx context.Context, cmd events.CreateAccountPayload) (events.AccountCreatedPayload, error) {
	userID, err := uuid.Parse(cmd.UserID)
	if err != nil {
		return events.AccountCreatedPayload{}, models.Invalid("invalid_user_id", "user_id must be a uuid")
	}
	kind, ok := models.ParseAccountType(cmd.AccountType)
	if !ok {
		return events.AccountCreatedPayload{}, models.Invalid("invalid_account_type", "account_type must be checking or savings")
	}
	if err := validateProfile(&cmd.Name, &cmd.Email, &cmd.Phone); err != nil {
		return events.AccountCreatedPayload{}, err
	}
	document := validation.SanitizeCPF(cmd.Document)
	if !validation.IsValidCPF(document) && !validation.IsValidCNPJ(document) {
		return events.AccountCreatedPayload{}, models.Invalid("invalid_document", "document must be a valid CPF or CNPJ")
	}

	now := s.clock.Now()
	if err := s.upsertCustomer(ctx, userID, cmd.Name, cmd.Email, document, validation.SanitizePhone(cmd.Phone), now); err != nil {
		return events.AccountCreatedPayload{}, err
	}

	accountID := uuid.New()
	number, err := s.reserveNumber(ctx, accountID)
	if err != nil {
		return events.AccountCreatedPayload{}, err
	}

	account := &models.Account{
		AccountID: accountID,
		UserID:    userID,
		Agency:    models.Agency,
		Number:    number,
		Type:      kind,
		Status:    models.AccountStatusActive,
		Currency:  models.Currency,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.accounts.Create(ctx, account); err != nil {
		return events.AccountCreatedPayload{}, err
	}

	return events.AccountCreatedPayload{
		AccountID:     accountID.String(),
		UserID:        userID.String(),
		AccountNumber: number,
		Agency:        models.Agency,
		AccountType:   string(kind),
		Status:        string(models.AccountStatusActive),
		CreatedAt:     now,
	}, nil
}

func (s *AccountService) Update(ctx context.Context, cmd events.UpdateAccountPayload) (events.AccountUpdatedPayload, error) {
	accountID, err := uuid.Parse(cmd.AccountID)
	if err != nil {
		return events.AccountUpdatedPayload{}, models.Invalid("invalid_account_id", "account_id must be a uuid")
	}
	if cmd.Name == nil && cmd.Email == nil && cmd.Phone == nil && cmd.Status == nil {
		return events.AccountUpdatedPayload{}, models.Invalid("empty_update", "at least one field must be provided")
	}

	account, err := s.accounts.Get(ctx, accountID)
	if err != nil {
		return events.AccountUpdatedPayload{}, err
	}
	if account.Status == models.AccountStatusClosed {
		return events.AccountUpdatedPayload{}, models.Invalid("account_closed", "account is closed")
	}
	if err := validateProfile(cmd.Name, cmd.Email, cmd.Phone); err != nil {
		return events.AccountUpdatedPayload{}, err
	}

	var status models.AccountStatus
	if cmd.Status != nil {
		parsed, ok := models.ParseAccountStatus(*cmd.Status)
		if !ok || parsed == models.AccountStatusClosed {
			return events.AccountUpdatedPayload{}, models.Invalid("invalid_status", "status must be active or blocked")
		}
		status = parsed
	}

	now := s.clock.Now()
	var phone *string
	if cmd.Phone != nil {
		sanitized := validation.SanitizePhone(*cmd.Phone)
		phone = &sanitized
	}
	if cmd.Name != nil || cmd.Email != nil || phone != nil {
		if err := s.customers.UpdateProfile(ctx, account.UserID, cmd.Name, cmd.Email, phone, now); err != nil {
			return events.AccountUpdatedPayload{}, err
		}
	}
	if status != "" {
		if err := s.accounts.UpdateStatus(ctx, accountID, status, now, nil); err != nil {
			return events.AccountUpdatedPayload{}, err
		}
		account.Status = status
	}

	customer, err := s.customers.Get(ctx, account.UserID)
	if err != nil {
		return events.AccountUpdatedPayload{}, err
	}

	return events.AccountUpdatedPayload{
		AccountID: accountID.String(),
		UserID:    account.UserID.String(),
		Name:      customer.Name,
		Email:     customer.Email,
		Phone:     customer.Phone,
		Status:    string(account.Status),
		UpdatedAt: now,
	}, nil
}

func (s *AccountService) Close(ctx context.Context, cmd events.DeleteAccountPayload) (events.AccountDeletedPayload, error) {
	accountID, err := uuid.Parse(cmd.AccountID)
	if err != nil {
		return events.AccountDeletedPayload{}, models.Invalid("invalid_account_id", "account_id must be a uuid")
	}

	account, err := s.accounts.Get(ctx, accountID)
	if err != nil {
		return events.AccountDeletedPayload{}, err
	}
	if account.Status == models.AccountStatusClosed {
		return events.AccountDeletedPayload{}, models.Invalid("account_closed", "account is already closed")
	}
	if account.BalanceCents != 0 {
		return events.AccountDeletedPayload{}, models.Invalid("account_has_balance", "account balance must be zero before closing")
	}

	now := s.clock.Now()
	applied, err := s.accounts.CloseIfEmpty(ctx, accountID, now)
	if err != nil {
		return events.AccountDeletedPayload{}, err
	}
	if !applied {
		if account, err = s.accounts.Get(ctx, accountID); err != nil {
			return events.AccountDeletedPayload{}, err
		}
		if account.Status == models.AccountStatusClosed {
			return events.AccountDeletedPayload{}, models.Invalid("account_closed", "account is already closed")
		}
		return events.AccountDeletedPayload{}, models.Invalid("account_has_balance", "account balance must be zero before closing")
	}

	return events.AccountDeletedPayload{
		AccountID: accountID.String(),
		UserID:    account.UserID.String(),
		Reason:    cmd.Reason,
		ClosedAt:  now,
	}, nil
}

func (s *AccountService) Get(ctx context.Context, accountID uuid.UUID) (*models.Account, error) {
	return s.accounts.Get(ctx, accountID)
}

func (s *AccountService) ListByUser(ctx context.Context, userID uuid.UUID) ([]*models.Account, error) {
	return s.accounts.ListByUser(ctx, userID)
}

func (s *AccountService) upsertCustomer(ctx context.Context, userID uuid.UUID, name, email, document, phone string, now time.Time) error {
	existing, err := s.customers.Get(ctx, userID)
	if errors.Is(err, models.ErrNotFound) {
		return s.customers.Upsert(ctx, &models.Customer{
			UserID:    userID,
			Name:      name,
			Email:     email,
			Document:  document,
			Phone:     phone,
			KYCStatus: models.KYCStatusPending,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	if err != nil {
		return err
	}

	existing.Name = name
	existing.Email = email
	existing.Document = document
	existing.Phone = phone
	existing.UpdatedAt = now
	return s.customers.Upsert(ctx, existing)
}

func (s *AccountService) reserveNumber(ctx context.Context, accountID uuid.UUID) (string, error) {
	for attempt := 0; attempt < maxNumberAttempts; attempt++ {
		number := s.number()
		reserved, err := s.accounts.ReserveNumber(ctx, models.Agency, number, accountID)
		if err != nil {
			return "", err
		}
		if reserved {
			return number, nil
		}
	}
	return "", models.ErrConflict
}

func validateProfile(name, email, phone *string) error {
	if name != nil && len(strings.TrimSpace(*name)) < 3 {
		return models.Invalid("invalid_name", "name must have at least 3 characters")
	}
	if email != nil && validation.ValidateVar(*email, "required,email") != nil {
		return models.Invalid("invalid_email", "email is not valid")
	}
	if phone != nil && *phone != "" && !validation.IsValidBrazilianPhone(*phone) {
		return models.Invalid("invalid_phone", "phone is not a valid Brazilian number")
	}
	return nil
}

type DebitResult struct {
	Debited  *events.AccountDebitedPayload
	Rejected *events.DebitRejectedPayload
}

func (s *AccountService) Credit(ctx context.Context, cmd events.CreditAccountPayload) (events.AccountCreditedPayload, error) {
	accountID, cents, err := parseBalanceCommand(cmd.AccountID, cmd.Amount, cmd.Currency)
	if err != nil {
		return events.AccountCreditedPayload{}, err
	}

	account, err := s.accounts.Get(ctx, accountID)
	if err != nil {
		return events.AccountCreditedPayload{}, err
	}
	if account.Status != models.AccountStatusActive {
		return events.AccountCreditedPayload{}, models.Invalid("account_not_active", "account is not active")
	}

	now := s.clock.Now()
	for attempt := 0; attempt < maxBalanceAttempts; attempt++ {
		next := account.BalanceCents + cents
		applied, err := s.accounts.CompareAndSetBalance(ctx, accountID, account.BalanceCents, next, now)
		if err != nil {
			return events.AccountCreditedPayload{}, err
		}
		if applied {
			return events.AccountCreditedPayload{
				AccountID:      accountID.String(),
				Amount:         cmd.Amount,
				BalanceAfter:   models.FromCents(next),
				Reference:      cmd.Reference,
				IdempotencyKey: cmd.IdempotencyKey,
				OccurredAt:     now,
			}, nil
		}
		if account, err = s.accounts.Get(ctx, accountID); err != nil {
			return events.AccountCreditedPayload{}, err
		}
		if account.Status != models.AccountStatusActive {
			return events.AccountCreditedPayload{}, models.Invalid("account_not_active", "account is not active")
		}
	}
	return events.AccountCreditedPayload{}, models.ErrConflict
}

func (s *AccountService) Debit(ctx context.Context, cmd events.DebitAccountPayload) (DebitResult, error) {
	accountID, cents, err := parseBalanceCommand(cmd.AccountID, cmd.Amount, cmd.Currency)
	if err != nil {
		return DebitResult{}, err
	}

	account, err := s.accounts.Get(ctx, accountID)
	if err != nil {
		return DebitResult{}, err
	}
	if account.Status != models.AccountStatusActive {
		return rejected(cmd, account, "account_not_active"), nil
	}

	now := s.clock.Now()
	for attempt := 0; attempt < maxBalanceAttempts; attempt++ {
		if account.BalanceCents < cents {
			return rejected(cmd, account, "insufficient_funds"), nil
		}
		next := account.BalanceCents - cents
		applied, err := s.accounts.CompareAndSetBalance(ctx, accountID, account.BalanceCents, next, now)
		if err != nil {
			return DebitResult{}, err
		}
		if applied {
			return DebitResult{Debited: &events.AccountDebitedPayload{
				AccountID:      accountID.String(),
				Amount:         cmd.Amount,
				BalanceAfter:   models.FromCents(next),
				Reference:      cmd.Reference,
				IdempotencyKey: cmd.IdempotencyKey,
				OccurredAt:     now,
			}}, nil
		}
		if account, err = s.accounts.Get(ctx, accountID); err != nil {
			return DebitResult{}, err
		}
		if account.Status != models.AccountStatusActive {
			return rejected(cmd, account, "account_not_active"), nil
		}
	}
	return DebitResult{}, models.ErrConflict
}

func rejected(cmd events.DebitAccountPayload, account *models.Account, reason string) DebitResult {
	return DebitResult{Rejected: &events.DebitRejectedPayload{
		AccountID:      account.AccountID.String(),
		Amount:         cmd.Amount,
		Balance:        models.FromCents(account.BalanceCents),
		Reason:         reason,
		Reference:      cmd.Reference,
		IdempotencyKey: cmd.IdempotencyKey,
	}}
}

func parseBalanceCommand(accountID string, amount float64, currency string) (uuid.UUID, int64, error) {
	id, err := uuid.Parse(accountID)
	if err != nil {
		return uuid.Nil, 0, models.Invalid("invalid_account_id", "account_id must be a uuid")
	}
	cents, err := models.ToCents(amount)
	if err != nil {
		return uuid.Nil, 0, err
	}
	if !strings.EqualFold(currency, models.Currency) {
		return uuid.Nil, 0, models.Invalid("unsupported_currency", "only BRL is supported")
	}
	return id, cents, nil
}

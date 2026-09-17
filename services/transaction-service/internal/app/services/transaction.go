package services

import (
	"context"
	"strings"
	"time"

	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/internal/contracts"
	"github.com/google/uuid"
)

const (
	source            = "transaction-service"
	maxKeyLength      = 64
	maxDescriptionLen = 255
)

type IDGenerator func() uuid.UUID

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}

type TransactionService struct {
	repo  contracts.TransactionRepository
	clock contracts.Clock
	newID IDGenerator
}

func NewTransactionService(repo contracts.TransactionRepository, clock contracts.Clock, newID IDGenerator) *TransactionService {
	return &TransactionService{repo: repo, clock: clock, newID: newID}
}

func (s *TransactionService) Create(ctx context.Context, cmd events.CreateTransactionPayload, trace string) (processor.Result, error) {
	accountID, err := uuid.Parse(cmd.AccountID)
	if err != nil {
		return processor.Result{}, domain.Invalid("invalid_account_id", "account_id must be a uuid")
	}
	kind, ok := models.ParseCreateType(cmd.Type)
	if !ok {
		return processor.Result{}, domain.Invalid("invalid_type", "type must be deposit or withdrawal")
	}
	cents, key, err := parseMoney(cmd.Amount, cmd.Currency, cmd.IdempotencyKey, cmd.Description)
	if err != nil {
		return processor.Result{}, err
	}

	tx := &models.Transaction{Type: kind, AccountID: accountID, AmountCents: cents, Description: cmd.Description, IdempotencyKey: key}
	if err := s.record(ctx, tx); err != nil {
		return processor.Result{}, err
	}

	command := debitCommand(tx, accountID, trace)
	if kind == models.TypeDeposit {
		command = creditCommand(tx, accountID, models.StepCredit, trace)
	}
	return processor.Result{Messages: []processor.Message{
		{Topic: events.Topics.TransactionEvents, Key: accountID.String(), Event: createdEvent(tx, trace)},
		{Topic: events.Topics.AccountCommands, Key: accountID.String(), Event: command},
	}}, nil
}

func (s *TransactionService) Transfer(ctx context.Context, cmd events.ProcessTransferPayload, trace string) (processor.Result, error) {
	from, err := uuid.Parse(cmd.FromAccountID)
	if err != nil {
		return processor.Result{}, domain.Invalid("invalid_from_account_id", "from_account_id must be a uuid")
	}
	to, err := uuid.Parse(cmd.ToAccountID)
	if err != nil {
		return processor.Result{}, domain.Invalid("invalid_to_account_id", "to_account_id must be a uuid")
	}
	if from == to {
		return processor.Result{}, domain.Invalid("same_account", "from_account_id and to_account_id must differ")
	}
	cents, key, err := parseMoney(cmd.Amount, cmd.Currency, cmd.IdempotencyKey, cmd.Description)
	if err != nil {
		return processor.Result{}, err
	}

	tx := &models.Transaction{Type: models.TypeTransfer, AccountID: from, CounterpartyID: &to, AmountCents: cents, Description: cmd.Description, IdempotencyKey: key}
	if err := s.record(ctx, tx); err != nil {
		return processor.Result{}, err
	}

	return processor.Result{Messages: []processor.Message{
		{Topic: events.Topics.TransactionEvents, Key: from.String(), Event: createdEvent(tx, trace)},
		{Topic: events.Topics.AccountCommands, Key: from.String(), Event: debitCommand(tx, from, trace)},
	}}, nil
}

func (s *TransactionService) Get(ctx context.Context, id uuid.UUID) (*models.Transaction, error) {
	return s.repo.Get(ctx, id)
}

func (s *TransactionService) ListByAccount(ctx context.Context, accountID uuid.UUID, limit int) ([]*models.Transaction, error) {
	return s.repo.ListByAccount(ctx, accountID, limit)
}

func (s *TransactionService) record(ctx context.Context, tx *models.Transaction) error {
	now := s.clock.Now()
	tx.ID = s.newID()
	tx.Status = models.StatusPending
	tx.Currency = models.Currency
	tx.CreatedAt = now
	tx.UpdatedAt = now

	reserved, err := s.repo.ReserveKey(ctx, tx.IdempotencyKey, tx.ID)
	if err != nil {
		return err
	}
	if !reserved {
		return models.ErrDuplicateKey
	}
	return s.repo.Create(ctx, tx)
}

func parseMoney(amount float64, currency, key, description string) (int64, string, error) {
	cents, err := domain.ToCents(amount)
	if err != nil {
		return 0, "", err
	}
	if !strings.EqualFold(currency, models.Currency) {
		return 0, "", domain.Invalid("unsupported_currency", "only BRL is supported")
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > maxKeyLength {
		return 0, "", domain.Invalid("invalid_idempotency_key", "idempotency_key must have between 1 and 64 characters")
	}
	if len(description) > maxDescriptionLen {
		return 0, "", domain.Invalid("invalid_description", "description must have at most 255 characters")
	}
	return cents, key, nil
}

func createdEvent(tx *models.Transaction, trace string) *events.Event {
	payload := events.TransactionCreatedPayload{
		TransactionID:  tx.ID.String(),
		Type:           string(tx.Type),
		AccountID:      tx.AccountID.String(),
		Amount:         domain.FromCents(tx.AmountCents),
		Currency:       tx.Currency,
		Description:    tx.Description,
		IdempotencyKey: tx.IdempotencyKey,
		CreatedAt:      tx.CreatedAt,
	}
	if tx.CounterpartyID != nil {
		payload.CounterpartyID = tx.CounterpartyID.String()
	}
	return events.NewTransactionEvent(events.EventTypes.TransactionCreated, payload).WithTraceID(trace)
}

func creditCommand(tx *models.Transaction, accountID uuid.UUID, step models.Step, trace string) *events.Event {
	return events.NewEvent(events.EventTypes.CreditAccount, source, events.CreditAccountPayload{
		AccountID:      accountID.String(),
		Amount:         domain.FromCents(tx.AmountCents),
		Currency:       tx.Currency,
		Reference:      tx.ID.String(),
		IdempotencyKey: models.StepKey(tx.ID, step),
	}).WithTraceID(trace)
}

func debitCommand(tx *models.Transaction, accountID uuid.UUID, trace string) *events.Event {
	return events.NewEvent(events.EventTypes.DebitAccount, source, events.DebitAccountPayload{
		AccountID:      accountID.String(),
		Amount:         domain.FromCents(tx.AmountCents),
		Currency:       tx.Currency,
		Reference:      tx.ID.String(),
		IdempotencyKey: models.StepKey(tx.ID, models.StepDebit),
	}).WithTraceID(trace)
}

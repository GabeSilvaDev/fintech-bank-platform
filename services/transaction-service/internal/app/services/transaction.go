package services

import (
	"context"
	"errors"
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

	owner, err := s.repo.ReserveKey(ctx, tx.IdempotencyKey, tx.ID)
	if err != nil {
		return err
	}
	if owner != tx.ID {
		_, err := s.repo.Get(ctx, owner)
		if err == nil {
			return models.ErrDuplicateKey
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		tx.ID = owner
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

type Reply struct {
	Kind           string
	Reference      string
	IdempotencyKey string
	BalanceAfter   float64
	Reason         string
	TraceID        string
}

func (s *TransactionService) ApplyAccountEvent(ctx context.Context, reply Reply) (processor.Result, error) {
	id, err := uuid.Parse(reply.Reference)
	if err != nil {
		return processor.Result{}, nil
	}
	keyID, step, ok := models.ParseStepKey(reply.IdempotencyKey)
	if !ok || keyID != id {
		return processor.Result{}, nil
	}

	tx, err := s.repo.Get(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return processor.Result{}, nil
	}
	if err != nil {
		return processor.Result{}, err
	}
	now := s.clock.Now()

	switch {
	case reply.Kind == events.EventTypes.AccountDebited && step == models.StepDebit:
		return s.onDebited(ctx, tx, reply, now)
	case reply.Kind == events.EventTypes.DebitRejected && step == models.StepDebit:
		return s.fail(ctx, tx, models.StatusPending, reply, now)
	case reply.Kind == events.EventTypes.AccountCredited && step == models.StepCredit:
		return s.onCredited(ctx, tx, reply, now)
	case reply.Kind == events.EventTypes.CreditRejected && step == models.StepCredit:
		return s.onCreditRejected(ctx, tx, reply, now)
	case reply.Kind == events.EventTypes.AccountCredited && step == models.StepReversal && tx.Type == models.TypeTransfer:
		return s.settle(ctx, tx, models.StatusReversing, models.StatusReversed, models.Patch{UpdatedAt: now}, reply, now, false)
	case reply.Kind == events.EventTypes.CreditRejected && step == models.StepReversal && tx.Type == models.TypeTransfer:
		return s.settle(ctx, tx, models.StatusReversing, models.StatusReversalFailed, models.Patch{UpdatedAt: now}, reply, now, true)
	}
	return processor.Result{}, nil
}

func (s *TransactionService) onDebited(ctx context.Context, tx *models.Transaction, reply Reply, now time.Time) (processor.Result, error) {
	balance := domain.Cents(reply.BalanceAfter)
	switch tx.Type {
	case models.TypeWithdrawal:
		patch := models.Patch{FromBalanceCents: &balance, CompletedAt: &now, UpdatedAt: now}
		return s.complete(ctx, tx, models.StatusPending, patch, reply, now)
	case models.TypeTransfer:
		if tx.CounterpartyID == nil {
			return processor.Result{}, nil
		}
		applied, err := s.repo.Transition(ctx, tx.ID, models.StatusPending, models.StatusDebited, models.Patch{FromBalanceCents: &balance, UpdatedAt: now})
		if err != nil || !applied {
			return processor.Result{}, err
		}
		return processor.Result{Messages: []processor.Message{
			{Topic: events.Topics.AccountCommands, Key: tx.CounterpartyID.String(), Event: creditCommand(tx, *tx.CounterpartyID, models.StepCredit, reply.TraceID)},
		}}, nil
	}
	return processor.Result{}, nil
}

func (s *TransactionService) onCredited(ctx context.Context, tx *models.Transaction, reply Reply, now time.Time) (processor.Result, error) {
	balance := domain.Cents(reply.BalanceAfter)
	switch tx.Type {
	case models.TypeDeposit:
		patch := models.Patch{ToBalanceCents: &balance, CompletedAt: &now, UpdatedAt: now}
		return s.complete(ctx, tx, models.StatusPending, patch, reply, now)
	case models.TypeTransfer:
		if tx.CounterpartyID == nil {
			return processor.Result{}, nil
		}
		applied, err := s.repo.Transition(ctx, tx.ID, models.StatusDebited, models.StatusCompleted, models.Patch{ToBalanceCents: &balance, CompletedAt: &now, UpdatedAt: now})
		if err != nil || !applied {
			return processor.Result{}, err
		}
		return s.publish(tx, events.EventTypes.TransferCompleted, events.TransferCompletedPayload{
			TransferID:       tx.ID.String(),
			FromAccountID:    tx.AccountID.String(),
			ToAccountID:      tx.CounterpartyID.String(),
			Amount:           domain.FromCents(tx.AmountCents),
			Currency:         tx.Currency,
			FromBalanceAfter: domain.FromCents(cents(tx.FromBalanceCents)),
			ToBalanceAfter:   reply.BalanceAfter,
			CompletedAt:      now,
		}, reply.TraceID), nil
	}
	return processor.Result{}, nil
}

func (s *TransactionService) onCreditRejected(ctx context.Context, tx *models.Transaction, reply Reply, now time.Time) (processor.Result, error) {
	switch tx.Type {
	case models.TypeDeposit:
		return s.fail(ctx, tx, models.StatusPending, reply, now)
	case models.TypeTransfer:
		if tx.CounterpartyID == nil {
			return processor.Result{}, nil
		}
		applied, err := s.repo.Transition(ctx, tx.ID, models.StatusDebited, models.StatusReversing, models.Patch{FailureReason: &reply.Reason, UpdatedAt: now})
		if err != nil || !applied {
			return processor.Result{}, err
		}
		return processor.Result{Messages: []processor.Message{
			{Topic: events.Topics.AccountCommands, Key: tx.AccountID.String(), Event: creditCommand(tx, tx.AccountID, models.StepReversal, reply.TraceID)},
		}}, nil
	}
	return processor.Result{}, nil
}

func (s *TransactionService) complete(ctx context.Context, tx *models.Transaction, from models.TransactionStatus, patch models.Patch, reply Reply, now time.Time) (processor.Result, error) {
	applied, err := s.repo.Transition(ctx, tx.ID, from, models.StatusCompleted, patch)
	if err != nil || !applied {
		return processor.Result{}, err
	}
	return s.publish(tx, events.EventTypes.TransactionCompleted, events.TransactionCompletedPayload{
		TransactionID: tx.ID.String(),
		AccountID:     tx.AccountID.String(),
		Type:          string(tx.Type),
		Amount:        domain.FromCents(tx.AmountCents),
		Currency:      tx.Currency,
		BalanceAfter:  reply.BalanceAfter,
		Status:        string(models.StatusCompleted),
		CompletedAt:   now,
	}, reply.TraceID), nil
}

func (s *TransactionService) fail(ctx context.Context, tx *models.Transaction, from models.TransactionStatus, reply Reply, now time.Time) (processor.Result, error) {
	applied, err := s.repo.Transition(ctx, tx.ID, from, models.StatusFailed, models.Patch{FailureReason: &reply.Reason, UpdatedAt: now})
	if err != nil || !applied {
		return processor.Result{}, err
	}
	if tx.Type == models.TypeTransfer {
		return s.publish(tx, events.EventTypes.TransferFailed, transferFailed(tx, reply.Reason, models.StatusFailed, now), reply.TraceID), nil
	}
	return s.publish(tx, events.EventTypes.TransactionFailed, events.TransactionFailedPayload{
		TransactionID: tx.ID.String(),
		AccountID:     tx.AccountID.String(),
		Type:          string(tx.Type),
		Amount:        domain.FromCents(tx.AmountCents),
		Currency:      tx.Currency,
		Reason:        reply.Reason,
		FailedAt:      now,
	}, reply.TraceID), nil
}

func (s *TransactionService) settle(ctx context.Context, tx *models.Transaction, from, to models.TransactionStatus, patch models.Patch, reply Reply, now time.Time, escalate bool) (processor.Result, error) {
	applied, err := s.repo.Transition(ctx, tx.ID, from, to, patch)
	if err != nil || !applied {
		return processor.Result{}, err
	}
	result := s.publish(tx, events.EventTypes.TransferFailed, transferFailed(tx, tx.FailureReason, to, now), reply.TraceID)
	if escalate {
		result.Messages = append(result.Messages, processor.Message{
			Topic: events.Topics.TransactionDLQ,
			Key:   tx.ID.String(),
			Event: events.NewTransactionEvent(events.EventTypes.TransactionCommandFailed, events.ErrorPayload{
				ErrorCode:    "reversal_failed",
				ErrorMessage: "transfer " + tx.ID.String() + " could not be reversed: " + reply.Reason,
			}).WithTraceID(reply.TraceID),
		})
	}
	return result, nil
}

func (s *TransactionService) publish(tx *models.Transaction, eventType string, payload interface{}, trace string) processor.Result {
	return processor.Reply(events.Topics.TransactionEvents, tx.AccountID.String(), events.NewTransactionEvent(eventType, payload).WithTraceID(trace))
}

func transferFailed(tx *models.Transaction, reason string, status models.TransactionStatus, now time.Time) events.TransferFailedPayload {
	toAccountID := ""
	if tx.CounterpartyID != nil {
		toAccountID = tx.CounterpartyID.String()
	}
	return events.TransferFailedPayload{
		TransferID:    tx.ID.String(),
		FromAccountID: tx.AccountID.String(),
		ToAccountID:   toAccountID,
		Amount:        domain.FromCents(tx.AmountCents),
		Currency:      tx.Currency,
		Reason:        reason,
		Status:        string(status),
		FailedAt:      now,
	}
}

func cents(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

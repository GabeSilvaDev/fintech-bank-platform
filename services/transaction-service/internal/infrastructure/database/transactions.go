package database

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/pkg/cassandra"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/google/uuid"
)

type TransactionRepository struct {
	session *gocql.Session
}

func NewTransactionRepository(session *gocql.Session) *TransactionRepository {
	return &TransactionRepository{session: session}
}

const transactionColumns = "transaction_id, type, status, account_id, counterparty_id, amount_cents, currency, description, idempotency_key, failure_reason, from_balance_cents, to_balance_cents, created_at, updated_at, completed_at"

func (r *TransactionRepository) Create(ctx context.Context, tx *models.Transaction) error {
	var counterparty *gocql.UUID
	if tx.CounterpartyID != nil {
		id := gocql.UUID(*tx.CounterpartyID)
		counterparty = &id
	}

	batch := r.session.Batch(gocql.LoggedBatch).WithContext(ctx).
		Query("INSERT INTO transactions ("+transactionColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			gocql.UUID(tx.ID), string(tx.Type), string(tx.Status), gocql.UUID(tx.AccountID), counterparty, tx.AmountCents, tx.Currency, tx.Description, tx.IdempotencyKey, tx.FailureReason, tx.FromBalanceCents, tx.ToBalanceCents, tx.CreatedAt, tx.UpdatedAt, tx.CompletedAt).
		Query("INSERT INTO transactions_by_account (account_id, created_at, transaction_id) VALUES (?, ?, ?)", gocql.UUID(tx.AccountID), tx.CreatedAt, gocql.UUID(tx.ID))
	if counterparty != nil {
		batch = batch.Query("INSERT INTO transactions_by_account (account_id, created_at, transaction_id) VALUES (?, ?, ?)", *counterparty, tx.CreatedAt, gocql.UUID(tx.ID))
	}
	return cassandra.MapWriteError(batch.Exec())
}

func (r *TransactionRepository) ReserveKey(ctx context.Context, key string, id uuid.UUID) (bool, error) {
	applied, err := r.session.Query("INSERT INTO transactions_by_key (idempotency_key, transaction_id) VALUES (?, ?) IF NOT EXISTS", key, gocql.UUID(id)).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return applied, cassandra.MapWriteError(err)
}

func (r *TransactionRepository) Get(ctx context.Context, id uuid.UUID) (*models.Transaction, error) {
	return scanTransaction(r.session.Query("SELECT "+transactionColumns+" FROM transactions WHERE transaction_id = ?", gocql.UUID(id)).WithContext(ctx))
}

func (r *TransactionRepository) ListByAccount(ctx context.Context, accountID uuid.UUID, limit int) ([]*models.Transaction, error) {
	iter := r.session.Query("SELECT transaction_id FROM transactions_by_account WHERE account_id = ? LIMIT ?", gocql.UUID(accountID), limit).WithContext(ctx).Iter()
	var ids []gocql.UUID
	var id gocql.UUID
	for iter.Scan(&id) {
		ids = append(ids, id)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}

	txns := make([]*models.Transaction, 0, len(ids))
	for _, id := range ids {
		tx, err := r.Get(ctx, uuid.UUID(id))
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		txns = append(txns, tx)
	}
	return txns, nil
}

func (r *TransactionRepository) Transition(ctx context.Context, id uuid.UUID, from, to models.TransactionStatus, patch models.Patch) (bool, error) {
	assignments := []string{"status = ?", "updated_at = ?"}
	values := []interface{}{string(to), patch.UpdatedAt}
	if patch.FailureReason != nil {
		assignments = append(assignments, "failure_reason = ?")
		values = append(values, *patch.FailureReason)
	}
	if patch.FromBalanceCents != nil {
		assignments = append(assignments, "from_balance_cents = ?")
		values = append(values, *patch.FromBalanceCents)
	}
	if patch.ToBalanceCents != nil {
		assignments = append(assignments, "to_balance_cents = ?")
		values = append(values, *patch.ToBalanceCents)
	}
	if patch.CompletedAt != nil {
		assignments = append(assignments, "completed_at = ?")
		values = append(values, *patch.CompletedAt)
	}
	values = append(values, gocql.UUID(id), string(from))

	applied, err := r.session.Query("UPDATE transactions SET "+strings.Join(assignments, ", ")+" WHERE transaction_id = ? IF status = ?", values...).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return applied, cassandra.MapWriteError(err)
}

func scanTransaction(query *gocql.Query) (*models.Transaction, error) {
	var (
		id, accountID                gocql.UUID
		counterparty                 *gocql.UUID
		kind, status, currency       string
		description, key, reason     string
		amount                       int64
		fromBalance, toBalance       *int64
		createdAt, updatedAt, closed time.Time
	)
	err := query.Scan(&id, &kind, &status, &accountID, &counterparty, &amount, &currency, &description, &key, &reason, &fromBalance, &toBalance, &createdAt, &updatedAt, &closed)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	tx := &models.Transaction{
		ID:               uuid.UUID(id),
		Type:             models.TransactionType(kind),
		Status:           models.TransactionStatus(status),
		AccountID:        uuid.UUID(accountID),
		AmountCents:      amount,
		Currency:         currency,
		Description:      description,
		IdempotencyKey:   key,
		FailureReason:    reason,
		FromBalanceCents: fromBalance,
		ToBalanceCents:   toBalance,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
	}
	if counterparty != nil {
		value := uuid.UUID(*counterparty)
		tx.CounterpartyID = &value
	}
	if !closed.IsZero() {
		tx.CompletedAt = &closed
	}
	return tx, nil
}

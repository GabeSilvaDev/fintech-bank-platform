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

const transactionIDChunkSize = 100

const (
	openBuckets = "0123456789abcdef"
	openInsert  = "INSERT INTO open_transactions (bucket, transaction_id) VALUES (?, ?)"
	openDelete  = "DELETE FROM open_transactions WHERE bucket = ? AND transaction_id = ?"
)

func openBucket(id gocql.UUID) string {
	return id.String()[:1]
}

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
	if !tx.Status.Terminal() {
		batch = batch.Query(openInsert, openBucket(gocql.UUID(tx.ID)), gocql.UUID(tx.ID))
	}
	return cassandra.MapWriteError(batch.Exec())
}

func (r *TransactionRepository) ReserveKey(ctx context.Context, accountID uuid.UUID, key string, id uuid.UUID) (uuid.UUID, error) {
	existing := map[string]interface{}{}
	applied, err := r.session.Query("INSERT INTO transactions_by_account_key (account_id, idempotency_key, transaction_id) VALUES (?, ?, ?) IF NOT EXISTS", gocql.UUID(accountID), key, gocql.UUID(id)).
		WithContext(ctx).MapScanCAS(existing)
	if err != nil {
		return uuid.Nil, cassandra.MapWriteError(err)
	}
	if applied {
		return id, nil
	}
	return uuid.UUID(existing["transaction_id"].(gocql.UUID)), nil
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
	if len(ids) == 0 {
		return []*models.Transaction{}, nil
	}

	byID := make(map[gocql.UUID]*models.Transaction, len(ids))
	for start := 0; start < len(ids); start += transactionIDChunkSize {
		if err := r.loadChunk(ctx, idChunk(ids, start), byID); err != nil {
			return nil, err
		}
	}

	txns := make([]*models.Transaction, 0, len(ids))
	for _, id := range ids {
		if tx, ok := byID[id]; ok {
			txns = append(txns, tx)
		}
	}
	return txns, nil
}

func idChunk(ids []gocql.UUID, start int) []gocql.UUID {
	end := start + transactionIDChunkSize
	if end > len(ids) {
		end = len(ids)
	}
	return ids[start:end]
}

func (r *TransactionRepository) loadChunk(ctx context.Context, ids []gocql.UUID, into map[gocql.UUID]*models.Transaction) error {
	iter := r.session.Query("SELECT "+transactionColumns+" FROM transactions WHERE transaction_id IN ?", ids).WithContext(ctx).Iter()
	for {
		tx, ok := scanTransactionIter(iter)
		if !ok {
			break
		}
		into[gocql.UUID(tx.ID)] = tx
	}
	return iter.Close()
}

func (r *TransactionRepository) ListStale(ctx context.Context, before time.Time, maxAge time.Duration, limit int) ([]*models.Transaction, error) {
	ids, err := r.openIDs(ctx)
	if err != nil {
		return nil, err
	}

	stale := []*models.Transaction{}
	for start := 0; start < len(ids) && len(stale) < limit; start += transactionIDChunkSize {
		chunk := idChunk(ids, start)
		byID := make(map[gocql.UUID]*models.Transaction, len(chunk))
		if err := r.loadChunk(ctx, chunk, byID); err != nil {
			return nil, err
		}
		for _, id := range chunk {
			if len(stale) >= limit {
				break
			}
			tx, ok := byID[id]
			if !ok || tx.Status.Terminal() {
				r.closeOpen(ctx, id)
				continue
			}
			if tx.UpdatedAt.Before(before) && !tx.UpdatedAt.After(tx.CreatedAt.Add(maxAge)) {
				stale = append(stale, tx)
			}
		}
	}
	return stale, nil
}

func (r *TransactionRepository) openIDs(ctx context.Context) ([]gocql.UUID, error) {
	var ids []gocql.UUID
	for _, bucket := range openBuckets {
		iter := r.session.Query("SELECT transaction_id FROM open_transactions WHERE bucket = ?", string(bucket)).WithContext(ctx).Iter()
		var id gocql.UUID
		for iter.Scan(&id) {
			ids = append(ids, id)
		}
		if err := iter.Close(); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func (r *TransactionRepository) closeOpen(ctx context.Context, id gocql.UUID) {
	_ = r.session.Query(openDelete, openBucket(id), id).WithContext(ctx).Exec()
}

func (r *TransactionRepository) Reindex(ctx context.Context) (int, error) {
	iter := r.session.Query("SELECT transaction_id, status FROM transactions").WithContext(ctx).PageSize(500).Iter()
	ensured := 0
	var id gocql.UUID
	var status string
	for iter.Scan(&id, &status) {
		if models.TransactionStatus(status).Terminal() {
			continue
		}
		if err := r.session.Query(openInsert, openBucket(id), id).WithContext(ctx).Exec(); err != nil {
			_ = iter.Close()
			return ensured, err
		}
		ensured++
	}
	if err := iter.Close(); err != nil {
		return ensured, err
	}
	return ensured, nil
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
	if err == nil && applied && to.Terminal() {
		r.closeOpen(ctx, gocql.UUID(id))
	}
	return applied, cassandra.MapWriteError(err)
}

func (r *TransactionRepository) Touch(ctx context.Context, id uuid.UUID, status models.TransactionStatus, observed, now time.Time) (bool, error) {
	applied, err := r.session.Query("UPDATE transactions SET updated_at = ? WHERE transaction_id = ? IF status = ? AND updated_at = ?", now, gocql.UUID(id), string(status), observed).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return applied, cassandra.MapWriteError(err)
}

type transactionRow struct {
	id, accountID                gocql.UUID
	counterparty                 *gocql.UUID
	kind, status, currency       string
	description, key, reason     string
	amount                       int64
	fromBalance, toBalance       *int64
	createdAt, updatedAt, closed time.Time
}

func (r *transactionRow) targets() []interface{} {
	return []interface{}{&r.id, &r.kind, &r.status, &r.accountID, &r.counterparty, &r.amount, &r.currency, &r.description, &r.key, &r.reason, &r.fromBalance, &r.toBalance, &r.createdAt, &r.updatedAt, &r.closed}
}

func (r *transactionRow) model() *models.Transaction {
	tx := &models.Transaction{
		ID:               uuid.UUID(r.id),
		Type:             models.TransactionType(r.kind),
		Status:           models.TransactionStatus(r.status),
		AccountID:        uuid.UUID(r.accountID),
		AmountCents:      r.amount,
		Currency:         r.currency,
		Description:      r.description,
		IdempotencyKey:   r.key,
		FailureReason:    r.reason,
		FromBalanceCents: r.fromBalance,
		ToBalanceCents:   r.toBalance,
		CreatedAt:        r.createdAt,
		UpdatedAt:        r.updatedAt,
	}
	if r.counterparty != nil {
		value := uuid.UUID(*r.counterparty)
		tx.CounterpartyID = &value
	}
	if !r.closed.IsZero() {
		tx.CompletedAt = &r.closed
	}
	return tx
}

func scanTransaction(query *gocql.Query) (*models.Transaction, error) {
	var row transactionRow
	err := query.Scan(row.targets()...)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return row.model(), nil
}

func scanTransactionIter(iter *gocql.Iter) (*models.Transaction, bool) {
	var row transactionRow
	if !iter.Scan(row.targets()...) {
		return nil, false
	}
	return row.model(), true
}

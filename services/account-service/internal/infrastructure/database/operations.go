package database

import (
	"context"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/cassandra"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/google/uuid"
)

type BalanceOperationRepository struct {
	session *gocql.Session
}

func NewBalanceOperationRepository(session *gocql.Session) *BalanceOperationRepository {
	return &BalanceOperationRepository{session: session}
}

const balanceOperationColumns = "account_id, idempotency_key, kind, status, result, created_at, updated_at"

func (r *BalanceOperationRepository) Reserve(ctx context.Context, accountID uuid.UUID, key, kind string, at time.Time) (bool, error) {
	applied, err := r.session.Query("INSERT INTO balance_operations (account_id, idempotency_key, kind, status, created_at, updated_at) VALUES (?, ?, ?, 'pending', ?, ?) IF NOT EXISTS",
		gocql.UUID(accountID), key, kind, at, at).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return applied, cassandra.MapWriteError(err)
}

func (r *BalanceOperationRepository) Get(ctx context.Context, accountID uuid.UUID, key string) (*models.BalanceOperation, error) {
	var (
		id                   gocql.UUID
		storedKey            string
		kind, status, result string
		createdAt, updatedAt time.Time
	)
	err := r.session.Query("SELECT "+balanceOperationColumns+" FROM balance_operations WHERE account_id = ? AND idempotency_key = ?", gocql.UUID(accountID), key).
		WithContext(ctx).Scan(&id, &storedKey, &kind, &status, &result, &createdAt, &updatedAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &models.BalanceOperation{
		AccountID: uuid.UUID(id),
		Key:       storedKey,
		Kind:      kind,
		Status:    models.OperationStatus(status),
		Result:    result,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func (r *BalanceOperationRepository) Complete(ctx context.Context, accountID uuid.UUID, key, result string, at time.Time) error {
	return cassandra.MapWriteError(r.session.Query("UPDATE balance_operations SET status = 'done', result = ?, updated_at = ? WHERE account_id = ? AND idempotency_key = ?",
		result, at, gocql.UUID(accountID), key).WithContext(ctx).Exec())
}

func (r *BalanceOperationRepository) Release(ctx context.Context, accountID uuid.UUID, key string) error {
	_, err := r.session.Query("DELETE FROM balance_operations WHERE account_id = ? AND idempotency_key = ? IF status = 'pending'", gocql.UUID(accountID), key).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return cassandra.MapWriteError(err)
}

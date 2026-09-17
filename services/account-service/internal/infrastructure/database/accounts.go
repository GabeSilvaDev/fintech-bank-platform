package database

import (
	"context"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/google/uuid"
)

type AccountRepository struct {
	session *gocql.Session
}

func NewAccountRepository(session *gocql.Session) *AccountRepository {
	return &AccountRepository{session: session}
}

const accountColumns = "account_id, user_id, agency, number, type, status, currency, balance_cents, created_at, updated_at, closed_at"

func (r *AccountRepository) Create(ctx context.Context, account *models.Account) error {
	return MapWriteError(r.session.Batch(gocql.LoggedBatch).WithContext(ctx).
		Query("INSERT INTO accounts ("+accountColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			gocql.UUID(account.AccountID), gocql.UUID(account.UserID), account.Agency, account.Number, string(account.Type), string(account.Status), account.Currency, account.BalanceCents, account.CreatedAt, account.UpdatedAt, account.ClosedAt).
		Query("INSERT INTO accounts_by_user (user_id, account_id) VALUES (?, ?)", gocql.UUID(account.UserID), gocql.UUID(account.AccountID)).
		Exec())
}

func (r *AccountRepository) ReserveNumber(ctx context.Context, agency, number string, accountID uuid.UUID) (bool, error) {
	applied, err := r.session.Query("INSERT INTO accounts_by_number (agency, number, account_id) VALUES (?, ?, ?) IF NOT EXISTS", agency, number, gocql.UUID(accountID)).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return applied, MapWriteError(err)
}

func (r *AccountRepository) Get(ctx context.Context, accountID uuid.UUID) (*models.Account, error) {
	return scanAccount(r.session.Query("SELECT "+accountColumns+" FROM accounts WHERE account_id = ?", gocql.UUID(accountID)).WithContext(ctx))
}

func (r *AccountRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]*models.Account, error) {
	iter := r.session.Query("SELECT account_id FROM accounts_by_user WHERE user_id = ?", gocql.UUID(userID)).WithContext(ctx).Iter()
	var ids []gocql.UUID
	var id gocql.UUID
	for iter.Scan(&id) {
		ids = append(ids, id)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}

	accounts := make([]*models.Account, 0, len(ids))
	for _, id := range ids {
		account, err := scanAccount(r.session.Query("SELECT "+accountColumns+" FROM accounts WHERE account_id = ?", id).WithContext(ctx))
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func (r *AccountRepository) UpdateStatus(ctx context.Context, accountID uuid.UUID, status models.AccountStatus, updatedAt time.Time, closedAt *time.Time) error {
	return MapWriteError(r.session.Query("UPDATE accounts SET status = ?, updated_at = ?, closed_at = ? WHERE account_id = ?", string(status), updatedAt, closedAt, gocql.UUID(accountID)).
		WithContext(ctx).Exec())
}

func (r *AccountRepository) CloseIfEmpty(ctx context.Context, accountID uuid.UUID, closedAt time.Time) (bool, error) {
	applied, err := r.session.Query("UPDATE accounts SET status = ?, updated_at = ?, closed_at = ? WHERE account_id = ? IF balance_cents = 0 AND status != ?",
		string(models.AccountStatusClosed), closedAt, closedAt, gocql.UUID(accountID), string(models.AccountStatusClosed)).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return applied, MapWriteError(err)
}

func (r *AccountRepository) CompareAndSetBalance(ctx context.Context, accountID uuid.UUID, expected, next int64, updatedAt time.Time) (bool, error) {
	applied, err := r.session.Query("UPDATE accounts SET balance_cents = ?, updated_at = ? WHERE account_id = ? IF balance_cents = ? AND status = ?",
		next, updatedAt, gocql.UUID(accountID), expected, string(models.AccountStatusActive)).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return applied, MapWriteError(err)
}

func scanAccount(query *gocql.Query) (*models.Account, error) {
	var (
		accountID, userID      gocql.UUID
		agency, number         string
		kind, status, currency string
		balance                int64
		createdAt, updatedAt   time.Time
		closedAt               time.Time
	)
	err := query.Scan(&accountID, &userID, &agency, &number, &kind, &status, &currency, &balance, &createdAt, &updatedAt, &closedAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	account := &models.Account{
		AccountID:    uuid.UUID(accountID),
		UserID:       uuid.UUID(userID),
		Agency:       agency,
		Number:       number,
		Type:         models.AccountType(kind),
		Status:       models.AccountStatus(status),
		Currency:     currency,
		BalanceCents: balance,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}
	if !closedAt.IsZero() {
		account.ClosedAt = &closedAt
	}
	return account, nil
}

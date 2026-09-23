package database

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/cassandra"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/google/uuid"
)

type PaymentRepository struct {
	session *gocql.Session
}

func NewPaymentRepository(session *gocql.Session) *PaymentRepository {
	return &PaymentRepository{session: session}
}

const paymentColumns = "payment_id, account_id, method, status, amount_cents, currency, recipient, pix_key, boleto_code, ted_bank_code, ted_branch, ted_account, ted_document, description, idempotency_key, external_id, failure_reason, balance_after_cents, created_at, updated_at, completed_at"

func (r *PaymentRepository) Create(ctx context.Context, p *models.Payment) error {
	ted := p.TED
	if ted == nil {
		ted = &models.TEDDetails{}
	}
	return cassandra.MapWriteError(r.session.Batch(gocql.LoggedBatch).WithContext(ctx).
		Query("INSERT INTO payments ("+paymentColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			gocql.UUID(p.ID), gocql.UUID(p.AccountID), string(p.Method), string(p.Status), p.AmountCents, p.Currency, p.Recipient, p.PixKey, p.BoletoCode,
			ted.BankCode, ted.Branch, ted.Account, ted.Document, p.Description, p.IdempotencyKey, p.ExternalID, p.FailureReason, p.BalanceAfterCents,
			p.CreatedAt, p.UpdatedAt, p.CompletedAt).
		Query("INSERT INTO payments_by_account (account_id, created_at, payment_id) VALUES (?, ?, ?)", gocql.UUID(p.AccountID), p.CreatedAt, gocql.UUID(p.ID)).
		Exec())
}

func (r *PaymentRepository) ReserveKey(ctx context.Context, accountID uuid.UUID, key string, id uuid.UUID) (uuid.UUID, error) {
	existing := map[string]interface{}{}
	applied, err := r.session.Query("INSERT INTO payments_by_key (account_id, idempotency_key, payment_id) VALUES (?, ?, ?) IF NOT EXISTS", gocql.UUID(accountID), key, gocql.UUID(id)).
		WithContext(ctx).MapScanCAS(existing)
	if err != nil {
		return uuid.Nil, cassandra.MapWriteError(err)
	}
	if applied {
		return id, nil
	}
	return uuid.UUID(existing["payment_id"].(gocql.UUID)), nil
}

func (r *PaymentRepository) Get(ctx context.Context, id uuid.UUID) (*models.Payment, error) {
	return scanPayment(r.session.Query("SELECT "+paymentColumns+" FROM payments WHERE payment_id = ?", gocql.UUID(id)).WithContext(ctx))
}

func (r *PaymentRepository) ListByAccount(ctx context.Context, accountID uuid.UUID, limit int) ([]*models.Payment, error) {
	iter := r.session.Query("SELECT payment_id FROM payments_by_account WHERE account_id = ? LIMIT ?", gocql.UUID(accountID), limit).WithContext(ctx).Iter()
	var ids []gocql.UUID
	var id gocql.UUID
	for iter.Scan(&id) {
		ids = append(ids, id)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}

	payments := make([]*models.Payment, 0, len(ids))
	for _, id := range ids {
		payment, err := r.Get(ctx, uuid.UUID(id))
		if errors.Is(err, domain.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		payments = append(payments, payment)
	}
	return payments, nil
}

func (r *PaymentRepository) Transition(ctx context.Context, id uuid.UUID, from, to models.Status, patch models.Patch) (bool, error) {
	assignments := []string{"status = ?", "updated_at = ?"}
	values := []interface{}{string(to), patch.UpdatedAt}
	if patch.ExternalID != nil {
		assignments = append(assignments, "external_id = ?")
		values = append(values, *patch.ExternalID)
	}
	if patch.FailureReason != nil {
		assignments = append(assignments, "failure_reason = ?")
		values = append(values, *patch.FailureReason)
	}
	if patch.BalanceAfterCents != nil {
		assignments = append(assignments, "balance_after_cents = ?")
		values = append(values, *patch.BalanceAfterCents)
	}
	if patch.CompletedAt != nil {
		assignments = append(assignments, "completed_at = ?")
		values = append(values, *patch.CompletedAt)
	}
	values = append(values, gocql.UUID(id), string(from))

	applied, err := r.session.Query("UPDATE payments SET "+strings.Join(assignments, ", ")+" WHERE payment_id = ? IF status = ?", values...).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return applied, cassandra.MapWriteError(err)
}

func (r *PaymentRepository) BindExternalID(ctx context.Context, externalID string, id uuid.UUID) error {
	return cassandra.MapWriteError(r.session.Query("INSERT INTO payments_by_external_id (external_id, payment_id) VALUES (?, ?)", externalID, gocql.UUID(id)).
		WithContext(ctx).Exec())
}

func (r *PaymentRepository) FindByExternalID(ctx context.Context, externalID string) (uuid.UUID, error) {
	var id gocql.UUID
	err := r.session.Query("SELECT payment_id FROM payments_by_external_id WHERE external_id = ?", externalID).WithContext(ctx).Scan(&id)
	if errors.Is(err, gocql.ErrNotFound) {
		return uuid.Nil, domain.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.UUID(id), nil
}

func (r *PaymentRepository) ListStale(ctx context.Context, before time.Time, limit int) ([]*models.Payment, error) {
	iter := r.session.Query("SELECT " + paymentColumns + " FROM payments").WithContext(ctx).PageSize(500).Iter()
	stale := []*models.Payment{}
	for len(stale) < limit {
		payment, ok := scanPaymentIter(iter)
		if !ok {
			break
		}
		switch payment.Status {
		case models.StatusPending, models.StatusDebited, models.StatusSubmitted, models.StatusRefunding:
			if payment.UpdatedAt.Before(before) {
				stale = append(stale, payment)
			}
		}
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return stale, nil
}

func (r *PaymentRepository) Touch(ctx context.Context, id uuid.UUID, status models.Status, observed, now time.Time) (bool, error) {
	applied, err := r.session.Query("UPDATE payments SET updated_at = ? WHERE payment_id = ? IF status = ? AND updated_at = ?", now, gocql.UUID(id), string(status), observed).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return applied, cassandra.MapWriteError(err)
}

type paymentRow struct {
	id, accountID                        gocql.UUID
	method, status, currency, recipient  string
	pixKey, boletoCode                   string
	bankCode, branch, account, document  string
	description, key, externalID, reason string
	amount                               int64
	balance                              *int64
	createdAt, updatedAt, completedAt    time.Time
}

func (r *paymentRow) targets() []interface{} {
	return []interface{}{&r.id, &r.accountID, &r.method, &r.status, &r.amount, &r.currency, &r.recipient, &r.pixKey, &r.boletoCode,
		&r.bankCode, &r.branch, &r.account, &r.document, &r.description, &r.key, &r.externalID, &r.reason, &r.balance,
		&r.createdAt, &r.updatedAt, &r.completedAt}
}

func (r *paymentRow) model() *models.Payment {
	payment := &models.Payment{
		ID:                uuid.UUID(r.id),
		AccountID:         uuid.UUID(r.accountID),
		Method:            models.Method(r.method),
		Status:            models.Status(r.status),
		AmountCents:       r.amount,
		Currency:          r.currency,
		Recipient:         r.recipient,
		PixKey:            r.pixKey,
		BoletoCode:        r.boletoCode,
		Description:       r.description,
		IdempotencyKey:    r.key,
		ExternalID:        r.externalID,
		FailureReason:     r.reason,
		BalanceAfterCents: r.balance,
		CreatedAt:         r.createdAt,
		UpdatedAt:         r.updatedAt,
	}
	if r.bankCode != "" {
		payment.TED = &models.TEDDetails{BankCode: r.bankCode, Branch: r.branch, Account: r.account, Document: r.document}
	}
	if !r.completedAt.IsZero() {
		payment.CompletedAt = &r.completedAt
	}
	return payment
}

func scanPayment(query *gocql.Query) (*models.Payment, error) {
	var row paymentRow
	err := query.Scan(row.targets()...)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return row.model(), nil
}

func scanPaymentIter(iter *gocql.Iter) (*models.Payment, bool) {
	var row paymentRow
	if !iter.Scan(row.targets()...) {
		return nil, false
	}
	return row.model(), true
}

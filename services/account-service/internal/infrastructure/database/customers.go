package database

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/google/uuid"
)

type CustomerRepository struct {
	session *gocql.Session
}

func NewCustomerRepository(session *gocql.Session) *CustomerRepository {
	return &CustomerRepository{session: session}
}

func (r *CustomerRepository) Upsert(ctx context.Context, customer *models.Customer) error {
	return MapWriteError(r.session.Query("INSERT INTO customers (user_id, name, email, document, phone, kyc_status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		gocql.UUID(customer.UserID), customer.Name, customer.Email, customer.Document, customer.Phone, customer.KYCStatus, customer.CreatedAt, customer.UpdatedAt).
		WithContext(ctx).Exec())
}

func (r *CustomerRepository) Get(ctx context.Context, userID uuid.UUID) (*models.Customer, error) {
	var (
		id                                      gocql.UUID
		name, email, document, phone, kycStatus string
		createdAt, updatedAt                    time.Time
	)
	err := r.session.Query("SELECT user_id, name, email, document, phone, kyc_status, created_at, updated_at FROM customers WHERE user_id = ?", gocql.UUID(userID)).
		WithContext(ctx).Scan(&id, &name, &email, &document, &phone, &kycStatus, &createdAt, &updatedAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &models.Customer{UserID: uuid.UUID(id), Name: name, Email: email, Document: document, Phone: phone, KYCStatus: kycStatus, CreatedAt: createdAt, UpdatedAt: updatedAt}, nil
}

func (r *CustomerRepository) UpdateProfile(ctx context.Context, userID uuid.UUID, name, email, phone *string, updatedAt time.Time) error {
	assignments := []string{"updated_at = ?"}
	values := []interface{}{updatedAt}
	if name != nil {
		assignments = append(assignments, "name = ?")
		values = append(values, *name)
	}
	if email != nil {
		assignments = append(assignments, "email = ?")
		values = append(values, *email)
	}
	if phone != nil {
		assignments = append(assignments, "phone = ?")
		values = append(values, *phone)
	}
	values = append(values, gocql.UUID(userID))

	return MapWriteError(r.session.Query("UPDATE customers SET "+strings.Join(assignments, ", ")+" WHERE user_id = ?", values...).WithContext(ctx).Exec())
}

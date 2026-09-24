package database

import (
	"context"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/cassandra"
	"github.com/fintech-bank-platform/pkg/domain"
)

type LoginFailureRepository struct {
	session *gocql.Session
}

func NewLoginFailureRepository(session *gocql.Session) *LoginFailureRepository {
	return &LoginFailureRepository{session: session}
}

func (r *LoginFailureRepository) Get(ctx context.Context, email string) (*models.LoginFailure, error) {
	var (
		failures     int
		firstFailure time.Time
	)
	err := r.session.Query("SELECT failures, first_failure FROM login_failures WHERE email = ?", email).
		WithContext(ctx).Scan(&failures, &firstFailure)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &models.LoginFailure{Email: email, Failures: failures, FirstFailure: firstFailure}, nil
}

func (r *LoginFailureRepository) Create(ctx context.Context, failure *models.LoginFailure, ttl time.Duration) (bool, error) {
	applied, err := r.session.Query("INSERT INTO login_failures (email, failures, first_failure) VALUES (?, ?, ?) IF NOT EXISTS USING TTL ?",
		failure.Email, failure.Failures, failure.FirstFailure, ttlSeconds(ttl)).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return applied, cassandra.MapWriteError(err)
}

func (r *LoginFailureRepository) Replace(ctx context.Context, current, next *models.LoginFailure, ttl time.Duration) (bool, error) {
	applied, err := r.session.Query("UPDATE login_failures USING TTL ? SET failures = ?, first_failure = ? WHERE email = ? IF failures = ? AND first_failure = ?",
		ttlSeconds(ttl), next.Failures, next.FirstFailure, current.Email, current.Failures, current.FirstFailure).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return applied, cassandra.MapWriteError(err)
}

func (r *LoginFailureRepository) Clear(ctx context.Context, email string) error {
	_, err := r.session.Query("DELETE FROM login_failures WHERE email = ? IF EXISTS", email).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return cassandra.MapWriteError(err)
}

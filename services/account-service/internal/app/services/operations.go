package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/google/uuid"
)

const releaseTimeout = 5 * time.Second

func (s *AccountService) once(ctx context.Context, accountID uuid.UUID, key, kind string, result interface{}, apply func() error) error {
	reserved, err := s.operations.Reserve(ctx, accountID, key, kind, s.clock.Now())
	if err != nil {
		return err
	}
	if !reserved {
		operation, err := s.operations.Get(ctx, accountID, key)
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("%w: operation %s was released concurrently", domain.ErrConflict, key)
		}
		if err != nil {
			return err
		}
		if operation.Kind != kind {
			return domain.Invalid("idempotency_key_reused", "idempotency_key was used for another operation")
		}
		if operation.Status != models.OperationDone {
			return fmt.Errorf("%w: operation %s is still pending", domain.ErrAmbiguousWrite, key)
		}
		return json.Unmarshal([]byte(operation.Result), result)
	}

	if err := apply(); err != nil {
		if !errors.Is(err, domain.ErrAmbiguousWrite) {
			if releaseErr := s.release(ctx, accountID, key); releaseErr != nil {
				return releaseErr
			}
		}
		return err
	}
	raw, _ := json.Marshal(result)
	_ = s.operations.Complete(ctx, accountID, key, string(raw), s.clock.Now())
	return nil
}

func (s *AccountService) release(ctx context.Context, accountID uuid.UUID, key string) error {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()
	return s.operations.Release(releaseCtx, accountID, key)
}

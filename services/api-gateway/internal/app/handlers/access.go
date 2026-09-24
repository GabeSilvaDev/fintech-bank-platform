package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/auth"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxGuardedBodyBytes = 1 << 20

type AccessGuard struct {
	owners contracts.AccountOwners
}

func NewAccessGuard(owners contracts.AccountOwners) *AccessGuard {
	return &AccessGuard{owners: owners}
}

func forbidden() *apperrors.AppError {
	return apperrors.Forbidden("FORBIDDEN", "access to this resource is not allowed")
}

func (g *AccessGuard) RequireSelf(param string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := caller(r.Context())
			if err == nil {
				err = sameUser(userID, chi.URLParam(r, param))
			}
			if err != nil {
				response.FromError(w, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (g *AccessGuard) RequireAccountOwner(param string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := chi.URLParam(r, param)
			decoded, err := url.PathUnescape(raw)
			var accountID uuid.UUID
			if err == nil {
				accountID, err = uuid.Parse(decoded)
			}
			if err != nil {
				response.FromError(w, invalidAccountID(param))
				return
			}
			if err := g.authorize(r.Context(), accountID); err != nil {
				response.FromError(w, err)
				return
			}
			if decoded != raw {
				response.FromError(w, invalidAccountID(param))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func invalidAccountID(param string) *apperrors.AppError {
	return apperrors.UnprocessableEntity("VALIDATION_ERROR", "request validation failed").WithDetail(param, "uuid")
}

func (g *AccessGuard) AuthorizeAccount(ctx context.Context, rawAccountID string) error {
	accountID, err := uuid.Parse(rawAccountID)
	if err != nil {
		return forbidden()
	}
	return g.authorize(ctx, accountID)
}

func (g *AccessGuard) authorize(ctx context.Context, accountID uuid.UUID) error {
	err := g.checkOwner(ctx, accountID)
	if errors.Is(err, contracts.ErrAccountNotFound) {
		return apperrors.NotFound("ACCOUNT_NOT_FOUND", "account not found")
	}
	return err
}

func (g *AccessGuard) ResolveUser(ctx context.Context, requested string) (string, error) {
	userID, err := caller(ctx)
	if err != nil {
		return "", err
	}
	if requested != "" {
		if err := sameUser(userID, requested); err != nil {
			return "", err
		}
	}
	return userID.String(), nil
}

func (g *AccessGuard) releaseOwned(resp *http.Response, fields []string) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxGuardedBodyBytes+1))
	_ = resp.Body.Close()
	if err != nil {
		return err
	}
	if len(body) > maxGuardedBodyBytes {
		return fmt.Errorf("upstream response exceeds %d bytes", maxGuardedBodyBytes)
	}

	var envelope struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return err
	}

	allowed, err := g.ownsAny(resp.Request.Context(), envelope.Data, fields)
	if err != nil {
		return err
	}
	if !allowed {
		return forbidden()
	}

	resp.Body = io.NopCloser(bytes.NewReader(body))
	return nil
}

func (g *AccessGuard) ownsAny(ctx context.Context, data map[string]interface{}, fields []string) (bool, error) {
	userID, err := caller(ctx)
	if err != nil {
		return false, err
	}

	for _, field := range fields {
		raw, _ := data[field].(string)
		accountID, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		owner, err := g.owners.Owner(ctx, accountID)
		if errors.Is(err, contracts.ErrAccountNotFound) {
			continue
		}
		if err != nil {
			return false, err
		}
		if owner == userID {
			return true, nil
		}
	}
	return false, nil
}

func (g *AccessGuard) checkOwner(ctx context.Context, accountID uuid.UUID) error {
	userID, err := caller(ctx)
	if err != nil {
		return err
	}

	owner, err := g.owners.Owner(ctx, accountID)
	if err != nil {
		return err
	}
	if owner != userID {
		return forbidden()
	}
	return nil
}

func caller(ctx context.Context) (uuid.UUID, error) {
	userID, ok := auth.UserID(ctx)
	if !ok {
		return uuid.Nil, forbidden()
	}
	return userID, nil
}

func sameUser(userID uuid.UUID, raw string) error {
	requested, err := uuid.Parse(raw)
	if err != nil || requested != userID {
		return forbidden()
	}
	return nil
}

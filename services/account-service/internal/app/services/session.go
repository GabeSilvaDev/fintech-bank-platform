package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/contracts"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/google/uuid"
)

const (
	DefaultRefreshTokenTTL = 720 * time.Hour
	refreshTokenBytes      = 32
)

var ErrInvalidSession = errors.New("invalid session")

type Session struct {
	Token     string
	ExpiresAt time.Time
}

type SessionService struct {
	tokens contracts.RefreshTokenRepository
	clock  contracts.Clock
	ttl    time.Duration
}

func NewSessionService(tokens contracts.RefreshTokenRepository, clock contracts.Clock, ttl time.Duration) *SessionService {
	if ttl <= 0 {
		ttl = DefaultRefreshTokenTTL
	}
	return &SessionService{tokens: tokens, clock: clock, ttl: ttl}
}

func (s *SessionService) Start(ctx context.Context, userID uuid.UUID) (Session, error) {
	return s.issue(ctx, userID, uuid.New())
}

func (s *SessionService) Rotate(ctx context.Context, token string) (uuid.UUID, Session, error) {
	current, err := s.lookup(ctx, token)
	if err != nil {
		return uuid.Nil, Session{}, err
	}
	if !s.clock.Now().Before(current.ExpiresAt) {
		return uuid.Nil, Session{}, ErrInvalidSession
	}
	if current.Status != models.RefreshTokenActive {
		return uuid.Nil, Session{}, s.revokeFamily(ctx, current.FamilyID)
	}

	next, err := s.issue(ctx, current.UserID, current.FamilyID)
	if err != nil {
		return uuid.Nil, Session{}, err
	}
	rotated, err := s.tokens.MarkRotated(ctx, current.TokenHash)
	if err != nil {
		return uuid.Nil, Session{}, err
	}
	if !rotated {
		return uuid.Nil, Session{}, s.revokeFamily(ctx, current.FamilyID)
	}
	return current.UserID, next, nil
}

func (s *SessionService) Revoke(ctx context.Context, token string) error {
	current, err := s.lookup(ctx, token)
	if errors.Is(err, ErrInvalidSession) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.tokens.RevokeFamily(ctx, current.FamilyID)
}

func (s *SessionService) lookup(ctx context.Context, token string) (*models.RefreshToken, error) {
	hash, ok := hashRefreshToken(token)
	if !ok {
		return nil, ErrInvalidSession
	}
	current, err := s.tokens.Get(ctx, hash)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, ErrInvalidSession
	}
	return current, err
}

func (s *SessionService) issue(ctx context.Context, userID, familyID uuid.UUID) (Session, error) {
	token := newRefreshToken()
	now := s.clock.Now().UTC().Truncate(time.Millisecond)
	record := &models.RefreshToken{
		TokenHash: digestRefreshToken(token),
		UserID:    userID,
		FamilyID:  familyID,
		Status:    models.RefreshTokenActive,
		ExpiresAt: now.Add(s.ttl),
		CreatedAt: now,
	}
	if err := s.tokens.Create(ctx, record, s.ttl); err != nil {
		return Session{}, err
	}
	return Session{Token: token, ExpiresAt: record.ExpiresAt}, nil
}

func (s *SessionService) revokeFamily(ctx context.Context, familyID uuid.UUID) error {
	if err := s.tokens.RevokeFamily(ctx, familyID); err != nil {
		return err
	}
	return ErrInvalidSession
}

func newRefreshToken() string {
	raw := make([]byte, refreshTokenBytes)
	rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func hashRefreshToken(token string) (string, bool) {
	if len(token) != base64.RawURLEncoding.EncodedLen(refreshTokenBytes) {
		return "", false
	}
	if _, err := base64.RawURLEncoding.Strict().DecodeString(token); err != nil {
		return "", false
	}
	return digestRefreshToken(token), true
}

func digestRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

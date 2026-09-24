package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	IssuerName = "fintech-gateway"
	TokenType  = "Bearer"
	leeway     = 30 * time.Second
)

var ErrInvalidToken = errors.New("invalid access token")

type Issuer struct {
	secret []byte
	ttl    time.Duration
}

func NewIssuer(secret string, ttl time.Duration) *Issuer {
	return &Issuer{secret: []byte(secret), ttl: ttl}
}

func (i *Issuer) Issue(userID uuid.UUID) (string, int, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		Issuer:    IssuerName,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(i.ttl)),
		ID:        uuid.NewString(),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
	return token, int(i.ttl / time.Second), err
}

type Verifier struct {
	secret []byte
	parser *jwt.Parser
}

func NewVerifier(secret string) *Verifier {
	return &Verifier{
		secret: []byte(secret),
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithIssuer(IssuerName),
			jwt.WithLeeway(leeway),
			jwt.WithExpirationRequired(),
		),
	}
}

func (v *Verifier) Verify(token string) (uuid.UUID, error) {
	var claims jwt.RegisteredClaims
	if _, err := v.parser.ParseWithClaims(token, &claims, v.key); err != nil {
		return uuid.Nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil || userID.String() != claims.Subject {
		return uuid.Nil, fmt.Errorf("%w: subject is not a user id", ErrInvalidToken)
	}
	return userID, nil
}

func (v *Verifier) key(*jwt.Token) (interface{}, error) {
	return v.secret, nil
}

package unit

import (
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/auth"
	"github.com/fintech-bank-platform/api-gateway/tests"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func signToken(t *testing.T, method jwt.SigningMethod, key interface{}, claims jwt.Claims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(method, claims).SignedString(key)
	require.NoError(t, err)
	return token
}

func validClaims(userID string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"sub": userID,
		"iss": auth.IssuerName,
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
		"jti": uuid.NewString(),
	}
}

func TestIssuerIssuesATokenTheVerifierAccepts(t *testing.T) {
	userID := uuid.New()

	token, expiresIn, err := auth.NewIssuer(tests.JWTSecret, 90*time.Minute).Issue(userID)
	require.NoError(t, err)
	assert.Equal(t, 5400, expiresIn)

	got, err := auth.NewVerifier(tests.JWTSecret).Verify(token)
	require.NoError(t, err)
	assert.Equal(t, userID, got)
}

func TestIssuerSetsTheExpectedClaims(t *testing.T) {
	userID := uuid.New()
	before := time.Now().Add(-time.Second)

	token, _, err := auth.NewIssuer(tests.JWTSecret, time.Hour).Issue(userID)
	require.NoError(t, err)

	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (interface{}, error) { return []byte(tests.JWTSecret), nil })
	require.NoError(t, err)
	assert.Equal(t, "HS256", parsed.Header["alg"])
	assert.Equal(t, userID.String(), claims["sub"])
	assert.Equal(t, "fintech-gateway", claims["iss"])
	_, err = uuid.Parse(claims["jti"].(string))
	assert.NoError(t, err)

	issuedAt, err := claims.GetIssuedAt()
	require.NoError(t, err)
	expiresAt, err := claims.GetExpirationTime()
	require.NoError(t, err)
	assert.False(t, issuedAt.Time.Before(before.Truncate(time.Second)))
	assert.Equal(t, time.Hour, expiresAt.Sub(issuedAt.Time))
}

func TestIssuerGivesEveryTokenItsOwnID(t *testing.T) {
	issuer := auth.NewIssuer(tests.JWTSecret, time.Hour)
	userID := uuid.New()

	first, _, _ := issuer.Issue(userID)
	second, _, _ := issuer.Issue(userID)

	assert.NotEqual(t, first, second)
}

func TestVerifierRejectsInvalidTokens(t *testing.T) {
	secret := []byte(tests.JWTSecret)
	userID := uuid.NewString()

	expired := validClaims(userID)
	expired["exp"] = time.Now().Add(-2 * time.Minute).Unix()

	wrongIssuer := validClaims(userID)
	wrongIssuer["iss"] = "someone-else"

	noIssuer := validClaims(userID)
	delete(noIssuer, "iss")

	noExpiry := validClaims(userID)
	delete(noExpiry, "exp")

	notAUser := validClaims("not-a-uuid")

	notCanonical := validClaims(uuid.New().String())
	notCanonical["sub"] = "{" + notCanonical["sub"].(string) + "}"

	noSubject := validClaims(userID)
	delete(noSubject, "sub")

	notYetValid := validClaims(userID)
	notYetValid["nbf"] = time.Now().Add(5 * time.Minute).Unix()

	cases := map[string]string{
		"expired beyond leeway": signToken(t, jwt.SigningMethodHS256, secret, expired),
		"wrong secret":          signToken(t, jwt.SigningMethodHS256, []byte("another-secret-of-at-least-32-bytes!!"), validClaims(userID)),
		"alg none":              signToken(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, validClaims(userID)),
		"HS512":                 signToken(t, jwt.SigningMethodHS512, secret, validClaims(userID)),
		"wrong issuer":          signToken(t, jwt.SigningMethodHS256, secret, wrongIssuer),
		"missing issuer":        signToken(t, jwt.SigningMethodHS256, secret, noIssuer),
		"missing exp":           signToken(t, jwt.SigningMethodHS256, secret, noExpiry),
		"non uuid subject":      signToken(t, jwt.SigningMethodHS256, secret, notAUser),
		"non canonical subject": signToken(t, jwt.SigningMethodHS256, secret, notCanonical),
		"missing subject":       signToken(t, jwt.SigningMethodHS256, secret, noSubject),
		"not yet valid":         signToken(t, jwt.SigningMethodHS256, secret, notYetValid),
		"malformed":             "not.a.jwt",
		"empty":                 "",
	}

	verifier := auth.NewVerifier(tests.JWTSecret)
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			userID, err := verifier.Verify(token)

			assert.ErrorIs(t, err, auth.ErrInvalidToken)
			assert.Equal(t, uuid.Nil, userID)
		})
	}
}

func TestVerifierRejectsATamperedPayload(t *testing.T) {
	token, _, err := auth.NewIssuer(tests.JWTSecret, time.Hour).Issue(uuid.New())
	require.NoError(t, err)

	forged := signToken(t, jwt.SigningMethodHS256, []byte(tests.JWTSecret), validClaims(uuid.NewString()))
	parts := strings.Split(token, ".")
	forgedParts := strings.Split(forged, ".")

	_, err = auth.NewVerifier(tests.JWTSecret).Verify(parts[0] + "." + forgedParts[1] + "." + parts[2])

	assert.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestVerifierAcceptsATokenExpiredWithinTheLeeway(t *testing.T) {
	userID := uuid.New()
	claims := validClaims(userID.String())
	claims["exp"] = time.Now().Add(-10 * time.Second).Unix()

	got, err := auth.NewVerifier(tests.JWTSecret).Verify(signToken(t, jwt.SigningMethodHS256, []byte(tests.JWTSecret), claims))

	require.NoError(t, err)
	assert.Equal(t, userID, got)
}

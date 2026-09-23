package unit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	"github.com/stretchr/testify/assert"
)

func TestSignMatchesHmacSha256(t *testing.T) {
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write([]byte("1700000000.{\"a\":1}"))
	assert.Equal(t, hex.EncodeToString(mac.Sum(nil)), services.Sign("s3cret", "1700000000", []byte(`{"a":1}`)))
}

func TestVerify(t *testing.T) {
	body := []byte(`{"external_id":"ted_1","status":"settled"}`)
	ts := strconv.FormatInt(now.Unix(), 10)
	signature := services.Sign("s3cret", ts, body)

	assert.NoError(t, services.Verify("s3cret", ts, body, signature, now, 5*time.Minute))
	assert.NoError(t, services.Verify("s3cret", ts, body, strings.ToUpper(signature), now.Add(4*time.Minute), 5*time.Minute))

	failures := []error{
		services.Verify("other", ts, body, signature, now, 5*time.Minute),
		services.Verify("s3cret", ts, []byte(`{"external_id":"ted_2","status":"settled"}`), signature, now, 5*time.Minute),
		services.Verify("s3cret", "abc", body, signature, now, 5*time.Minute),
		services.Verify("s3cret", "", body, signature, now, 5*time.Minute),
		services.Verify("s3cret", ts, body, "", now, 5*time.Minute),
		services.Verify("s3cret", ts, body, signature, now.Add(6*time.Minute), 5*time.Minute),
		services.Verify("s3cret", ts, body, signature, now.Add(-6*time.Minute), 5*time.Minute),
	}
	for i, err := range failures {
		assert.ErrorIs(t, err, services.ErrInvalidSignature, i)
	}
}

package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidSignature = errors.New("invalid webhook signature")

func Sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func Verify(secret, timestamp string, body []byte, signature string, now time.Time, tolerance time.Duration) error {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrInvalidSignature
	}
	age := now.Sub(time.Unix(seconds, 0))
	if age > tolerance || age < -tolerance {
		return ErrInvalidSignature
	}
	if !hmac.Equal([]byte(Sign(secret, timestamp, body)), []byte(strings.ToLower(signature))) {
		return ErrInvalidSignature
	}
	return nil
}

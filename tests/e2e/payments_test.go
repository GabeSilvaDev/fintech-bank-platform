//go:build e2e

package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	settledBoleto  = "34191790010100000012334567812309811000000015000"
	rejectedBoleto = "99990000040000000000000000000018111000000005000"
)

func pay(t *testing.T, accountID string, fields map[string]interface{}) string {
	t.Helper()
	k := key()
	body := map[string]interface{}{
		"account_id":      accountID,
		"currency":        "BRL",
		"recipient":       "Destinatário E2E",
		"description":     "e2e payment",
		"idempotency_key": k,
	}
	for name, value := range fields {
		body[name] = value
	}
	accepted(t, "/api/v1/payments", body)
	return k
}

func ted(bankCode string) map[string]interface{} {
	return map[string]interface{}{"bank_code": bankCode, "branch": "1234", "account": "123456", "document": "52998224725"}
}

func TestPaymentSandboxPaths(t *testing.T) {
	t.Parallel()
	_, accountID := newCustomer(t, "")
	deposit(t, accountID, "2000.00")

	pix := pay(t, accountID, map[string]interface{}{"payment_method": "pix", "amount": "42.50", "pix_key": "ana@example.com"})
	pixRejected := pay(t, accountID, map[string]interface{}{"payment_method": "pix", "amount": "10.00", "pix_key": "reject@reject.test"})
	tedSettled := pay(t, accountID, map[string]interface{}{"payment_method": "ted", "amount": "100.00", "ted": ted("341")})
	tedRejected := pay(t, accountID, map[string]interface{}{"payment_method": "ted", "amount": "50.00", "ted": ted("999")})
	boleto := pay(t, accountID, map[string]interface{}{"payment_method": "boleto", "amount": "150.00", "boleto_code": settledBoleto})
	boletoRejected := pay(t, accountID, map[string]interface{}{"payment_method": "boleto", "amount": "50.00", "boleto_code": rejectedBoleto})

	cases := []struct {
		name   string
		key    string
		status string
		reason string
		prefix string
	}{
		{"pix", pix, "completed", "", "pix_"},
		{"pix rejected", pixRejected, "refunded", "pix_key_not_found", ""},
		{"ted", tedSettled, "completed", "", "ted_"},
		{"ted rejected", tedRejected, "refunded", "invalid_destination", "ted_"},
		{"boleto", boleto, "completed", "", "boleto_"},
		{"boleto rejected", boletoRejected, "refunded", "boleto_not_found", "boleto_"},
	}
	for _, c := range cases {
		p := payment(t, accountID, c.key)
		require.Equal(t, c.status, p["status"], "%s: %v", c.name, p)
		if c.reason != "" {
			require.Equal(t, c.reason, p["failure_reason"], "%s: %v", c.name, p)
		}
		if c.prefix != "" {
			externalID, _ := p["external_id"].(string)
			require.True(t, strings.HasPrefix(externalID, c.prefix), "%s external_id %q", c.name, externalID)
			require.Equal(t, c.prefix+p["payment_id"].(string), externalID)
		}
	}

	require.Equal(t, "1707.50", balance(t, accountID))

	require.Equal(t, "42.50", payment(t, accountID, pix)["amount"])

	tedPayment := payment(t, accountID, tedSettled)
	require.Equal(t, "100.00", tedPayment["amount"])
	tedDetails, ok := tedPayment["ted"].(map[string]interface{})
	require.True(t, ok, "ted payment has no ted details: %v", tedPayment)
	require.Equal(t, "*********25", tedDetails["document"])

	status, rejection := postRejected(t, "/api/v1/payments", map[string]interface{}{
		"account_id":      accountID,
		"payment_method":  "boleto",
		"amount":          "149.99",
		"currency":        "BRL",
		"recipient":       "Destinatário E2E",
		"boleto_code":     settledBoleto,
		"idempotency_key": key(),
	})
	require.Equal(t, http.StatusUnprocessableEntity, status)
	require.Equal(t, "VALIDATION_ERROR", rejection.Code)
	require.Equal(t, map[string]string{"amount": "boleto_amount"}, rejection.Details)

	invalidBoleto := settledBoleto[:len(settledBoleto)-1] + "1"
	status, rejection = postRejected(t, "/api/v1/payments", map[string]interface{}{
		"account_id":      accountID,
		"payment_method":  "boleto",
		"amount":          "150.00",
		"currency":        "BRL",
		"recipient":       "Destinatário E2E",
		"boleto_code":     invalidBoleto,
		"idempotency_key": key(),
	})
	require.Equal(t, http.StatusUnprocessableEntity, status)
	require.Equal(t, "VALIDATION_ERROR", rejection.Code)
	require.Equal(t, map[string]string{"boleto_code": "boleto"}, rejection.Details)

	status, rejection = postRejected(t, "/api/v1/payments", map[string]interface{}{
		"account_id":      accountID,
		"payment_method":  "ted",
		"amount":          "100.00",
		"currency":        "BRL",
		"recipient":       "Destinatário E2E",
		"idempotency_key": key(),
	})
	require.Equal(t, http.StatusUnprocessableEntity, status)
	require.Equal(t, "VALIDATION_ERROR", rejection.Code)
	require.Equal(t, map[string]string{"ted": "required"}, rejection.Details)
}

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

func pay(t *testing.T, c customer, fields map[string]interface{}) string {
	t.Helper()
	k := key()
	body := map[string]interface{}{
		"account_id":      c.accountID,
		"currency":        "BRL",
		"recipient":       "Destinatário E2E",
		"description":     "e2e payment",
		"idempotency_key": k,
	}
	for name, value := range fields {
		body[name] = value
	}
	accepted(t, c.token, "/api/v1/payments", body)
	return k
}

func ted(bankCode string) map[string]interface{} {
	return map[string]interface{}{"bank_code": bankCode, "branch": "1234", "account": "123456", "document": "52998224725"}
}

func TestPaymentSandboxPaths(t *testing.T) {
	t.Parallel()
	c := newCustomer(t, "")
	deposit(t, c, "2000.00")

	pix := pay(t, c, map[string]interface{}{"payment_method": "pix", "amount": "42.50", "pix_key": "ana@example.com"})
	pixRejected := pay(t, c, map[string]interface{}{"payment_method": "pix", "amount": "10.00", "pix_key": "reject@reject.test"})
	tedSettled := pay(t, c, map[string]interface{}{"payment_method": "ted", "amount": "100.00", "ted": ted("341")})
	tedRejected := pay(t, c, map[string]interface{}{"payment_method": "ted", "amount": "50.00", "ted": ted("999")})
	boleto := pay(t, c, map[string]interface{}{"payment_method": "boleto", "amount": "150.00", "boleto_code": settledBoleto})
	boletoRejected := pay(t, c, map[string]interface{}{"payment_method": "boleto", "amount": "50.00", "boleto_code": rejectedBoleto})

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
	for _, want := range cases {
		p := payment(t, c, want.key)
		require.Equal(t, want.status, p["status"], "%s: %v", want.name, p)
		if want.reason != "" {
			require.Equal(t, want.reason, p["failure_reason"], "%s: %v", want.name, p)
		}
		if want.prefix != "" {
			externalID, _ := p["external_id"].(string)
			require.True(t, strings.HasPrefix(externalID, want.prefix), "%s external_id %q", want.name, externalID)
			require.Equal(t, want.prefix+p["payment_id"].(string), externalID)
		}
	}

	require.Equal(t, "1707.50", balance(t, c))

	require.Equal(t, "42.50", payment(t, c, pix)["amount"])

	tedPayment := payment(t, c, tedSettled)
	require.Equal(t, "100.00", tedPayment["amount"])
	tedDetails, ok := tedPayment["ted"].(map[string]interface{})
	require.True(t, ok, "ted payment has no ted details: %v", tedPayment)
	require.Equal(t, "*********25", tedDetails["document"])

	status, rejection := postRejected(t, c.token, "/api/v1/payments", map[string]interface{}{
		"account_id":      c.accountID,
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
	status, rejection = postRejected(t, c.token, "/api/v1/payments", map[string]interface{}{
		"account_id":      c.accountID,
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

	status, rejection = postRejected(t, c.token, "/api/v1/payments", map[string]interface{}{
		"account_id":      c.accountID,
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

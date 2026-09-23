//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

type mailpitSearch struct {
	Messages []struct {
		Subject string `json:"Subject"`
		To      []struct {
			Address string `json:"Address"`
		} `json:"To"`
	} `json:"messages"`
}

func mailSubjects(t *testing.T, email string) []string {
	t.Helper()
	query := url.QueryEscape(`to:"` + email + `"`)
	status, raw := send(t, http.MethodGet, mailpit()+"/api/v1/search?query="+query, nil)
	require.Equal(t, http.StatusOK, status, "mailpit search: %s", raw)
	var result mailpitSearch
	require.NoError(t, json.Unmarshal(raw, &result))
	subjects := make([]string, 0, len(result.Messages))
	for _, message := range result.Messages {
		subjects = append(subjects, message.Subject)
	}
	return subjects
}

func TestAccountCreationSendsWelcomeEmail(t *testing.T) {
	t.Parallel()
	userID, accountID := newCustomer(t, "11987654321")

	status, data := get(t, "/api/v1/accounts/"+accountID)
	require.Equal(t, http.StatusOK, status)
	account := data.(map[string]interface{})
	require.Equal(t, userID, account["user_id"])
	require.Equal(t, "checking", account["type"])
	require.Equal(t, "active", account["status"])
	require.InDelta(t, 0, account["balance"].(float64), 0.001)

	email := emailOf(userID)
	var subjects []string
	eventually(t, 0, func() bool {
		subjects = mailSubjects(t, email)
		return contains(subjects, "Bem-vindo(a) ao Fintech Bank")
	}, "welcome e-mail to %s not found, subjects: %v", email, &subjects)
}

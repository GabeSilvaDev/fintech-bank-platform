//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/notification-service/internal/infrastructure/senders"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type mailpitSearchResult struct {
	MessagesCount int `json:"messages_count"`
	Messages      []struct {
		To []struct {
			Address string `json:"Address"`
		} `json:"To"`
	} `json:"messages"`
}

func searchMailpit(t *testing.T, baseURL, subject string) mailpitSearchResult {
	t.Helper()
	query := url.Values{"query": {`subject:"` + subject + `"`}}
	resp, err := http.Get(baseURL + "/api/v1/search?" + query.Encode())
	require.NoError(t, err)
	defer resp.Body.Close()

	var result mailpitSearchResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	return result
}

func TestSMTPDeliversToMailpit(t *testing.T) {
	addr := os.Getenv("SMTP_ADDR")
	mailpitURL := os.Getenv("MAILPIT_URL")
	if addr == "" || mailpitURL == "" {
		t.Skip("SMTP_ADDR and MAILPIT_URL must be set")
	}

	subject := "Transferência recebida " + uuid.NewString()
	message := models.Message{To: "ana@example.com", Subject: subject, Body: "Você recebeu R$ 30,00.", Priority: "normal"}

	sender := senders.NewSMTP(addr, "no-reply@fintech.local", 10*time.Second)
	require.NoError(t, sender.Send(context.Background(), message))

	deadline := time.Now().Add(10 * time.Second)
	var result mailpitSearchResult
	for time.Now().Before(deadline) {
		result = searchMailpit(t, mailpitURL, subject)
		if result.MessagesCount >= 1 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	require.GreaterOrEqual(t, result.MessagesCount, 1)
	require.NotEmpty(t, result.Messages)
	require.NotEmpty(t, result.Messages[0].To)
	require.Equal(t, message.To, result.Messages[0].To[0].Address)
}

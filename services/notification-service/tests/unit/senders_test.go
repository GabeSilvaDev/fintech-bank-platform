package unit

import (
	"bytes"
	"context"
	"errors"
	"mime"
	"net/smtp"
	"strings"
	"testing"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/notification-service/internal/infrastructure/senders"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/stretchr/testify/assert"
)

func TestBuildEmailHeadersAndBody(t *testing.T) {
	message := models.Message{To: "ana@example.com", Subject: "Oi", Body: "Olá,\nSua conta está pronta.", Priority: "normal"}
	built := senders.BuildEmail("no-reply@fintech.local", message)

	expected := "From: no-reply@fintech.local\r\n" +
		"To: ana@example.com\r\n" +
		"Subject: Oi\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" +
		"Olá,\r\nSua conta está pronta.\r\n"
	assert.Equal(t, expected, string(built))
}

func TestBuildEmailEncodesNonASCIISubject(t *testing.T) {
	message := models.Message{To: "ana@example.com", Subject: "Transferência recebida", Body: "corpo", Priority: "normal"}
	built := senders.BuildEmail("no-reply@fintech.local", message)

	lines := strings.Split(string(built), "\r\n")
	subjectLine := lines[2]
	assert.True(t, strings.HasPrefix(subjectLine, "Subject: =?utf-8?q?"), subjectLine)
	assert.Equal(t, "Subject: "+mime.QEncoding.Encode("utf-8", message.Subject), subjectLine)
}

func TestSMTPSendPassesArgsAndReturnsError(t *testing.T) {
	message := models.Message{To: "ana@example.com", Subject: "Oi", Body: "corpo", Priority: "high"}
	var gotAddr, gotFrom string
	var gotAuth smtp.Auth
	var gotTo []string
	var gotMsg []byte
	wantErr := errors.New("smtp down")

	sender := senders.NewSMTPWith("mailpit:1025", "no-reply@fintech.local", func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		gotAddr, gotAuth, gotFrom, gotTo, gotMsg = addr, auth, from, to, msg
		return wantErr
	})

	err := sender.Send(context.Background(), message)
	assert.Equal(t, wantErr, err)
	assert.Equal(t, "mailpit:1025", gotAddr)
	assert.Nil(t, gotAuth)
	assert.Equal(t, "no-reply@fintech.local", gotFrom)
	assert.Equal(t, []string{"ana@example.com"}, gotTo)
	assert.Equal(t, senders.BuildEmail("no-reply@fintech.local", message), gotMsg)
}

func TestSMTPSendReturnsNilOnSuccess(t *testing.T) {
	sender := senders.NewSMTPWith("mailpit:1025", "no-reply@fintech.local", func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		return nil
	})
	err := sender.Send(context.Background(), models.Message{To: "ana@example.com"})
	assert.NoError(t, err)
}

func TestSandboxLogsSMS(t *testing.T) {
	buf := &bytes.Buffer{}
	sandbox := senders.NewSandbox(models.ChannelSMS, logger.New(logger.Config{Output: buf}))

	err := sandbox.Send(context.Background(), models.Message{To: "+5511999887766", Subject: "", Body: "Pagamento de R$ 10,00 confirmado", Priority: "high"})
	assert.NoError(t, err)

	logged := buf.String()
	assert.Contains(t, logged, `"channel":"sms"`)
	assert.Contains(t, logged, `"to":"+5511999887766"`)
	assert.Contains(t, logged, `"priority":"high"`)
	assert.Contains(t, logged, `"body":"Pagamento de R$ 10,00 confirmado"`)
	assert.Contains(t, logged, `"message":"notification sent"`)
}

func TestSandboxLogsPush(t *testing.T) {
	buf := &bytes.Buffer{}
	sandbox := senders.NewSandbox(models.ChannelPush, logger.New(logger.Config{Output: buf}))

	err := sandbox.Send(context.Background(), models.Message{To: "user-123", Subject: "Transferência recebida", Body: "Você recebeu R$ 30,00", Priority: "normal"})
	assert.NoError(t, err)

	logged := buf.String()
	assert.Contains(t, logged, `"channel":"push"`)
	assert.Contains(t, logged, `"to":"user-123"`)
	assert.Contains(t, logged, `"priority":"normal"`)
	assert.Contains(t, logged, `"subject":"Transferência recebida"`)
	assert.Contains(t, logged, `"body":"Você recebeu R$ 30,00"`)
	assert.Contains(t, logged, `"message":"notification sent"`)
}

package senders

import (
	"bytes"
	"context"
	"mime"
	"net/smtp"
	"strings"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
)

type SendMailFunc func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error

type SMTP struct {
	addr string
	from string
	send SendMailFunc
}

func NewSMTP(addr, from string) *SMTP {
	return NewSMTPWith(addr, from, smtp.SendMail)
}

func NewSMTPWith(addr, from string, send SendMailFunc) *SMTP {
	return &SMTP{addr: addr, from: from, send: send}
}

func (s *SMTP) Send(_ context.Context, message models.Message) error {
	return s.send(s.addr, nil, s.from, []string{message.To}, BuildEmail(s.from, message))
}

func BuildEmail(from string, message models.Message) []byte {
	var b bytes.Buffer
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + message.To + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", message.Subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(message.Body, "\n", "\r\n"))
	b.WriteString("\r\n")
	return b.Bytes()
}

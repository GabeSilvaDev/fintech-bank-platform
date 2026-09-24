package senders

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/domain"
)

const defaultSMTPTimeout = 10 * time.Second

type SMTP struct {
	addr     string
	from     string
	envelope string
	timeout  time.Duration
	now      func() time.Time
}

func NewSMTP(addr, from string, timeout time.Duration) *SMTP {
	if timeout <= 0 {
		timeout = defaultSMTPTimeout
	}
	envelope := from
	if address, err := mail.ParseAddress(from); err == nil {
		envelope = address.Address
	}
	return &SMTP{addr: addr, from: from, envelope: envelope, timeout: timeout, now: time.Now}
}

func (s *SMTP) WithClock(now func() time.Time) *SMTP {
	s.now = now
	return s
}

func (s *SMTP) Send(ctx context.Context, message models.Message) error {
	address, err := mail.ParseAddress(message.To)
	if err != nil {
		return domain.Invalid("invalid_recipient", "e-mail recipient is not a valid address")
	}
	message.To = address.Address
	return s.deliver(ctx, message.To, BuildEmail(s.from, message, s.now(), MessageID(s.from)))
}

func (s *SMTP) deliver(ctx context.Context, to string, msg []byte) error {
	err := s.converse(ctx, to, msg)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
			return context.DeadlineExceeded
		}
	}
	return err
}

func (s *SMTP) converse(ctx context.Context, to string, msg []byte) error {
	host, _, err := net.SplitHostPort(s.addr)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(s.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	conn, err := (&net.Dialer{Deadline: deadline}).DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() {
		_ = conn.SetDeadline(time.Now())
	})
	defer stop()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	if err := client.Hello("localhost"); err != nil {
		return err
	}
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if err := client.Mail(s.envelope); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(msg); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	_ = client.Quit()
	return nil
}

func MessageID(from string) string {
	host := "localhost"
	if address, err := mail.ParseAddress(from); err == nil {
		host = address.Address[strings.LastIndex(address.Address, "@")+1:]
	}
	random := make([]byte, 16)
	_, _ = rand.Read(random)
	return "<" + hex.EncodeToString(random) + "@" + host + ">"
}

func BuildEmail(from string, message models.Message, date time.Time, messageID string) []byte {
	var b bytes.Buffer
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + message.To + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", message.Subject) + "\r\n")
	b.WriteString("Date: " + date.Format(time.RFC1123Z) + "\r\n")
	b.WriteString("Message-ID: " + messageID + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(message.Body, "\n", "\r\n"))
	b.WriteString("\r\n")
	return b.Bytes()
}

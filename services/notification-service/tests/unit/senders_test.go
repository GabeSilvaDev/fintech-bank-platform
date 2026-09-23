package unit

import (
	"bytes"
	"context"
	"mime"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/notification-service/internal/infrastructure/senders"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/stretchr/testify/assert"
)

var emailDate = time.Date(2026, 9, 23, 18, 0, 0, 0, time.FixedZone("BRT", -3*60*60))

type smtpCapture struct {
	mu    sync.Mutex
	hello string
	from  string
	rcpt  []string
	data  string
}

func (c *smtpCapture) snapshot() smtpCapture {
	c.mu.Lock()
	defer c.mu.Unlock()
	return smtpCapture{hello: c.hello, from: c.from, rcpt: append([]string(nil), c.rcpt...), data: c.data}
}

func startSMTPServer(t *testing.T, replies map[string]string) (string, *smtpCapture) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	capture := &smtpCapture{}
	reply := func(key, fallback string) string {
		if r, ok := replies[key]; ok {
			return r
		}
		return fallback
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveSMTP(conn, capture, reply)
		}
	}()
	return listener.Addr().String(), capture
}

func serveSMTP(conn net.Conn, capture *smtpCapture, reply func(key, fallback string) string) {
	defer conn.Close()
	text := textproto.NewConn(conn)
	_ = text.PrintfLine("%s", reply("greeting", "220 fake ESMTP"))
	for {
		line, err := text.ReadLine()
		if err != nil {
			return
		}
		verb, args, _ := strings.Cut(line, " ")
		verb = strings.ToUpper(verb)
		capture.mu.Lock()
		switch verb {
		case "EHLO", "HELO":
			capture.hello = args
		case "MAIL":
			capture.from = args
		case "RCPT":
			capture.rcpt = append(capture.rcpt, args)
		}
		capture.mu.Unlock()
		switch verb {
		case "EHLO":
			_ = text.PrintfLine("%s", reply("EHLO", "250-fake\r\n250 8BITMIME"))
		case "HELO", "MAIL", "RCPT":
			_ = text.PrintfLine("%s", reply(verb, "250 OK"))
		case "STARTTLS":
			_ = text.PrintfLine("%s", reply("STARTTLS", "454 TLS not available"))
		case "DATA":
			_ = text.PrintfLine("%s", reply("DATA", "354 go ahead"))
			if !strings.HasPrefix(reply("DATA", "354"), "354") {
				continue
			}
			body, err := text.ReadDotBytes()
			if err != nil {
				return
			}
			capture.mu.Lock()
			capture.data = string(body)
			capture.mu.Unlock()
			_ = text.PrintfLine("%s", reply(".", "250 queued"))
		case "QUIT":
			_ = text.PrintfLine("%s", reply("QUIT", "221 bye"))
			return
		default:
			_ = text.PrintfLine("500 unknown command")
		}
	}
}

func silentListener(t *testing.T) string {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	var mu sync.Mutex
	var conns []net.Conn
	t.Cleanup(func() {
		listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range conns {
			conn.Close()
		}
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	return listener.Addr().String()
}

func closedAddress(t *testing.T) string {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	addr := listener.Addr().String()
	listener.Close()
	return addr
}

func TestBuildEmailHeadersAndBody(t *testing.T) {
	message := models.Message{To: "ana@example.com", Subject: "Oi", Body: "Olá,\nSua conta está pronta.", Priority: "normal"}
	built := senders.BuildEmail("no-reply@fintech.local", message, emailDate, "<abc@fintech.local>")

	expected := "From: no-reply@fintech.local\r\n" +
		"To: ana@example.com\r\n" +
		"Subject: Oi\r\n" +
		"Date: Wed, 23 Sep 2026 18:00:00 -0300\r\n" +
		"Message-ID: <abc@fintech.local>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" +
		"Olá,\r\nSua conta está pronta.\r\n"
	assert.Equal(t, expected, string(built))
}

func TestBuildEmailEncodesNonASCIISubject(t *testing.T) {
	message := models.Message{To: "ana@example.com", Subject: "Transferência recebida", Body: "corpo", Priority: "normal"}
	built := senders.BuildEmail("no-reply@fintech.local", message, emailDate, "<abc@fintech.local>")

	lines := strings.Split(string(built), "\r\n")
	subjectLine := lines[2]
	assert.True(t, strings.HasPrefix(subjectLine, "Subject: =?utf-8?q?"), subjectLine)
	assert.Equal(t, "Subject: "+mime.QEncoding.Encode("utf-8", message.Subject), subjectLine)
}

func TestMessageIDUsesTheSenderDomain(t *testing.T) {
	assert.Regexp(t, `^<[0-9a-f]{32}@fintech\.local>$`, senders.MessageID("no-reply@fintech.local"))
	assert.Regexp(t, `^<[0-9a-f]{32}@bank\.test>$`, senders.MessageID("Fintech Bank <avisos@bank.test>"))
	assert.Regexp(t, `^<[0-9a-f]{32}@localhost>$`, senders.MessageID("not an address"))
	assert.NotEqual(t, senders.MessageID("a@b.c"), senders.MessageID("a@b.c"))
}

func TestSMTPSendDeliversTheMessage(t *testing.T) {
	addr, capture := startSMTPServer(t, nil)
	message := models.Message{To: "Ana <ana@example.com>", Subject: "Oi", Body: "corpo\nlinha", Priority: "high"}

	sender := senders.NewSMTP(addr, "no-reply@fintech.local", time.Second).WithClock(func() time.Time { return emailDate })
	assert.NoError(t, sender.Send(context.Background(), message))

	got := capture.snapshot()
	assert.Equal(t, "localhost", got.hello)
	assert.True(t, strings.HasPrefix(got.from, "FROM:<no-reply@fintech.local>"), got.from)
	assert.Equal(t, []string{"TO:<ana@example.com>"}, got.rcpt)
	assert.Contains(t, got.data, "From: no-reply@fintech.local\n")
	assert.Contains(t, got.data, "To: ana@example.com\n")
	assert.Contains(t, got.data, "Subject: Oi\n")
	assert.Contains(t, got.data, "Date: Wed, 23 Sep 2026 18:00:00 -0300\n")
	assert.Regexp(t, `(?m)^Message-ID: <[0-9a-f]{32}@fintech\.local>$`, got.data)
	assert.True(t, strings.HasSuffix(got.data, "\ncorpo\nlinha\n"), got.data)
}

func TestSMTPSendReturnsServerRejections(t *testing.T) {
	cases := map[string]map[string]string{
		"greeting": {"greeting": "554 go away"},
		"hello":    {"EHLO": "502 no", "HELO": "502 no"},
		"starttls": {"EHLO": "250-fake\r\n250 STARTTLS"},
		"mail":     {"MAIL": "550 sender rejected"},
		"rcpt":     {"RCPT": "550 no such user"},
		"data":     {"DATA": "554 no data"},
		"data end": {".": "554 rejected"},
	}
	for name, replies := range cases {
		addr, _ := startSMTPServer(t, replies)
		sender := senders.NewSMTP(addr, "no-reply@fintech.local", time.Second)
		assert.Error(t, sender.Send(context.Background(), models.Message{To: "ana@example.com", Body: "corpo"}), name)
	}
}

func TestSMTPSendIgnoresQuitFailuresAfterTheMessageIsAccepted(t *testing.T) {
	addr, capture := startSMTPServer(t, map[string]string{"QUIT": "500 no"})
	sender := senders.NewSMTP(addr, "no-reply@fintech.local", time.Second)

	assert.NoError(t, sender.Send(context.Background(), models.Message{To: "ana@example.com", Body: "corpo"}))
	assert.Contains(t, capture.snapshot().data, "corpo")
}

func TestSMTPSendUsesTheBareSenderAddressAsEnvelope(t *testing.T) {
	addr, capture := startSMTPServer(t, nil)
	sender := senders.NewSMTP(addr, "Fintech Bank <avisos@bank.test>", 0)

	assert.NoError(t, sender.Send(context.Background(), models.Message{To: "ana@example.com", Body: "corpo"}))
	got := capture.snapshot()
	assert.True(t, strings.HasPrefix(got.from, "FROM:<avisos@bank.test>"), got.from)
	assert.Contains(t, got.data, "From: Fintech Bank <avisos@bank.test>\n")
}

func TestSMTPSendFailsOnUnreachableServers(t *testing.T) {
	assert.Error(t, senders.NewSMTP("no-port", "no-reply@fintech.local", time.Second).Send(context.Background(), models.Message{To: "ana@example.com"}))
	assert.Error(t, senders.NewSMTP(closedAddress(t), "no-reply@fintech.local", time.Second).Send(context.Background(), models.Message{To: "ana@example.com"}))
}

func TestSMTPSendTimesOutOnSilentServer(t *testing.T) {
	sender := senders.NewSMTP(silentListener(t), "no-reply@fintech.local", 200*time.Millisecond)

	started := time.Now()
	err := sender.Send(context.Background(), models.Message{To: "ana@example.com", Body: "corpo"})

	assert.Error(t, err)
	assert.Less(t, time.Since(started), 2*time.Second)
}

func TestSMTPSendHonoursTheContext(t *testing.T) {
	addr := silentListener(t)
	sender := senders.NewSMTP(addr, "no-reply@fintech.local", 10*time.Second)
	message := models.Message{To: "ana@example.com", Body: "corpo"}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, sender.Send(cancelled, message), context.Canceled)

	short, cancelShort := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelShort()
	started := time.Now()
	assert.ErrorIs(t, sender.Send(short, message), context.DeadlineExceeded)
	assert.Less(t, time.Since(started), 2*time.Second)

	aborted, abort := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, abort)
	started = time.Now()
	assert.ErrorIs(t, sender.Send(aborted, message), context.Canceled)
	assert.Less(t, time.Since(started), 2*time.Second)
}

func TestSMTPSendRejectsMalformedRecipients(t *testing.T) {
	addr, capture := startSMTPServer(t, nil)
	for _, to := range []string{"ana@example.com\r\nBcc: x@evil.test", "not an address"} {
		sender := senders.NewSMTP(addr, "no-reply@fintech.local", time.Second)

		err := sender.Send(context.Background(), models.Message{To: to})
		assert.Equal(t, "invalid_recipient", domain.InvalidCode(err), to)
	}
	assert.Empty(t, capture.snapshot().hello)
}

func TestSandboxLogsSMS(t *testing.T) {
	buf := &bytes.Buffer{}
	sandbox := senders.NewSandbox(models.ChannelSMS, logger.New(logger.Config{Output: buf}))

	err := sandbox.Send(context.Background(), models.Message{To: "+5511999887766", Subject: "", Body: "Pagamento de R$ 10,00 confirmado", Priority: "high"})
	assert.NoError(t, err)

	logged := buf.String()
	assert.Contains(t, logged, `"channel":"sms"`)
	assert.Contains(t, logged, `"to":"+55*******7766"`)
	assert.NotContains(t, logged, "+5511999887766")
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

package events

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/stretchr/testify/assert"
)

func TestNewEvent(t *testing.T) {
	payload := map[string]string{"key": "value"}
	event := NewEvent("test.event", "test-service", payload)

	assert.NotEmpty(t, event.ID)
	assert.Equal(t, "test.event", event.Type)
	assert.Equal(t, "1.0", event.Version)
	assert.Equal(t, "test-service", event.Source)
	assert.NotZero(t, event.Timestamp)
	assert.Equal(t, payload, event.Payload)
}

func TestEvent_WithTraceID(t *testing.T) {
	event := NewEvent("test.event", "test-service", nil)
	result := event.WithTraceID("trace-123")

	assert.Equal(t, "trace-123", result.TraceID)
	assert.Same(t, event, result)
}

func TestEvent_WithMetadata(t *testing.T) {
	event := NewEvent("test.event", "test-service", nil)

	event.WithMetadata("key1", "value1").WithMetadata("key2", "value2")

	assert.Equal(t, "value1", event.Metadata["key1"])
	assert.Equal(t, "value2", event.Metadata["key2"])
}

func TestEvent_ToJSON(t *testing.T) {
	payload := map[string]string{"message": "hello"}
	event := NewEvent("test.event", "test-service", payload)

	jsonData, err := event.ToJSON()

	assert.NoError(t, err)
	assert.Contains(t, string(jsonData), "test.event")
	assert.Contains(t, string(jsonData), "test-service")
}

func TestFromJSON(t *testing.T) {
	original := NewEvent("test.event", "test-service", map[string]string{"key": "value"})
	jsonData, _ := original.ToJSON()

	event, err := FromJSON(jsonData)

	assert.NoError(t, err)
	assert.Equal(t, original.ID, event.ID)
	assert.Equal(t, original.Type, event.Type)
	assert.Equal(t, original.Source, event.Source)
}

func TestFromJSON_Invalid(t *testing.T) {
	event, err := FromJSON([]byte("invalid json"))

	assert.Error(t, err)
	assert.Nil(t, event)
}

func TestTopics(t *testing.T) {
	assert.Equal(t, "account.commands", Topics.AccountCommands)
	assert.Equal(t, "transaction.commands", Topics.TransactionCommands)
	assert.Equal(t, "payment.commands", Topics.PaymentCommands)
	assert.Equal(t, "account.events", Topics.AccountEvents)
	assert.Equal(t, "transaction.events", Topics.TransactionEvents)
	assert.Equal(t, "payment.events", Topics.PaymentEvents)
	assert.Equal(t, "notification.events", Topics.NotificationEvents)
	assert.Equal(t, "account.dlq", Topics.AccountDLQ)
	assert.Equal(t, "transaction.dlq", Topics.TransactionDLQ)
	assert.Equal(t, "payment.dlq", Topics.PaymentDLQ)
}

func TestEventTypes(t *testing.T) {
	assert.Equal(t, "account.create", EventTypes.CreateAccount)
	assert.Equal(t, "account.update", EventTypes.UpdateAccount)
	assert.Equal(t, "account.delete", EventTypes.DeleteAccount)

	assert.Equal(t, "account.created", EventTypes.AccountCreated)
	assert.Equal(t, "account.updated", EventTypes.AccountUpdated)

	assert.Equal(t, "transaction.create", EventTypes.CreateTransaction)
	assert.Equal(t, "transaction.transfer", EventTypes.ProcessTransfer)

	assert.Equal(t, "transaction.completed", EventTypes.TransactionCompleted)
	assert.Equal(t, "transaction.failed", EventTypes.TransactionFailed)

	assert.Equal(t, "payment.process", EventTypes.ProcessPayment)
	assert.Equal(t, "payment.refund", EventTypes.RefundPayment)

	assert.Equal(t, "payment.completed", EventTypes.PaymentCompleted)
	assert.Equal(t, "payment.failed", EventTypes.PaymentFailed)

	assert.Equal(t, "notification.email", EventTypes.SendEmail)
	assert.Equal(t, "notification.sms", EventTypes.SendSMS)
}

func TestCreateAccountPayload(t *testing.T) {
	payload := CreateAccountPayload{
		UserID:      "user-123",
		AccountType: "checking",
		Name:        "John Doe",
		Email:       "john@example.com",
		Document:    "52998224725",
		Phone:       "11999887766",
	}

	jsonData, err := json.Marshal(payload)
	assert.NoError(t, err)

	var result CreateAccountPayload
	err = json.Unmarshal(jsonData, &result)
	assert.NoError(t, err)
	assert.Equal(t, payload.UserID, result.UserID)
	assert.Equal(t, payload.Email, result.Email)
}

func TestProcessTransferPayload(t *testing.T) {
	payload := ProcessTransferPayload{
		FromAccountID:  "acc-123",
		ToAccountID:    "acc-456",
		Amount:         domain.AmountFromCents(10050),
		Currency:       "BRL",
		Description:    "Test transfer",
		IdempotencyKey: "idem-123",
	}

	jsonData, err := json.Marshal(payload)
	assert.NoError(t, err)

	var result ProcessTransferPayload
	err = json.Unmarshal(jsonData, &result)
	assert.NoError(t, err)
	assert.Equal(t, payload.Amount, result.Amount)
	assert.Equal(t, payload.IdempotencyKey, result.IdempotencyKey)
}

func TestProcessPaymentPayload(t *testing.T) {
	payload := ProcessPaymentPayload{
		AccountID:      "acc-123",
		PaymentMethod:  "pix",
		Amount:         domain.AmountFromCents(25000),
		Currency:       "BRL",
		Recipient:      "Merchant XYZ",
		PixKey:         "merchant@example.com",
		IdempotencyKey: "idem-456",
	}

	jsonData, err := json.Marshal(payload)
	assert.NoError(t, err)

	var result ProcessPaymentPayload
	err = json.Unmarshal(jsonData, &result)
	assert.NoError(t, err)
	assert.Equal(t, payload.PaymentMethod, result.PaymentMethod)
	assert.Equal(t, payload.PixKey, result.PixKey)
}

func TestTransactionCompletedPayload(t *testing.T) {
	now := time.Now().UTC()
	payload := TransactionCompletedPayload{
		TransactionID: "txn-123",
		AccountID:     "acc-123",
		Type:          "credit",
		Amount:        domain.AmountFromCents(50000),
		Currency:      "BRL",
		BalanceAfter:  domain.AmountFromCents(150000),
		Status:        "completed",
		CompletedAt:   now,
	}

	jsonData, err := json.Marshal(payload)
	assert.NoError(t, err)

	var result TransactionCompletedPayload
	err = json.Unmarshal(jsonData, &result)
	assert.NoError(t, err)
	assert.Equal(t, payload.TransactionID, result.TransactionID)
	assert.Equal(t, payload.BalanceAfter, result.BalanceAfter)
}

func TestSendEmailPayload(t *testing.T) {
	payload := SendEmailPayload{
		To:       "user@example.com",
		Subject:  "Welcome!",
		Template: "welcome_email",
		Data:     map[string]string{"name": "John"},
		Priority: "high",
	}

	jsonData, err := json.Marshal(payload)
	assert.NoError(t, err)

	var result SendEmailPayload
	err = json.Unmarshal(jsonData, &result)
	assert.NoError(t, err)
	assert.Equal(t, payload.To, result.To)
	assert.Equal(t, payload.Data["name"], result.Data["name"])
}

func TestSendSMSPayload(t *testing.T) {
	payload := SendSMSPayload{
		To:       "11999887766",
		Message:  "Your code is 123456",
		Priority: "normal",
	}

	jsonData, err := json.Marshal(payload)
	assert.NoError(t, err)

	var result SendSMSPayload
	err = json.Unmarshal(jsonData, &result)
	assert.NoError(t, err)
	assert.Equal(t, payload.Message, result.Message)
}

func TestSendPushPayload(t *testing.T) {
	payload := SendPushPayload{
		UserID:   "user-123",
		Title:    "New Transaction",
		Body:     "You received R$ 100.00",
		Data:     map[string]string{"screen": "transactions"},
		Priority: "high",
	}

	jsonData, err := json.Marshal(payload)
	assert.NoError(t, err)

	var result SendPushPayload
	err = json.Unmarshal(jsonData, &result)
	assert.NoError(t, err)
	assert.Equal(t, payload.Title, result.Title)
}

func TestErrorPayload(t *testing.T) {
	originalEvent := NewEvent("test.event", "test-service", nil)
	payload := ErrorPayload{
		OriginalEvent: originalEvent,
		ErrorCode:     "VALIDATION_ERROR",
		ErrorMessage:  "Invalid input",
		Retries:       3,
		LastRetryAt:   "2024-01-01T00:00:00Z",
	}

	jsonData, err := json.Marshal(payload)
	assert.NoError(t, err)

	var result ErrorPayload
	err = json.Unmarshal(jsonData, &result)
	assert.NoError(t, err)
	assert.Equal(t, payload.ErrorCode, result.ErrorCode)
	assert.Equal(t, payload.Retries, result.Retries)
}

func TestNewAccountCommand(t *testing.T) {
	payload := CreateAccountPayload{UserID: "user-123"}
	event := NewAccountCommand(EventTypes.CreateAccount, payload)

	assert.Equal(t, EventTypes.CreateAccount, event.Type)
	assert.Equal(t, "api-gateway", event.Source)
}

func TestNewTransactionCommand(t *testing.T) {
	payload := CreateTransactionPayload{AccountID: "acc-123"}
	event := NewTransactionCommand(EventTypes.CreateTransaction, payload)

	assert.Equal(t, EventTypes.CreateTransaction, event.Type)
	assert.Equal(t, "api-gateway", event.Source)
}

func TestNewPaymentCommand(t *testing.T) {
	payload := ProcessPaymentPayload{AccountID: "acc-123"}
	event := NewPaymentCommand(EventTypes.ProcessPayment, payload)

	assert.Equal(t, EventTypes.ProcessPayment, event.Type)
	assert.Equal(t, "api-gateway", event.Source)
}

func TestNewAccountEvent(t *testing.T) {
	payload := AccountCreatedPayload{AccountID: "acc-123"}
	event := NewAccountEvent(EventTypes.AccountCreated, payload)

	assert.Equal(t, EventTypes.AccountCreated, event.Type)
	assert.Equal(t, "account-service", event.Source)
}

func TestNewTransactionEvent(t *testing.T) {
	payload := TransactionCompletedPayload{TransactionID: "txn-123"}
	event := NewTransactionEvent(EventTypes.TransactionCompleted, payload)

	assert.Equal(t, EventTypes.TransactionCompleted, event.Type)
	assert.Equal(t, "transaction-service", event.Source)
}

func TestNewPaymentEvent(t *testing.T) {
	payload := PaymentCompletedPayload{PaymentID: "pay-123"}
	event := NewPaymentEvent(EventTypes.PaymentCompleted, payload)

	assert.Equal(t, EventTypes.PaymentCompleted, event.Type)
	assert.Equal(t, "payment-service", event.Source)
}

func TestNewNotificationEvent(t *testing.T) {
	payload := SendEmailPayload{To: "user@example.com"}
	event := NewNotificationEvent(EventTypes.SendEmail, payload)

	assert.Equal(t, EventTypes.SendEmail, event.Type)
	assert.Equal(t, "notification-service", event.Source)
}

func TestDeleteAccountPayload(t *testing.T) {
	payload := DeleteAccountPayload{
		AccountID: "acc-123",
		Reason:    "customer request",
	}

	jsonData, err := json.Marshal(payload)
	assert.NoError(t, err)
	assert.Contains(t, string(jsonData), `"account_id":"acc-123"`)

	var result DeleteAccountPayload
	err = json.Unmarshal(jsonData, &result)
	assert.NoError(t, err)
	assert.Equal(t, payload, result)
}

func TestBalanceEventTypes(t *testing.T) {
	assert.Equal(t, "account.credit", EventTypes.CreditAccount)
	assert.Equal(t, "account.debit", EventTypes.DebitAccount)
	assert.Equal(t, "account.credited", EventTypes.AccountCredited)
	assert.Equal(t, "account.debited", EventTypes.AccountDebited)
	assert.Equal(t, "account.debit_rejected", EventTypes.DebitRejected)
	assert.Equal(t, "account.command_failed", EventTypes.AccountCommandFailed)
}

func TestBalancePayloadsRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	cases := []interface{}{
		CreditAccountPayload{AccountID: "a", Amount: domain.AmountFromCents(1050), Currency: "BRL", Reference: "tx-1", IdempotencyKey: "k1"},
		DebitAccountPayload{AccountID: "a", Amount: domain.AmountFromCents(300), Currency: "BRL", Reference: "tx-2", IdempotencyKey: "k2"},
		AccountCreditedPayload{AccountID: "a", Amount: domain.AmountFromCents(1050), BalanceAfter: domain.AmountFromCents(1050), Reference: "tx-1", IdempotencyKey: "k1", OccurredAt: now},
		AccountDebitedPayload{AccountID: "a", Amount: domain.AmountFromCents(300), BalanceAfter: domain.AmountFromCents(750), Reference: "tx-2", IdempotencyKey: "k2", OccurredAt: now},
		DebitRejectedPayload{AccountID: "a", Amount: domain.AmountFromCents(10000), Balance: domain.AmountFromCents(750), Reason: "insufficient_funds", Reference: "tx-3", IdempotencyKey: "k3"},
		AccountUpdatedPayload{AccountID: "a", UserID: "u", Name: "Ana", Email: "ana@example.com", Phone: "11999887766", Status: "active", UpdatedAt: now},
		AccountDeletedPayload{AccountID: "a", UserID: "u", Reason: "customer request", ClosedAt: now},
	}

	for _, payload := range cases {
		data, err := json.Marshal(payload)
		assert.NoError(t, err)
		decoded := reflect.New(reflect.TypeOf(payload)).Interface()
		assert.NoError(t, json.Unmarshal(data, decoded))
		assert.Equal(t, payload, reflect.ValueOf(decoded).Elem().Interface())
	}
}

func TestSprintThreeEventTypes(t *testing.T) {
	assert.Equal(t, "account.credit_rejected", EventTypes.CreditRejected)
	assert.Equal(t, "transaction.command_failed", EventTypes.TransactionCommandFailed)
}

func TestSprintThreePayloadsRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	cases := []interface{}{
		CreditRejectedPayload{AccountID: "a", Amount: domain.AmountFromCents(500), Balance: domain.AmountFromCents(100), Reason: "account_not_active", Reference: "t1", IdempotencyKey: "t1:credit"},
		TransactionCreatedPayload{TransactionID: "t1", Type: "transfer", AccountID: "a", CounterpartyID: "b", Amount: domain.AmountFromCents(500), Currency: "BRL", Description: "rent", IdempotencyKey: "k", CreatedAt: now},
		TransactionFailedPayload{TransactionID: "t1", AccountID: "a", Type: "deposit", Amount: domain.AmountFromCents(500), Currency: "BRL", Reason: "account_not_active", FailedAt: now},
		TransferFailedPayload{TransferID: "t1", FromAccountID: "a", ToAccountID: "b", Amount: domain.AmountFromCents(500), Currency: "BRL", Reason: "insufficient_funds", Status: "failed", FailedAt: now},
	}

	for _, payload := range cases {
		data, err := json.Marshal(payload)
		assert.NoError(t, err)
		decoded := reflect.New(reflect.TypeOf(payload)).Interface()
		assert.NoError(t, json.Unmarshal(data, decoded))
		assert.Equal(t, payload, reflect.ValueOf(decoded).Elem().Interface())
	}

	data, _ := json.Marshal(CreditRejectedPayload{AccountID: "a"})
	assert.Contains(t, string(data), `"account_id":"a"`)
	assert.Contains(t, string(data), `"reason":""`)
}

func TestSprintFourEventTypes(t *testing.T) {
	assert.Equal(t, "payment.created", EventTypes.PaymentCreated)
	assert.Equal(t, "payment.submit", EventTypes.SubmitPayment)
	assert.Equal(t, "payment.settle", EventTypes.SettlePayment)
	assert.Equal(t, "payment.command_failed", EventTypes.PaymentCommandFailed)
}

func TestSprintFourPayloadsRoundTrip(t *testing.T) {
	at := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	payloads := []interface{}{
		ProcessPaymentPayload{AccountID: "a", PaymentMethod: "ted", Amount: domain.AmountFromCents(1000), Currency: "BRL", Recipient: "Ana", TED: &TEDDetails{BankCode: "341", Branch: "0001", Account: "123456", Document: "52998224725"}, IdempotencyKey: "k"},
		SubmitPaymentPayload{PaymentID: "p"},
		SettlePaymentPayload{ExternalID: "ted_1", Status: "rejected", Reason: "invalid_destination"},
		PaymentCreatedPayload{PaymentID: "p", AccountID: "a", PaymentMethod: "pix", Amount: domain.AmountFromCents(150), Currency: "BRL", Recipient: "Ana", IdempotencyKey: "k", CreatedAt: at},
		PaymentProcessedPayload{PaymentID: "p", AccountID: "a", PaymentMethod: "ted", Amount: domain.AmountFromCents(150), Currency: "BRL", ExternalID: "ted_1", Status: "submitted", ProcessedAt: at},
		PaymentFailedPayload{PaymentID: "p", AccountID: "a", PaymentMethod: "boleto", Amount: domain.AmountFromCents(150), Currency: "BRL", Reason: "boleto_not_found", Status: "refunded", FailedAt: at},
	}
	for _, payload := range payloads {
		raw, err := NewPaymentEvent(EventTypes.PaymentFailed, payload).ToJSON()
		assert.NoError(t, err)
		decoded, err := FromJSON(raw)
		assert.NoError(t, err)
		assert.NotEmpty(t, decoded.Payload)
	}

	raw, _ := NewPaymentCommand(EventTypes.ProcessPayment, ProcessPaymentPayload{PaymentMethod: "pix"}).ToJSON()
	assert.NotContains(t, string(raw), `"ted"`)
	raw, _ = NewPaymentCommand(EventTypes.ProcessPayment, payloads[0]).ToJSON()
	assert.Contains(t, string(raw), `"ted":{"bank_code":"341","branch":"0001","account":"123456","document":"52998224725"}`)
	raw, _ = NewPaymentEvent(EventTypes.SettlePayment, SettlePaymentPayload{ExternalID: "x", Status: "settled"}).ToJSON()
	assert.NotContains(t, string(raw), `"reason"`)
}

func TestSprintFiveNames(t *testing.T) {
	assert.Equal(t, "notification.dlq", Topics.NotificationDLQ)
	assert.Equal(t, "notification.command_failed", EventTypes.NotificationCommandFailed)
}

func TestMoneyFieldsMarshalAsDecimalStrings(t *testing.T) {
	data, err := json.Marshal(TransferCompletedPayload{
		Amount:           domain.AmountFromCents(123450),
		FromBalanceAfter: domain.AmountFromCents(5),
		ToBalanceAfter:   domain.AmountFromCents(-100),
	})
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"amount":"1234.50"`)
	assert.Contains(t, string(data), `"from_balance_after":"0.05"`)
	assert.Contains(t, string(data), `"to_balance_after":"-1.00"`)
}

func TestMoneyFieldsDecodeLegacyNumbers(t *testing.T) {
	var payload AccountDebitedPayload
	err := json.Unmarshal([]byte(`{"account_id":"a","amount":10.5,"balance_after":1234.56}`), &payload)
	assert.NoError(t, err)
	assert.Equal(t, domain.AmountFromCents(1050), payload.Amount)
	assert.Equal(t, domain.AmountFromCents(123456), payload.BalanceAfter)
}

// ═══════════════════════════════════════════════════════════════════════════
// Package events - Kafka event definitions for the Fintech Platform
// ═══════════════════════════════════════════════════════════════════════════

package events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ═══════════════════════════════════════════════════════════════════════════
// BASE EVENT STRUCTURE
// ═══════════════════════════════════════════════════════════════════════════

// Event represents a base event structure for all Kafka messages
type Event struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Version   string            `json:"version"`
	Source    string            `json:"source"`
	Timestamp time.Time         `json:"timestamp"`
	TraceID   string            `json:"trace_id,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Payload   interface{}       `json:"payload"`
}

// NewEvent creates a new event with default values
func NewEvent(eventType, source string, payload interface{}) *Event {
	return &Event{
		ID:        uuid.NewString(),
		Type:      eventType,
		Version:   "1.0",
		Source:    source,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

// WithTraceID adds a trace ID to the event
func (e *Event) WithTraceID(traceID string) *Event {
	e.TraceID = traceID
	return e
}

// WithMetadata adds metadata to the event
func (e *Event) WithMetadata(key, value string) *Event {
	if e.Metadata == nil {
		e.Metadata = make(map[string]string)
	}
	e.Metadata[key] = value
	return e
}

// ToJSON serializes the event to JSON
func (e *Event) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// FromJSON deserializes JSON to an event
func FromJSON(data []byte) (*Event, error) {
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, err
	}
	return &event, nil
}

// ═══════════════════════════════════════════════════════════════════════════
// KAFKA TOPICS
// ═══════════════════════════════════════════════════════════════════════════

// Topics defines all Kafka topic names
var Topics = struct {
	// Commands (requests for actions)
	AccountCommands     string
	TransactionCommands string
	PaymentCommands     string

	// Events (results of actions)
	AccountEvents     string
	TransactionEvents string
	PaymentEvents     string

	// Notifications
	NotificationEvents string

	// Dead Letter Queues
	AccountDLQ     string
	TransactionDLQ string
	PaymentDLQ     string
}{
	AccountCommands:     "account.commands",
	TransactionCommands: "transaction.commands",
	PaymentCommands:     "payment.commands",

	AccountEvents:     "account.events",
	TransactionEvents: "transaction.events",
	PaymentEvents:     "payment.events",

	NotificationEvents: "notification.events",

	AccountDLQ:     "account.dlq",
	TransactionDLQ: "transaction.dlq",
	PaymentDLQ:     "payment.dlq",
}

// ═══════════════════════════════════════════════════════════════════════════
// EVENT TYPES
// ═══════════════════════════════════════════════════════════════════════════

// EventTypes defines all event type constants
var EventTypes = struct {
	// Account Commands
	CreateAccount string
	UpdateAccount string
	DeleteAccount string
	VerifyKYC     string

	// Account Events
	AccountCreated  string
	AccountUpdated  string
	AccountDeleted  string
	AccountVerified string
	KYCCompleted    string
	KYCFailed       string

	// Transaction Commands
	CreateTransaction  string
	ProcessTransfer    string
	ReverseTransaction string

	// Transaction Events
	TransactionCreated   string
	TransactionCompleted string
	TransactionFailed    string
	TransactionReversed  string
	TransferCompleted    string
	TransferFailed       string

	// Payment Commands
	ProcessPayment string
	RefundPayment  string
	CancelPayment  string

	// Payment Events
	PaymentProcessed string
	PaymentCompleted string
	PaymentFailed    string
	PaymentRefunded  string
	PaymentCancelled string

	// Notification Events
	SendEmail string
	SendSMS   string
	SendPush  string
}{
	// Account Commands
	CreateAccount: "account.create",
	UpdateAccount: "account.update",
	DeleteAccount: "account.delete",
	VerifyKYC:     "account.verify_kyc",

	// Account Events
	AccountCreated:  "account.created",
	AccountUpdated:  "account.updated",
	AccountDeleted:  "account.deleted",
	AccountVerified: "account.verified",
	KYCCompleted:    "account.kyc_completed",
	KYCFailed:       "account.kyc_failed",

	// Transaction Commands
	CreateTransaction:  "transaction.create",
	ProcessTransfer:    "transaction.transfer",
	ReverseTransaction: "transaction.reverse",

	// Transaction Events
	TransactionCreated:   "transaction.created",
	TransactionCompleted: "transaction.completed",
	TransactionFailed:    "transaction.failed",
	TransactionReversed:  "transaction.reversed",
	TransferCompleted:    "transaction.transfer_completed",
	TransferFailed:       "transaction.transfer_failed",

	// Payment Commands
	ProcessPayment: "payment.process",
	RefundPayment:  "payment.refund",
	CancelPayment:  "payment.cancel",

	// Payment Events
	PaymentProcessed: "payment.processed",
	PaymentCompleted: "payment.completed",
	PaymentFailed:    "payment.failed",
	PaymentRefunded:  "payment.refunded",
	PaymentCancelled: "payment.cancelled",

	// Notification Events
	SendEmail: "notification.email",
	SendSMS:   "notification.sms",
	SendPush:  "notification.push",
}

// ═══════════════════════════════════════════════════════════════════════════
// ACCOUNT PAYLOADS
// ═══════════════════════════════════════════════════════════════════════════

// CreateAccountPayload represents the payload for creating an account
type CreateAccountPayload struct {
	UserID      string `json:"user_id"`
	AccountType string `json:"account_type"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Document    string `json:"document"`
	Phone       string `json:"phone,omitempty"`
}

// UpdateAccountPayload represents the payload for updating an account
type UpdateAccountPayload struct {
	AccountID string  `json:"account_id"`
	Name      *string `json:"name,omitempty"`
	Email     *string `json:"email,omitempty"`
	Phone     *string `json:"phone,omitempty"`
	Status    *string `json:"status,omitempty"`
}

// AccountCreatedPayload represents the payload for account created event
type AccountCreatedPayload struct {
	AccountID     string    `json:"account_id"`
	UserID        string    `json:"user_id"`
	AccountNumber string    `json:"account_number"`
	Agency        string    `json:"agency"`
	AccountType   string    `json:"account_type"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

// ═══════════════════════════════════════════════════════════════════════════
// TRANSACTION PAYLOADS
// ═══════════════════════════════════════════════════════════════════════════

// CreateTransactionPayload represents the payload for creating a transaction
type CreateTransactionPayload struct {
	AccountID      string  `json:"account_id"`
	Type           string  `json:"type"`
	Amount         float64 `json:"amount"`
	Currency       string  `json:"currency"`
	Description    string  `json:"description,omitempty"`
	IdempotencyKey string  `json:"idempotency_key"`
}

// ProcessTransferPayload represents the payload for processing a transfer
type ProcessTransferPayload struct {
	FromAccountID  string  `json:"from_account_id"`
	ToAccountID    string  `json:"to_account_id"`
	Amount         float64 `json:"amount"`
	Currency       string  `json:"currency"`
	Description    string  `json:"description,omitempty"`
	IdempotencyKey string  `json:"idempotency_key"`
}

// TransactionCompletedPayload represents the payload for transaction completed event
type TransactionCompletedPayload struct {
	TransactionID string    `json:"transaction_id"`
	AccountID     string    `json:"account_id"`
	Type          string    `json:"type"`
	Amount        float64   `json:"amount"`
	Currency      string    `json:"currency"`
	BalanceAfter  float64   `json:"balance_after"`
	Status        string    `json:"status"`
	CompletedAt   time.Time `json:"completed_at"`
}

// TransferCompletedPayload represents the payload for transfer completed event
type TransferCompletedPayload struct {
	TransferID       string    `json:"transfer_id"`
	FromAccountID    string    `json:"from_account_id"`
	ToAccountID      string    `json:"to_account_id"`
	Amount           float64   `json:"amount"`
	Currency         string    `json:"currency"`
	FromBalanceAfter float64   `json:"from_balance_after"`
	ToBalanceAfter   float64   `json:"to_balance_after"`
	CompletedAt      time.Time `json:"completed_at"`
}

// ═══════════════════════════════════════════════════════════════════════════
// PAYMENT PAYLOADS
// ═══════════════════════════════════════════════════════════════════════════

// ProcessPaymentPayload represents the payload for processing a payment
type ProcessPaymentPayload struct {
	AccountID      string  `json:"account_id"`
	PaymentMethod  string  `json:"payment_method"`
	Amount         float64 `json:"amount"`
	Currency       string  `json:"currency"`
	Recipient      string  `json:"recipient"`
	PixKey         string  `json:"pix_key,omitempty"`
	BoletoCode     string  `json:"boleto_code,omitempty"`
	Description    string  `json:"description,omitempty"`
	IdempotencyKey string  `json:"idempotency_key"`
}

// PaymentCompletedPayload represents the payload for payment completed event
type PaymentCompletedPayload struct {
	PaymentID     string    `json:"payment_id"`
	AccountID     string    `json:"account_id"`
	PaymentMethod string    `json:"payment_method"`
	Amount        float64   `json:"amount"`
	Currency      string    `json:"currency"`
	Status        string    `json:"status"`
	ExternalID    string    `json:"external_id,omitempty"`
	CompletedAt   time.Time `json:"completed_at"`
}

// ═══════════════════════════════════════════════════════════════════════════
// NOTIFICATION PAYLOADS
// ═══════════════════════════════════════════════════════════════════════════

// SendEmailPayload represents the payload for sending an email
type SendEmailPayload struct {
	To       string            `json:"to"`
	Subject  string            `json:"subject"`
	Template string            `json:"template"`
	Data     map[string]string `json:"data,omitempty"`
	Priority string            `json:"priority,omitempty"`
}

// SendSMSPayload represents the payload for sending an SMS
type SendSMSPayload struct {
	To       string `json:"to"`
	Message  string `json:"message"`
	Priority string `json:"priority,omitempty"`
}

// SendPushPayload represents the payload for sending a push notification
type SendPushPayload struct {
	UserID   string            `json:"user_id"`
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Data     map[string]string `json:"data,omitempty"`
	Priority string            `json:"priority,omitempty"`
}

// ═══════════════════════════════════════════════════════════════════════════
// ERROR PAYLOADS
// ═══════════════════════════════════════════════════════════════════════════

// ErrorPayload represents a generic error payload for failed events
type ErrorPayload struct {
	OriginalEvent *Event `json:"original_event"`
	ErrorCode     string `json:"error_code"`
	ErrorMessage  string `json:"error_message"`
	Retries       int    `json:"retries"`
	LastRetryAt   string `json:"last_retry_at,omitempty"`
}

// ═══════════════════════════════════════════════════════════════════════════
// HELPER FUNCTIONS
// ═══════════════════════════════════════════════════════════════════════════

// NewAccountCommand creates a new account command event
func NewAccountCommand(eventType string, payload interface{}) *Event {
	return NewEvent(eventType, "api-gateway", payload)
}

// NewTransactionCommand creates a new transaction command event
func NewTransactionCommand(eventType string, payload interface{}) *Event {
	return NewEvent(eventType, "api-gateway", payload)
}

// NewPaymentCommand creates a new payment command event
func NewPaymentCommand(eventType string, payload interface{}) *Event {
	return NewEvent(eventType, "api-gateway", payload)
}

// NewAccountEvent creates a new account event
func NewAccountEvent(eventType string, payload interface{}) *Event {
	return NewEvent(eventType, "account-service", payload)
}

// NewTransactionEvent creates a new transaction event
func NewTransactionEvent(eventType string, payload interface{}) *Event {
	return NewEvent(eventType, "transaction-service", payload)
}

// NewPaymentEvent creates a new payment event
func NewPaymentEvent(eventType string, payload interface{}) *Event {
	return NewEvent(eventType, "payment-service", payload)
}

// NewNotificationEvent creates a new notification event
func NewNotificationEvent(eventType string, payload interface{}) *Event {
	return NewEvent(eventType, "notification-service", payload)
}

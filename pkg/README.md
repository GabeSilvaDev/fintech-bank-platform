# Shared packages

Go module `github.com/fintech-bank-platform/pkg`, imported by every service of the platform. Each package has its own tests; CI requires 100 % coverage and clean `gofmt`.

```
pkg/
├── logger/        zerolog wrapper with presets
├── errors/        AppError with HTTP status, code and details
├── response/      JSON response helpers for net/http
├── validation/    Brazilian and banking validators (CPF, CNPJ, PIX, …)
├── events/        Kafka event envelope, topics and typed payloads
├── env/           typed getters for environment variables
├── middleware/    request-id, request logging and panic recovery for chi
├── messaging/     kafka-go producer and consumer
├── domain/        shared domain errors and money helpers
├── cassandra/     migrator and write-error mapping
└── processor/     idempotent Kafka command processor
```

## logger

```go
import "github.com/fintech-bank-platform/pkg/logger"

log := logger.New(logger.Config{Level: "info", Pretty: true})
// or logger.NewDevelopment() / logger.NewProduction() / logger.NewDefault()

// *Logger embeds zerolog.Logger, so the zerolog API is available directly
log.WithFields(map[string]interface{}{"user_id": "123", "email": "user@example.com"}).
    Info().Msg("user created")

log.WithRequestID(reqID).WithError(err).Error().Msg("request failed")
```

## errors

```go
import "github.com/fintech-bank-platform/pkg/errors"

err := errors.NotFound("USER_NOT_FOUND", "User not found")
err = errors.BadRequest("VALIDATION_ERROR", "Invalid email").WithDetail("field", "email")

if errors.IsAppError(err) {
    appErr := err.(*errors.AppError) // .Code, .Message, .HTTPStatus, .Details
}
```

## response

```go
import "github.com/fintech-bank-platform/pkg/response"

response.OK(w, data)
response.Created(w, data)
response.NoContent(w)

response.BadRequest(w, "CODE", "message")
response.NotFound(w, "CODE", "message")
response.FromError(w, err) // renders an *errors.AppError with its own status

response.SuccessWithMeta(w, http.StatusOK, data, &response.Meta{
    Page: 1, PerPage: 10, Total: 100, TotalPages: 10,
})
```

## validation

```go
import "github.com/fintech-bank-platform/pkg/validation"

validation.IsValidCPF("529.982.247-25")
validation.IsValidCNPJ("11.222.333/0001-81")
validation.IsValidBrazilianPhone("+55 11 91234-5678")
validation.IsValidPixKey("user@example.com")
validation.IsStrongPassword("S3nh@Forte!")

type Account struct {
    CPF    string `validate:"cpf"`
    Phone  string `validate:"phone_br"`
    Agency string `validate:"agency_number"`
    Number string `validate:"account_number"`
}
err := validation.Validate(account)

validation.FormatCPF("52998224725")   // "529.982.247-25"
validation.FormatCNPJ("11222333000181")
validation.FormatPhone("11912345678")

validation.IsValidBoleto("34191790010100000012334567812309811000000015000")   // true
cents, ok := validation.BoletoAmountCents("34191790010100000012334567812309811000000015000") // 15000, true
```

Struct tags: `cpf`, `cnpj`, `phone_br`, `pix_key`, `agency_number`, `account_number`, `currency`, `password_strength`, `boleto`.

## events

```go
import "github.com/fintech-bank-platform/pkg/events"

event := events.NewAccountCommand(events.EventTypes.CreateAccount, events.CreateAccountPayload{
    UserID:      "user-123",
    AccountType: "checking",
    Name:        "John Doe",
    Email:       "john@example.com",
    Document:    "52998224725",
})
event.WithTraceID("trace-123").WithMetadata("source", "mobile-app")

data, _ := event.ToJSON()
back, _ := events.FromJSON(data)

topic := events.Topics.AccountCommands // "account.commands"

payment := events.NewPaymentCommand(events.EventTypes.ProcessPayment, events.ProcessPaymentPayload{
    AccountID:      "5d1e7c2a-0a6b-4c1e-9f4e-2b6f7a8c9d01",
    PaymentMethod:  "ted",
    Amount:         150.00,
    Currency:       "BRL",
    Recipient:      "Ana Souza",
    TED:            &events.TEDDetails{BankCode: "341", Branch: "0001", Account: "123456", Document: "52998224725"},
    IdempotencyKey: "idem-123",
})
```

The payment lifecycle runs through `events.EventTypes.ProcessPayment`/`SubmitPayment`/`SettlePayment` commands and `PaymentCreated`/`PaymentProcessed`/`PaymentCompleted`/`PaymentFailed` events on `events.Topics.PaymentEvents`; `TEDDetails` is only set on `ProcessPaymentPayload.TED` for TED payments.

## env

```go
import "github.com/fintech-bank-platform/pkg/env"

env.Get("KAFKA_BROKERS", "localhost:9092")
env.GetInt("KAFKA_MAX_ATTEMPTS", 3)
env.GetDuration("KAFKA_WRITE_TIMEOUT", 5*time.Second)
env.GetDurations("CONSUMER_RETRY_BACKOFF", []time.Duration{200 * time.Millisecond, time.Second, 5 * time.Second})
env.GetBool("LOG_PRETTY", false)
env.SplitAndTrim("kafka-1:9092, kafka-2:9092") // ["kafka-1:9092", "kafka-2:9092"]
```

## middleware

```go
import "github.com/fintech-bank-platform/pkg/middleware"

router.Use(middleware.RequestID, middleware.Logger(log), middleware.Recovery)

// downstream handlers read the request id middleware.RequestID generated (or forwarded from middleware.RequestIDHeader)
requestID := middleware.GetRequestID(r.Context())
```

## messaging

```go
import "github.com/fintech-bank-platform/pkg/messaging"

producer := messaging.NewProducer(messaging.ProducerConfig{
    Brokers:        []string{"localhost:9092"},
    WriteTimeout:   5 * time.Second,
    BatchTimeout:   10 * time.Millisecond,
    PublishTimeout: 20 * time.Second,
    MaxAttempts:    3,
})
err := producer.Publish(ctx, events.Topics.AccountCommands, key, event)

newConsumer := func() *messaging.Consumer {
    return messaging.NewConsumer(messaging.ConsumerConfig{
        Brokers:      []string{"localhost:9092"},
        GroupID:      "account-service",
        Topic:        "account.commands",
        DrainTimeout: 30 * time.Second, // how long the in-flight message may finish after ctx is cancelled
    })
}
err = newConsumer().Run(ctx, handle) // handle(ctx, kafka.Message) error; committed per message on success
messaging.RunWithRestart(ctx, newConsumer, handle, backoff, onError) // rebuilds the consumer after Run fails, waiting backoff[attempt] between tries
```

## domain

```go
import "github.com/fintech-bank-platform/pkg/domain"

domain.ErrNotFound       // not found
domain.ErrConflict       // concurrent update conflict
domain.ErrAmbiguousWrite // write may or may not have applied

err := domain.Invalid("invalid_amount", "amount must be greater than zero")
domain.IsInvalid(err)     // true
domain.InvalidCode(err)   // "invalid_amount"

cents, err := domain.ToCents(19.99) // 1999; rejects amounts <= 0 or with more than two decimal places
domain.FromCents(1999)              // 19.99
domain.Cents(19.99)                 // 1999, unvalidated
```

## cassandra

```go
import "github.com/fintech-bank-platform/pkg/cassandra"

// Executor is implemented by each service over its own gocql session
type Executor interface {
    Exec(ctx context.Context, statement string, values ...interface{}) error
    Versions(ctx context.Context, keyspace string) ([]int, error)
}

migrator := cassandra.NewMigrator(executor, "fintech_transactions", os.DirFS("migrations"))
applied, err := migrator.Up(ctx) // applies the first *.cql (the keyspace) then the rest in order, {{keyspace}} substituted, tracked in schema_migrations

err = cassandra.MapWriteError(err) // wraps a write timeout, an unavailable error or a cancelled/expired context as domain.ErrAmbiguousWrite
```

## processor

```go
import "github.com/fintech-bank-platform/pkg/processor"

type myDispatcher struct{}

func (myDispatcher) Dispatch(ctx context.Context, cmd *events.Event) (processor.Result, error) {
    var payload somePayload
    if err := processor.DecodePayload(cmd, &payload); err != nil {
        return processor.Result{}, err
    }
    return processor.Reply(events.Topics.AccountEvents, payload.AccountID, events.NewAccountEvent(events.EventTypes.AccountCredited, payload)), nil
}

proc := processor.NewProcessor(myDispatcher{}, store, publisher, processor.Config{
    Source:          "account-service",
    FailedEventType: events.EventTypes.AccountCommandFailed,
    DLQTopic:        events.Topics.AccountDLQ,
    Backoff:         []time.Duration{200 * time.Millisecond, time.Second, 5 * time.Second},
}, log)

err := proc.Process(ctx, msg.Key, msg.Value)
// dedupes by event id through store.MarkProcessed, retries transient dispatcher errors with Config.Backoff,
// and dead-letters the rest as Config.FailedEventType on Config.DLQTopic before publishing the dispatcher's reply messages
```

`Store.MarkProcessed(ctx, eventID) (bool, error)` and `Publisher.Publish(ctx, topic, key, event) error` are the other two seams. A dispatcher error is dead-lettered right away — no retry — when `domain.IsInvalid(err)` is true or it wraps `domain.ErrNotFound`, `domain.ErrAmbiguousWrite`, `processor.ErrUnknownCommand`, `processor.ErrBadPayload` or `processor.ErrPanic`; anything else is treated as transient and retried with `Config.Backoff`.

## Tests

```bash
make test            # go test ./...
make test-coverage   # coverage.html
make lint            # gofmt check
make docker-test     # same, inside the fintech-pkg container
```

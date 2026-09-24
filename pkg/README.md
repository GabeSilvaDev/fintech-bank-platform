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
├── processor/     idempotent Kafka command processor
├── retry/         fixed-delay retry with context cancellation
├── metrics/       prometheus registry, http middleware and /metrics handler
└── tracing/       opentelemetry setup, http middleware and transport, kafka header propagation
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

validation.IsValidIdempotencyKey("order-123") // true; 1-64 bytes, each in 0x21-0x7E (no spaces or control chars)
```

Struct tags: `cpf`, `cnpj`, `phone_br`, `pix_key`, `agency_number`, `account_number`, `currency`, `password_strength`, `boleto`, `idempotency_key`.

## events

```go
import (
    "github.com/fintech-bank-platform/pkg/domain"
    "github.com/fintech-bank-platform/pkg/events"
)

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
    Amount:         domain.AmountFromCents(15000),
    Currency:       "BRL",
    Recipient:      "Ana Souza",
    TED:            &events.TEDDetails{BankCode: "341", Branch: "0001", Account: "123456", Document: "52998224725"},
    IdempotencyKey: "idem-123",
})
```

The payment lifecycle runs through `events.EventTypes.ProcessPayment`/`SubmitPayment`/`SettlePayment` commands and `PaymentCreated`/`PaymentProcessed`/`PaymentCompleted`/`PaymentFailed` events on `events.Topics.PaymentEvents`; `TEDDetails` is only set on `ProcessPaymentPayload.TED` for TED payments.

Every domain follows the same dead-letter pattern: a topic in `events.Topics` (`AccountDLQ`, `TransactionDLQ`, `PaymentDLQ`, `NotificationDLQ`) paired with an `EventTypes.*CommandFailed` type, such as `events.EventTypes.NotificationCommandFailed` for the notification service.

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

router.Use(middleware.RequestID, middleware.Logger(log), middleware.Recovery(log))

// downstream handlers read the request id middleware.RequestID generated (or forwarded from middleware.RequestIDHeader)
requestID := middleware.GetRequestID(r.Context())
```

`Logger` adds `otel_trace_id` and `otel_span_id` to the request log line when the request context carries a span, so mount `tracing.Middleware` outside it; likewise it adds `client_ip` when a chi `ClientIPFrom*` middleware mounted outside it resolved the client address, next to the TCP peer in `remote_addr`. `Recovery(log)` logs a panic at error level (`panic`, `stack`, `request_id`, `method`, `path`, and the trace ids when present) with `handler panicked`, then answers the JSON error envelope with a 500 `INTERNAL_ERROR` unless the handler already wrote a response; `http.ErrAbortHandler` re-panics instead of being swallowed, and a `nil` logger falls back to `logger.NewDefault()`.

## messaging

```go
import "github.com/fintech-bank-platform/pkg/messaging"

producer := messaging.NewProducer(messaging.ProducerConfig{
    Brokers:        []string{"localhost:9092"},
    WriteTimeout:   5 * time.Second,
    BatchTimeout:   10 * time.Millisecond,
    PublishTimeout: 20 * time.Second,
    MaxAttempts:    3,
}).WithMetrics(m) // optional; records messages_published_total{topic,outcome="ok"|"error"}
err := producer.Publish(ctx, events.Topics.AccountCommands, key, event)

newConsumer := func() *messaging.Consumer {
    return messaging.NewConsumer(messaging.ConsumerConfig{
        Brokers:      []string{"localhost:9092"},
        GroupID:      "account-service",
        Topic:        "account.commands",
        DrainTimeout: 30 * time.Second, // how long the in-flight message may finish after ctx is cancelled
        StartOffset:  kafka.FirstOffset, // where a group without a committed offset starts; 0 means kafka.FirstOffset, kafka.LastOffset starts new groups at the end
        Metrics:      m,                 // optional; sets kafka_consumer_lag{topic,group} from the reader stats after every fetch
    })
}
err = newConsumer().Run(ctx, handle) // handle(ctx, kafka.Message) error; committed per message on success
messaging.RunWithRestart(ctx, newConsumer, handle, backoff, onError) // rebuilds the consumer after Run fails, waiting backoff[attempt] between tries; an empty backoff defaults to 1s
```

Traces cross Kafka through the message headers: `Publish` starts a `publish <topic>` producer span and injects its `traceparent` next to the `event_type` and `trace_id` headers, and the consumer hands the handler a context extracted from those headers, so the handler's spans continue the producer's trace. A consumer built with `NewConsumerWithReader` gets the lag gauge through `consumer.WithMetrics(m, topic, group)`; the gauge only moves when the reader has a `Stats() kafka.ReaderStats` method, as `*kafka.Reader` does. A `nil` `*metrics.Metrics` keeps everything unregistered.

## domain

```go
import "github.com/fintech-bank-platform/pkg/domain"

domain.ErrNotFound       // not found
domain.ErrConflict       // concurrent update conflict
domain.ErrAmbiguousWrite // write may or may not have applied

err := domain.Invalid("invalid_amount", "amount must be greater than zero")
domain.IsInvalid(err)     // true
domain.InvalidCode(err)   // "invalid_amount"

amount, err := domain.ParseAmount("19.99") // domain.Amount(1999); ^-?[0-9]{1,17}(\.[0-9]{1,2})?$, integer arithmetic, domain.Invalid("invalid_amount", …) otherwise
domain.AmountFromCents(1999) // domain.Amount(1999), unvalidated
amount.Cents()       // 1999
amount.String()      // "19.99"; "-0.50" and "-3.00" keep their sign
amount.IsPositive()  // true

data, _ := json.Marshal(amount)     // "19.99", from the value receiver, so it also marshals *domain.Amount struct fields
_ = json.Unmarshal(data, &amount)   // accepts a JSON string (ParseAmount) or a JSON number
// a JSON number is rejected once it rounds outside the int64 range, or has more than two decimal places
// json.Unmarshal([]byte("null"), &amount) fails with invalid_amount "amount is required";
// encoding/json only calls UnmarshalJSON for null on non-pointer fields, so a *domain.Amount struct field set to null stays nil instead
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

err = cassandra.MapWriteError(err) // wraps a write timeout, an unavailable error, an unknown lightweight transaction outcome or a cancelled/expired context as domain.ErrAmbiguousWrite
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
    Metrics:         m, // optional
}, log)

err := proc.Process(ctx, msg.Key, msg.Value)
// dedupes by event id through store.MarkProcessed, retries transient dispatcher errors with Config.Backoff,
// and dead-letters the rest as Config.FailedEventType on Config.DLQTopic before publishing the dispatcher's reply messages
```

`Store.MarkProcessed(ctx, eventID) (bool, error)` and `Publisher.Publish(ctx, topic, key, event) error` are the other two seams. A dispatcher error is dead-lettered right away — no retry — when `domain.IsInvalid(err)` is true or it wraps `domain.ErrNotFound`, `domain.ErrAmbiguousWrite`, `processor.ErrUnknownCommand`, `processor.ErrBadPayload` or `processor.ErrPanic`; anything else is treated as transient and retried with `Config.Backoff`.

`Process` runs inside a `process <event type>` consumer span that continues the trace found in `ctx` (the one the consumer extracted from the message headers), and the store, the dispatcher and every publish get that span's context, so result and dead-letter messages carry the same trace. With `Config.Metrics` set it records `messages_processed_total{type,outcome}` (`ok`, `duplicate` or `dead_lettered`; events that cannot be decoded use type `unknown`), `message_processing_duration_seconds{type}` and `message_retries_total{type}` (dispatch attempts beyond the first). A dispatched event whose result publish fails still counts as `ok` — the failed result shows up as `messages_published_total{outcome="error"}` and is dead-lettered — while a context error or an idempotency store failure records nothing because the message will be redelivered. Dead letters and failures mark the span as an error, and the processor's own log lines carry `otel_trace_id` and `otel_span_id`.

## retry

```go
import "github.com/fintech-bank-platform/pkg/retry"

err := retry.Do(ctx, 30, 2*time.Second, func() error {
    return db.Ping()
})
// calls fn up to 30 times, waiting 2s between attempts; returns nil on the first success,
// the last error once attempts run out, or ctx.Err() if the context is cancelled while waiting
```

The four domain services (account, transaction, payment and notification) use this to wait for Cassandra (bootstrap connection, migrations, keyspace session) or Redis (ping) at start-up — the API gateway does not — configured through `STARTUP_RETRY_ATTEMPTS` (default 30) and `STARTUP_RETRY_DELAY` (default `2s`; a non-positive value falls back to the default).

## metrics

```go
import "github.com/fintech-bank-platform/pkg/metrics"

m := metrics.New("account-service") // registers the Go and process collectors under service="account-service"

router.Use(m.Middleware) // records http_requests_total{method,route,status} and http_request_duration_seconds{method,route}
router.Handle("/metrics", m.Handler())

paymentsTotal := m.CounterVec("payments_processed_total", "Number of payments processed", "status")
paymentsTotal.WithLabelValues("success").Inc()

queueDepth := m.GaugeVec("queue_depth", "Current consumer queue depth", "topic")
queueDepth.WithLabelValues("account.commands").Set(12)

publishLatency := m.HistogramVec("publish_latency_seconds", "Kafka publish latency", prometheus.DefBuckets, "topic")
publishLatency.WithLabelValues("account.commands").Observe(0.042)

m.GaugeFunc("circuit_breaker_state", "Circuit breaker state", func() float64 { return stateValue(breaker.State()) }) // read at scrape time
```

Every method is nil-safe, so a service can pass a `*metrics.Metrics` obtained from optional configuration straight through: a `nil` receiver makes the factories return a fresh, unregistered collector, `Middleware` returns `next` unchanged, and `Handler` returns a 404. Calling a factory twice with the same name and labels returns the already-registered collector instead of panicking. The middleware's `method` label is one of the nine standard methods or `OTHER`, so arbitrary method tokens cannot create new series.

## tracing

```go
import "github.com/fintech-bank-platform/pkg/tracing"

shutdown, err := tracing.Init(ctx, tracing.Config{
    Service:     "account-service",
    Endpoint:    "http://jaeger:4318", // OTLP/HTTP; spans are posted to /v1/traces. Empty keeps an exporter-less provider
    SampleRatio: 0.1,                  // root sampling ratio; <= 0 or > 1 means 1. A sampled parent is always followed
})
defer shutdown(context.Background()) // flushes the batch span processor

router.Use(tracing.Middleware)     // server span per request, continued from an incoming traceparent, renamed "GET /accounts/{id}" after routing
router.Use(tracing.EdgeMiddleware) // public edge: new root span, incoming traceparent kept only as a link, traceparent/tracestate/baggage stripped from the request

client := &http.Client{Transport: tracing.Transport(nil)} // client span "GET host" and traceparent on every outgoing request

ctx, span := tracing.Tracer().Start(ctx, "debit")
defer span.End()

headers := tracing.Inject(ctx, msg.Headers) // adds traceparent (and tracestate) to a copy of the kafka headers
ctx = tracing.Extract(ctx, msg.Headers)     // continues the producer's trace on the consumer side

traceID, spanID, ok := tracing.IDs(ctx) // hex ids of the current span, ok only when the span context is valid
```

`Init` always installs the W3C `traceparent`/`tracestate` and `baggage` propagators as the global text map propagator, so `Middleware`, `Transport`, `Inject` and `Extract` use the same format everywhere. Without an endpoint, spans still get real ids, so trace ids propagate through HTTP and Kafka and show up in logs, but nothing is exported. The resource carries `service.name`; server and client spans record `http.request.method`, `url.path` and `http.response.status_code` (plus `http.route` on the server and `server.address` on the client), and a status of 500 or more, or a transport error, marks the span as an error. A method outside the nine standard ones is recorded as `_OTHER` (span name `HTTP`) with the raw value in `http.request.method_original`. Neither middleware traces `/health` or `/metrics`.

## Tests

```bash
make test            # go test ./...
make test-coverage   # coverage.html
make lint            # gofmt check
make docker-test     # same, inside the fintech-pkg container
```

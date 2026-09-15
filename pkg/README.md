# Shared packages

Go module `github.com/fintech-bank-platform/pkg`, imported by every service of the platform. Each package has its own tests; CI requires 100 % coverage and clean `gofmt`.

```
pkg/
├── logger/        zerolog wrapper with presets
├── errors/        AppError with HTTP status, code and details
├── response/      JSON response helpers for net/http
├── validation/    Brazilian and banking validators (CPF, CNPJ, PIX, …)
└── events/        Kafka event envelope, topics and typed payloads
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
```

Struct tags: `cpf`, `cnpj`, `phone_br`, `pix_key`, `agency_number`, `account_number`, `currency`, `password_strength`.

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
```

## Tests

```bash
make test            # go test ./...
make test-coverage   # coverage.html
make lint            # gofmt check
make docker-test     # same, inside the fintech-pkg container
```

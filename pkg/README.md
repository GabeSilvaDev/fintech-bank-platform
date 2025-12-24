# 📦 Fintech Platform - Shared Packages

Este diretório contém pacotes compartilhados utilizados por todos os microserviços da plataforma fintech.

## 📁 Estrutura

```
pkg/
├── logger/        # Logger estruturado (zerolog)
├── errors/        # Error handling padronizado
├── response/      # HTTP response helpers
├── validation/    # Validadores compartilhados (CPF, CNPJ, Phone, etc.)
└── events/        # Definições de eventos Kafka
```

## 📋 Pacotes Disponíveis

### 🪵 Logger (`pkg/logger`)

Logger estruturado baseado em zerolog com suporte a níveis de log, saída JSON e contexto.

```go
import "github.com/fintech-bank-platform/pkg/logger"

// Inicializar logger
log := logger.NewLogger(logger.Config{
    Level:  "info",
    Pretty: true, // false em produção
})

// Usar logger
log.Info("User created", logger.Fields{
    "user_id": "123",
    "email":   "user@example.com",
})
```

### ⚠️ Errors (`pkg/errors`)

Error handling padronizado com códigos HTTP e detalhes.

```go
import "github.com/fintech-bank-platform/pkg/errors"

// Criar erros tipados
err := errors.NotFound("USER_NOT_FOUND", "User not found")
err := errors.BadRequest("VALIDATION_ERROR", "Invalid email").
    WithDetail("field", "email")

// Verificar tipo
if errors.IsNotFound(err) {
    // handle not found
}
```

### 📤 Response (`pkg/response`)

HTTP response helpers para respostas padronizadas.

```go
import "github.com/fintech-bank-platform/pkg/response"

// Sucesso
response.OK(w, data)
response.Created(w, data)
response.NoContent(w)

// Erro
response.BadRequest(w, "CODE", "message")
response.NotFound(w, "CODE", "message")

// Com paginação
response.SuccessWithMeta(w, http.StatusOK, data, &response.Meta{
    Page:       1,
    PerPage:    10,
    Total:      100,
    TotalPages: 10,
})
```

### ✅ Validation (`pkg/validation`)

Validadores compartilhados para dados brasileiros e bancários.

```go
import "github.com/fintech-bank-platform/pkg/validation"

// Validar CPF
if validation.IsValidCPF("529.982.247-25") {
    // CPF válido
}

// Validar CNPJ
if validation.IsValidCNPJ("11.222.333/0001-81") {
    // CNPJ válido
}

// Validar com struct tags
type Account struct {
    CPF    string `validate:"cpf"`
    Phone  string `validate:"phone_br"`
    Agency string `validate:"agency_number"`
}

err := validation.Validate(account)

// Formatar
formatted := validation.FormatCPF("52998224725") // "529.982.247-25"
```

### 📨 Events (`pkg/events`)

Definições de eventos Kafka para comunicação entre microserviços.

```go
import "github.com/fintech-bank-platform/pkg/events"

// Criar evento
event := events.NewAccountCommand(events.EventTypes.CreateAccount, events.CreateAccountPayload{
    UserID:      "user-123",
    AccountType: "checking",
    Name:        "John Doe",
    Email:       "john@example.com",
    Document:    "52998224725",
})

// Adicionar metadata
event.WithTraceID("trace-123").WithMetadata("source", "mobile-app")

// Serializar
jsonData, _ := event.ToJSON()

// Tópicos disponíveis
topic := events.Topics.AccountCommands // "account.commands"
```

## 🧪 Testes

### Rodar testes localmente

```bash
# Com Go instalado
go test -v ./...

# Com coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### Rodar testes via Docker

```bash
# Usando Makefile
make docker-test
make docker-coverage

# Usando docker-compose
docker-compose run --rm pkg-test
docker-compose run --rm pkg-coverage
```

## 🛠️ Desenvolvimento

### Adicionar ao seu serviço

No `go.mod` do seu serviço, adicione:

```go
require github.com/fintech-bank-platform/pkg v0.0.0

replace github.com/fintech-bank-platform/pkg => ../../pkg
```

### Convenções

- **Testes**: Mínimo 80% de cobertura
- **Formatação**: `gofmt -s -w .`
- **Linting**: `golangci-lint run`
- **Documentação**: Comentários em todas as funções públicas

## 📊 Validadores Customizados

| Tag | Descrição | Exemplo |
|-----|-----------|---------|
| `cpf` | CPF brasileiro | `52998224725` |
| `cnpj` | CNPJ brasileiro | `11222333000181` |
| `phone_br` | Telefone brasileiro | `11999887766` |
| `currency` | Código ISO 4217 | `BRL`, `USD` |
| `password_strength` | Senha forte | `MyP@ssw0rd` |
| `account_number` | Número de conta | `12345678` |
| `agency_number` | Número de agência | `1234` |
| `pix_key` | Chave PIX | CPF, Email, Phone, EVP |

## 📝 Licença

Este projeto é parte da plataforma Fintech Bank.

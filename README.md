<div align="center">

# Fintech Bank Platform

**Event-driven banking backend in Go** — an HTTP API gateway publishing commands to Kafka, domain microservices consuming them, Cassandra for persistence and Redis for cache.

[![Status](https://img.shields.io/badge/status-work%20in%20progress-f59e0b)](#roadmap)
[![CI](https://github.com/GabeSilvaDev/fintech-bank-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/GabeSilvaDev/fintech-bank-platform/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Kafka](https://img.shields.io/badge/Kafka-3.7%20KRaft-231F20?logo=apachekafka&logoColor=white)](https://kafka.apache.org)
[![Cassandra](https://img.shields.io/badge/Cassandra-4.1-1287B1?logo=apachecassandra&logoColor=white)](https://cassandra.apache.org)
[![Redis](https://img.shields.io/badge/Redis-7.2-DC382D?logo=redis&logoColor=white)](https://redis.io)
[![Coverage](https://img.shields.io/badge/coverage-100%25%20enforced-2e7d32)](#development)
[![License](https://img.shields.io/badge/license-MIT-555)](LICENSE)

**English** · [Português (Brasil)](README.pt-BR.md)

</div>

> **Work in progress.** The infrastructure, the shared packages and the API gateway are in place — the gateway already validates commands and publishes them to Kafka; the domain services that consume them are next. See the [roadmap](#roadmap) for what is done and what is planned.

## Architecture

```mermaid
flowchart LR
    C[Client] -->|HTTP| GW[API Gateway<br/>Go · Chi]
    GW -->|commands| K[(Kafka<br/>KRaft)]
    K --> A[Account Service]
    K --> T[Transaction Service]
    K --> P[Payment Service]
    K --> N[Notification Service]
    A & T & P --> CS[(Cassandra)]
    A & T & P --> R[(Redis)]
    A & T & P -->|events| K

    classDef planned stroke-dasharray: 5 5,opacity:0.6
    class A,T,P,N,CS,R planned
```

Solid boxes exist today; dashed ones are planned. The gateway receives HTTP requests and publishes them as commands on Kafka; each domain service consumes its command topic, persists to Cassandra, and emits result events. Redis caches hot reads and backs rate limiting.

**Topics** (`pkg/events`): `account.commands`, `transaction.commands`, `payment.commands` for commands; `account.events`, `transaction.events`, `payment.events`, `notification.events` for results; one dead-letter topic per domain.

## What exists today

| Component | Path | State |
|---|---|---|
| Infrastructure | `docker-compose.yml` | Kafka 3.7.1 (KRaft), Cassandra 4.1, Redis 7.2, optional Kafka UI and Cassandra Web |
| Shared packages | `pkg/` | `logger`, `errors`, `response`, `validation`, `events` — 100 % test coverage, enforced in CI |
| API Gateway | `services/api-gateway/` | Chi router with request-id, real-IP, logging, recovery, CORS and rate-limit middleware; `GET /health`; command endpoints publishing to Kafka through a circuit-breaker-guarded producer; typed config from env; unit + feature tests at 100 % coverage, Kafka integration test in CI |

### Shared packages

| Package | What it gives every service |
|---|---|
| `logger` | zerolog wrapper with `Config{Level, Pretty, TimeFormat, Output}` and development/production presets |
| `errors` | `AppError` with code, message, HTTP status and details; constructors per status (`BadRequest`, `NotFound`, …) |
| `response` | JSON helpers (`OK`, `Created`, `NoContent`, `BadRequest`, …), `SuccessWithMeta` for pagination, `FromError` to render an `AppError` |
| `validation` | Brazilian and banking validators — CPF, CNPJ, phone, PIX key, agency and account numbers, currency, password strength — as functions and as `validate:"…"` struct tags |
| `events` | Kafka `Event` envelope (id, type, version, source, timestamp, trace id, metadata, payload), topic and event-type catalogs, typed payloads and `NewAccountCommand`-style constructors |

Usage examples live in [`pkg/README.md`](pkg/README.md).

## Getting started

Requires Docker, Docker Compose and ~4 GB of RAM for the containers. Go 1.25 only if you want to run the services outside Docker.

```bash
git clone https://github.com/GabeSilvaDev/fintech-bank-platform.git
cd fintech-bank-platform
cp .env.example .env

docker compose up -d                  # Kafka + Cassandra + Redis
docker compose --profile ui up -d     # + Kafka UI (:8080) and Cassandra Web (:3000)
docker compose ps                     # wait until everything is healthy (~1–2 min)
```

| Service | Container | Port |
|---|---|---|
| Kafka (KRaft) | `fintech-kafka` | 9092 |
| Cassandra | `fintech-cassandra` | 9042 |
| Redis | `fintech-redis` | 6379 |
| Kafka UI *(profile `ui`)* | `fintech-kafka-ui` | 8080 |
| Cassandra Web *(profile `ui`)* | `fintech-cassandra-web` | 3000 |

### API Gateway

```bash
cd services/api-gateway
cp .env.example .env
docker compose up -d                  # hot reload with Air, published on :8081
curl http://localhost:8081/health
```

Or natively: `make run` (listens on `SERVER_PORT`, default 8080). Configuration is read from the environment: `SERVER_*` (host, port, timeouts), `CORS_*`, `RATE_LIMIT_REQUESTS` / `RATE_LIMIT_WINDOW`, `KAFKA_BROKERS` / `KAFKA_WRITE_TIMEOUT` / `KAFKA_BATCH_TIMEOUT` / `KAFKA_PUBLISH_TIMEOUT` / `KAFKA_MAX_ATTEMPTS` / `KAFKA_BREAKER_*` and `LOG_LEVEL` / `LOG_PRETTY`.

#### Command endpoints

Every write is accepted asynchronously: the gateway validates the body, publishes a command to Kafka and answers `202` with the command id and the trace id (`X-Request-ID`).

| Method | Path | Topic | Event type |
|---|---|---|---|
| `POST` | `/api/v1/accounts` | `account.commands` | `account.create` |
| `PATCH` | `/api/v1/accounts/{id}` | `account.commands` | `account.update` |
| `DELETE` | `/api/v1/accounts/{id}` | `account.commands` | `account.delete` |
| `POST` | `/api/v1/transactions` | `transaction.commands` | `transaction.create` |
| `POST` | `/api/v1/transfers` | `transaction.commands` | `transaction.transfer` |
| `POST` | `/api/v1/payments` | `payment.commands` | `payment.process` |

```bash
curl -s -X POST localhost:8081/api/v1/accounts \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"5d1e7c2a-0a6b-4c1e-9f4e-2b6f7a8c9d01","account_type":"checking","name":"Ana Souza","email":"ana@example.com","document":"52998224725"}'
# {"success":true,"data":{"command_id":"…","trace_id":"…"}}
```

Errors: `400 INVALID_JSON`, `413 PAYLOAD_TOO_LARGE` (body over 1 MiB), `422 VALIDATION_ERROR` (with per-field `details`), `422 EMPTY_UPDATE` (PATCH without fields), `429 RATE_LIMIT_EXCEEDED`, `503 PUBLISH_FAILED` when the broker is unreachable or the circuit is open.

## Development

```bash
# shared packages
cd pkg && make test && make lint            # go test ./... · gofmt check

# api gateway
cd services/api-gateway
make test                                   # unit + feature, coverage of ./internal/...
make test-coverage                          # writes coverage.html
make test-integration                       # needs KAFKA_BROKERS pointing at a broker
```

CI (`.github/workflows/ci.yml`) runs on every push and pull request: `gofmt` check and the test suites for `pkg` and `api-gateway`, failing the build if coverage drops below 100 %.

## Project structure

```
fintech-bank-platform/
├── docker-compose.yml         Kafka · Cassandra · Redis (+ ui profile)
├── .env.example               every variable the platform reads
├── .github/workflows/ci.yml   gofmt + tests + coverage gate
├── pkg/                       shared Go module
│   ├── logger/  errors/  response/  validation/  events/
│   ├── Makefile · Dockerfile · docker-compose.yml
│   └── README.md
└── services/
    └── api-gateway/
        ├── cmd/main.go                    entry point
        ├── internal/
        │   ├── config/                    env → typed Config
        │   ├── contracts/                 interfaces for config, context and http
        │   ├── app/handlers/              command endpoints
        │   └── infrastructure/
        │       ├── http/                  server, router, handlers, middleware/
        │       └── messaging/             kafka producer, circuit breaker
        ├── tests/  (unit/ · feature/ · integration/)     testify-style TestCase helpers
        ├── Makefile · Dockerfile · docker-compose.yml · .air.toml
        └── .env.example
```

Each future service follows the same layout: `cmd/`, `internal/{config,contracts,infrastructure,app}`, `migrations/` (CQL) and `tests/`.

## Roadmap

- [x] **Sprint 0** — Docker Compose infrastructure (Kafka KRaft, Cassandra, Redis, debug UIs)
- [x] **Shared packages** — logger, errors, response, validation, events, with CI and 100 % coverage
- [x] **Sprint 1 — API Gateway** — HTTP skeleton, middleware, config, Kafka producer with circuit breaker and command endpoints
- [ ] **Sprint 2 — Account Service** — account and customer CRUD, Cassandra keyspace and migrations
- [ ] **Sprint 3 — Transaction Service** — transfers with idempotency and balance checks
- [ ] **Sprint 4 — Payment Service** — PIX, TED and boleto flows
- [ ] **Sprint 5 — Notification Service** — e-mail, SMS and push consumers
- [ ] **Sprint 6** — end-to-end tests and load tests
- [ ] **Sprint 7** — observability (Prometheus, Jaeger) and docs

## License

[MIT](LICENSE) © [Gabriel Silva](https://github.com/GabeSilvaDev)

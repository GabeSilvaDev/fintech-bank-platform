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

> **Work in progress.** Infrastructure, shared packages and all five services — the API gateway, the account service, the transaction service, the payment service and the notification service — are in place: commands flow from HTTP to Kafka to Cassandra, deposits, withdrawals and transfers settle as sagas over the account service, PIX, TED and boleto payments settle the same way through a sandbox provider with signed webhooks, and result events turn into e-mail, SMS and push through the notification service, with reads coming back through the gateway. Customers register and log in with an e-mail and a password, and the gateway issues JWT access tokens and lets each user reach only their own accounts, statements, transactions, payments and notifications. Money is exact: amounts travel as decimal strings through the API and the events and are handled as integer cents inside every service. Every service retries its Cassandra or Redis connection at start-up, account credits and debits are idempotent per key, a reconciliation sweeper recovers stuck transactions and payments from an index of the open ones, account statements page through history with a `before` cursor, and the platform is exercised end to end and under k6 load, on top of its unit, feature and integration tests. Every service exposes Prometheus metrics and OpenTelemetry traces, with a Grafana dashboard, alert rules and Jaeger in an optional compose profile, and the public API is described in OpenAPI. See the [roadmap](#roadmap) for what is done.

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
    N --> R[(Redis)]
    N --> MP[(Mailpit)]
    A & T & P & N -->|events| K
```

The gateway receives HTTP requests and publishes them as commands on Kafka; each domain service consumes its command topic, persists to Cassandra, and emits result events. The notification service turns those result events into e-mail, SMS and push, using Redis for idempotency and history and Mailpit to catch outgoing e-mail in development.

**Topics** (`pkg/events`): `account.commands`, `transaction.commands`, `payment.commands` for commands; `account.events`, `transaction.events`, `payment.events`, `notification.events` for results; one dead-letter topic per domain. The root compose pre-creates every topic with a `kafka-init` one-shot.

The cross-service design — components, the topics table, sequence diagrams for authentication, every saga, notifications and reconciliation, idempotency layers, failure semantics, security and observability — is in [`docs/architecture.md`](docs/architecture.md). How to run the checks, write commits and add a service is in [`CONTRIBUTING.md`](CONTRIBUTING.md).

## What exists today

| Component | Path | State |
|---|---|---|
| Infrastructure | `docker-compose.yml` | Kafka 3.7.1 (KRaft), Cassandra 4.1, Redis 7.2, Mailpit, optional Kafka UI and Cassandra Web (profile `ui`), optional Prometheus, Grafana and Jaeger (profile `observability`, configured in `observability/`) |
| Shared packages | `pkg/` | `logger`, `errors`, `response`, `validation`, `events`, `env`, `middleware`, `messaging`, `domain`, `cassandra`, `processor`, `retry`, `metrics`, `tracing` — 100 % test coverage, enforced in CI |
| API Gateway | `services/api-gateway/` | Chi router with request-id, tracing, metrics, client IP (the connection address, or a trusted `X-Forwarded-For` hop with `TRUST_PROXY_HEADERS=true`), logging, recovery, CORS and rate-limit middleware; `GET /health`; registration and login (`POST /api/v1/auth/register`, `/auth/login`, under their own rate limit) issuing HS256 JWT access tokens, bearer authentication on every other `/api/v1` route and owner-only access checked against the account service's owner endpoint (see [Authentication and authorization](#authentication-and-authorization)); command endpoints publishing to Kafka through a circuit-breaker-guarded producer; typed config from env; OpenAPI 3.1 document served at `GET /api/v1/openapi.yaml` and linted with Redocly in CI; unit + feature tests at 100 % coverage, Kafka integration test in CI; read routes proxied to the domain services |
| Account Service | `services/account-service/` | Consumes `account.commands`, retries its Cassandra bootstrap, migrations and keyspace session at start-up (`STARTUP_RETRY_*`), persists customers and accounts in Cassandra (`fintech_accounts`), owns balances with compare-and-set credits/debits that require an `idempotency_key` and apply at most once (`balance_operations`, 30-day TTL), publishes results — including `account.credit_rejected` — on `account.events` and failures on `account.dlq`; stores e-mail/password identities (`identities_by_email`, bcrypt) behind internal endpoints the gateway calls for registration and login; read API on `:8082`; unit + feature tests at 100 % of `internal/app`, Cassandra and Kafka integration tests in CI |
| Transaction Service | `services/transaction-service/` | Consumes `transaction.commands` and the account service's replies on `account.events`, retries its Cassandra bootstrap at start-up (`STARTUP_RETRY_*`), records deposits, withdrawals and transfers in Cassandra (`fintech_transactions`), orchestrates each one as a saga over `account.commands` (debit → credit → compensating credit on failure) with per-step idempotency keys, runs a reconciliation sweeper that reads an index of open transactions (`open_transactions`) and re-sends the next step of the stale ones (`SWEEPER_*`), publishes `transaction.created/completed/failed` and `transaction.transfer_completed/transfer_failed` on `transaction.events`, dead-letters on `transaction.dlq`; read API with `before`-cursor statements on `:8083`; unit + feature tests at 100 % of `internal/app`, Cassandra and Kafka integration tests in CI |
| Payment Service | `services/payment-service/` | Consumes `payment.commands` and the account service's replies on `account.events`, retries its Cassandra bootstrap at start-up (`STARTUP_RETRY_*`), stores PIX, TED and boleto payments in Cassandra (`fintech_payments`), reserves funds with `account.debit`, submits to a sandbox provider — PIX settles at once, TED and boleto settle through a signed webhook — refunds rejections with `account.credit`, runs a reconciliation sweeper that reads an index of open payments (`open_payments`) and re-sends the next step of the stale ones (`SWEEPER_*`), publishes `payment.created/processed/completed/failed` on `payment.events`, dead-letters on `payment.dlq`; read API with `before`-cursor statements and webhook on `:8084`; unit + feature tests at 100 % of `internal/app`, Cassandra and Kafka integration tests in CI |
| Notification Service | `services/notification-service/` | Consumes `account.events`, `transaction.events` and `payment.events` and routes the results as Portuguese e-mail, SMS and push, retrying its Redis ping at start-up (`STARTUP_RETRY_*`), looking up the account service's internal owner endpoint for contacts; delivery commands on `notification.events` are sent over SMTP (Mailpit in development) or sandbox SMS/push providers and recorded in a Redis-backed history, dead-lettering on `notification.dlq`; read API on `:8085`; unit + feature tests at 100 % of `internal/app`, Kafka, Redis and Mailpit integration tests in CI |

Every service also serves Prometheus metrics on `GET /metrics` of its HTTP port and propagates OpenTelemetry traces over HTTP and Kafka — see [Observability](#observability).

### Shared packages

| Package | What it gives every service |
|---|---|
| `logger` | zerolog wrapper with `Config{Level, Pretty, TimeFormat, Output}` and development/production presets |
| `errors` | `AppError` with code, message, HTTP status and details; constructors per status (`BadRequest`, `NotFound`, …) |
| `response` | JSON helpers (`OK`, `Created`, `NoContent`, `BadRequest`, …), `SuccessWithMeta` for pagination, `FromError` to render an `AppError` |
| `validation` | Brazilian and banking validators — CPF, CNPJ, phone, PIX key, agency and account numbers, currency, password strength, boleto digitable line (and the amount it encodes), idempotency key — as functions and as `validate:"…"` struct tags |
| `events` | Kafka `Event` envelope (id, type, version, source, timestamp, trace id, metadata, payload), topic and event-type catalogs, typed payloads and `NewAccountCommand`-style constructors |
| `env` | typed getters for environment variables |
| `middleware` | request-id, request logging and panic recovery for chi; `Recovery(log)` logs the panic and answers the JSON error envelope with `500 INTERNAL_ERROR` |
| `messaging` | kafka-go producer with publish timeout and a consumer loop with per-message commit that finishes the in-flight message on shutdown (`DrainTimeout`) and restarts with backoff (`RunWithRestart`) |
| `domain` | Shared domain errors (`ErrNotFound`, `ErrConflict`, `ErrAmbiguousWrite`, `Invalid`/`IsInvalid`/`InvalidCode`) and the exact money type `Amount` (int64 cents, JSON as a two-decimal string, `ParseAmount`, `AmountFromCents`) |
| `cassandra` | `Migrator` that runs the keyspace file first, then every other `.cql` file in order over an `Executor` seam, tracking versions in `schema_migrations`; `MapWriteError` maps ambiguous Cassandra failures to `domain.ErrAmbiguousWrite` |
| `processor` | Idempotent Kafka command processor: dedupes by event id, retries transient errors with backoff, dead-letters the rest, and publishes a dispatcher's reply events |
| `retry` | `Do(ctx, attempts, delay, fn)` retries `fn` with a fixed delay between attempts until it succeeds, the attempts run out or the context is cancelled; used by the four domain services (account, transaction, payment and notification) to wait for Cassandra or Redis at start-up; the API gateway has nothing to wait for |
| `metrics` | Per-service Prometheus registry labelled with `service`, Go and process collectors, HTTP middleware (`http_requests_total`, `http_request_duration_seconds`), `/metrics` handler and nil-safe counter, gauge and histogram factories |
| `tracing` | OpenTelemetry setup with an optional OTLP/HTTP exporter and ratio sampling, HTTP server middleware and client transport, `traceparent` injection and extraction on Kafka headers |

Usage examples live in [`pkg/README.md`](pkg/README.md).

## Getting started

Requires Docker, Docker Compose and ~4 GB of RAM for the containers. Go 1.25 only if you want to run the services outside Docker.

```bash
git clone https://github.com/GabeSilvaDev/fintech-bank-platform.git
cd fintech-bank-platform
cp .env.example .env

docker compose up -d                  # Kafka + Cassandra + Redis
docker compose --profile ui up -d     # + Kafka UI (:8080) and Cassandra Web (:3000)
docker compose --profile observability up -d   # + Prometheus (:9090), Grafana (:3000), Jaeger (:16686)
docker compose ps                     # wait until everything is healthy (~1–2 min)
```

| Service | Container | Port |
|---|---|---|
| Kafka (KRaft) | `fintech-kafka` | 9092 |
| Cassandra | `fintech-cassandra` | 9042 |
| Redis | `fintech-redis` | 6379 (`REDIS_PORT`) |
| Mailpit | `fintech-mailpit` | 1025 SMTP (`MAILPIT_SMTP_PORT`) · 8025 UI (`MAILPIT_UI_PORT`) |
| Kafka UI *(profile `ui`)* | `fintech-kafka-ui` | 8080 |
| Cassandra Web *(profile `ui`)* | `fintech-cassandra-web` | 3000 |
| Prometheus *(profile `observability`)* | `fintech-prometheus` | 9090 (`PROMETHEUS_PORT`) |
| Grafana *(profile `observability`)* | `fintech-grafana` | 3000 (`GRAFANA_PORT`) |
| Jaeger *(profile `observability`)* | `fintech-jaeger` | 16686 UI (`JAEGER_UI_PORT`) |

Grafana and Cassandra Web both default to port 3000: set `GRAFANA_PORT` (for example `3001`) in `.env` before enabling both profiles.

Redis backs the notification service's idempotency and history; Mailpit catches its outgoing e-mail, with a web UI at http://localhost:8025.

### API Gateway

```bash
cd services/api-gateway
cp .env.example .env
docker compose up -d                  # hot reload with Air, published on :8081
curl http://localhost:8081/health
```

Or natively: `make run` (listens on `SERVER_PORT`, default 8080). Configuration is read from the environment: `SERVER_*` (host, port, timeouts), `CORS_*` (`CORS_ALLOWED_METHODS` defaults to `GET,POST,PUT,PATCH,DELETE,OPTIONS`, `CORS_ALLOW_CREDENTIALS` to `false` and `CORS_EXPOSED_HEADERS` to `Link,X-Next-Before`), `RATE_LIMIT_REQUESTS` / `RATE_LIMIT_WINDOW`, `KAFKA_BROKERS` / `KAFKA_WRITE_TIMEOUT` / `KAFKA_BATCH_TIMEOUT` / `KAFKA_PUBLISH_TIMEOUT` / `KAFKA_MAX_ATTEMPTS` / `KAFKA_BREAKER_*`, `TRUST_PROXY_HEADERS` / `TRUSTED_PROXY_HOPS`, `JWT_SECRET` / `JWT_TTL`, `AUTH_RATE_LIMIT_REQUESTS` / `AUTH_RATE_LIMIT_WINDOW`, `OWNER_CACHE_TTL`, `METRICS_ENABLED` / `OTEL_EXPORTER_OTLP_ENDPOINT` / `OTEL_SAMPLER_RATIO` and `LOG_LEVEL` / `LOG_PRETTY`.

| Variable | Default | Effect |
|---|---|---|
| `JWT_SECRET` | none, required | HMAC key that signs and verifies access tokens; surrounding whitespace is trimmed, and the gateway refuses to start when what is left is shorter than 32 bytes. The `dev-only-jwt-secret-change-me-0123456789` value in `docker-compose.yml` and `.env.example` is public and for local development only: the gateway logs a warning at start-up whenever it runs with it (`JWT_SECRET is the published development value; set a private secret before exposing the gateway`); anywhere else, set a random secret of at least 32 bytes |
| `JWT_TTL` | `1h` | Lifetime of an access token (`expires_in` in seconds), at most `24h`: a longer value stops the gateway at start-up; a non-positive value falls back to the default |
| `CORS_ALLOW_CREDENTIALS` | `false` | Whether browsers may send cookies or HTTP authentication with cross-origin requests. The API authenticates with a bearer token, so it isn't needed; turn it on only together with an explicit `CORS_ALLOWED_ORIGINS` list |
| `AUTH_RATE_LIMIT_REQUESTS` / `AUTH_RATE_LIMIT_WINDOW` | `10` / `1m` | Separate limit on `/api/v1/auth/*`, counted per client like the general one; values below `1` (or a non-positive window) fall back to the defaults |
| `OWNER_CACHE_TTL` | `1m` | How long the gateway keeps an account's owner in memory; a non-positive value falls back to the default |

The rate limit (`RATE_LIMIT_REQUESTS` per `RATE_LIMIT_WINDOW`) is counted per client address, and an IPv6 client by its /64 network, so rotating addresses within its prefix doesn't earn it more requests (IPv4 addresses, including IPv4-mapped IPv6 ones, are counted one by one). By default (`TRUST_PROXY_HEADERS=false`) that is the address of the TCP connection, and `X-Forwarded-For`, `X-Real-IP` and `True-Client-IP` are ignored, so a client can't pick its own rate-limit bucket by sending them. Set `TRUST_PROXY_HEADERS=true` only when the gateway is reachable exclusively through your reverse proxies, and set `TRUSTED_PROXY_HOPS` (default `1`; values below `1` fall back to `1`) to the number of proxies between the internet and the gateway. The client address is then the `X-Forwarded-For` entry appended by the outermost of those proxies: the gateway counts `TRUSTED_PROXY_HOPS` entries from the right of the header (every `X-Forwarded-For` header merged in order) and takes that one, so with one proxy it is the rightmost entry — the address that proxy saw — and anything a client put further left is never read. `X-Real-IP` and `True-Client-IP` are ignored in this mode too. When `X-Forwarded-For` is missing, has fewer entries than `TRUSTED_PROXY_HOPS`, or the chosen entry is not an IP, the request is keyed on the connection address instead (the nearest proxy's, so those requests share one bucket). Set the exact count: one too low keys every client on a proxy's address, and one too high reads an entry the client wrote itself (or, when there is none, falls back to that shared bucket). The gateway never rewrites the request's remote address, so request logs keep the TCP peer — the proxy, behind one — in `remote_addr` and add the resolved client address as `client_ip` whenever there is one.

The public API is described in OpenAPI 3.1 in `services/api-gateway/api/openapi.yaml`, embedded in the binary and served by the gateway itself (`curl http://localhost:8081/api/v1/openapi.yaml`); CI lints it with Redocly using the root `redocly.yaml`.

### Account Service

```bash
cd services/account-service
cp .env.example .env
docker compose up -d                  # hot reload with Air, published on :8082
curl http://localhost:8082/health     # {"success":true,"data":{"status":"healthy","cassandra":"up"}}
```

Connecting to Cassandra, applying migrations in `migrations/*.cql` (against `CASSANDRA_KEYSPACE`, default `fintech_accounts`) and opening the keyspace session are retried at boot up to `STARTUP_RETRY_ATTEMPTS` times (default 30), waiting `STARTUP_RETRY_DELAY` between attempts (default `2s`; a non-positive value falls back to the default). Configuration: `SERVER_*`, `KAFKA_BROKERS` / `KAFKA_GROUP_ID` / `KAFKA_*_TIMEOUT` / `KAFKA_MAX_ATTEMPTS`, `CONSUMER_RETRY_BACKOFF` / `CONSUMER_DRAIN_TIMEOUT`, `CASSANDRA_HOSTS` / `CASSANDRA_KEYSPACE` / `CASSANDRA_CONSISTENCY` / `CASSANDRA_*_TIMEOUT` / `CASSANDRA_MIGRATIONS_PATH`, `STARTUP_RETRY_ATTEMPTS` / `STARTUP_RETRY_DELAY`, `IDENTITY_HASH_CONCURRENCY`, `METRICS_ENABLED` / `OTEL_EXPORTER_OTLP_ENDPOINT` / `OTEL_SAMPLER_RATIO`, `LOG_LEVEL` / `LOG_PRETTY`.

The account service also keeps the platform's identities, in `identities_by_email` (`migrations/008_identities_by_email.cql`: e-mail, user id, bcrypt password hash with cost 12, creation time). The gateway calls two internal endpoints for registration and login; they are not proxied by the gateway:

| Method | Path | Body | Answer |
|---|---|---|---|
| `POST` | `/identities` | `{email, password}` | `201` `{user_id}` (a new random UUID), `409 EMAIL_TAKEN`, `422 VALIDATION_ERROR` (`email` or `password`), `503 SERVICE_BUSY` |
| `POST` | `/identities/verify` | `{email, password}` | `200` `{user_id}`, `401 INVALID_CREDENTIALS`, `503 SERVICE_BUSY` |

The e-mail is trimmed and lower-cased before it is stored or looked up, and a registration is written with `IF NOT EXISTS`, so two concurrent registrations of the same e-mail can't both succeed. The password must be 8 to 72 bytes, bcrypt's own limit. Verification answers the same `401` for an unknown e-mail, a wrong password, an e-mail that isn't a valid address and a password longer than 72 bytes; when there is no identity to compare against, it still runs a bcrypt comparison against a fixed dummy hash, so the time it takes doesn't reveal which e-mails are registered. Bodies over 16 KiB answer `413 PAYLOAD_TOO_LARGE`.

bcrypt is deliberately slow, so the service runs at most `IDENTITY_HASH_CONCURRENCY` hashes and comparisons at once (default twice `GOMAXPROCS`; values below `1` fall back to the default), the dummy comparison included. A request that can't start its hash within 2 s, or whose caller gives up first, answers `503 SERVICE_BUSY` without touching the stored identities, so a burst of logins queues briefly and then sheds load instead of starving the rest of the service.

Commands it handles (topic `account.commands`) and the events it answers with (topic `account.events`):

| Command | Result | Dead-letter (`account.dlq`) when |
|---|---|---|
| `account.create` | `account.created` | invalid data, number collision after 5 tries |
| `account.update` | `account.updated` | unknown account, closed account, empty update |
| `account.delete` | `account.deleted` | unknown account, non-zero balance |
| `account.credit` | `account.credited` or `account.credit_rejected` (`account_not_active`, `account_not_found`, `balance_limit_exceeded` when the credit would take the balance past the largest int64 number of cents) — replayed as-is for a repeated `idempotency_key` | invalid amount, currency or idempotency key (`invalid_idempotency_key`, `idempotency_key_reused`), or `ambiguous_write` while that key's outcome is still pending |
| `account.debit` | `account.debited` or `account.debit_rejected` (`insufficient_funds`, `account_not_active`, `account_not_found`) — replayed as-is for a repeated `idempotency_key` | invalid amount, currency or idempotency key (`invalid_idempotency_key`, `idempotency_key_reused`), or `ambiguous_write` while that key's outcome is still pending |

Every `account.credit` and `account.debit` must carry an `idempotency_key` and is applied at most once per `(account_id, idempotency_key)`, tracked in `balance_operations` (30-day TTL, `services/account-service/migrations/007_balance_operations.cql`). A repeated key skips the balance change and replays the stored outcome as the reply — including a stored `insufficient_funds` rejection; a key still reserved by an attempt that crashed before it recorded an outcome is dead-lettered as `ambiguous_write` on every retry until the row expires with the 30-day TTL, since the platform can't tell whether the balance change happened; it needs manual resolution before then, because once the row expires the same key is accepted as new and the balance change would be applied again (which is why the transaction and payment sweepers stop re-sending after `SWEEPER_MAX_AGE`); a key already used for the other kind of operation (a credit retried as a debit, or vice versa) is rejected as `idempotency_key_reused`.

Every command is applied at most once (`processed_events`, 7-day TTL); transient failures are retried with `CONSUMER_RETRY_BACKOFF` and then dead-lettered as `account.command_failed`, with `retries` counting the dispatch attempts. A Cassandra write timeout or unavailable error, a lightweight transaction whose outcome is unknown, a client-side write timeout, or a write whose context was cancelled or expired is dead-lettered right away as `ambiguous_write`, since the write may or may not have been applied and retrying could apply it twice. Because the event id is marked before dispatch, a dead-lettered command replayed as-is is skipped as a duplicate: replays need a new event id. On shutdown the consumer finishes the message in flight (up to `CONSUMER_DRAIN_TIMEOUT`) before committing, and a consumer that stops on an error is restarted with `CONSUMER_RETRY_BACKOFF` while the read API keeps serving.

### Transaction Service

```bash
cd services/transaction-service
cp .env.example .env
docker compose up -d                  # hot reload with Air, published on :8083
curl http://localhost:8083/health     # {"success":true,"data":{"status":"healthy","cassandra":"up"}}
```

Migrations in `migrations/*.cql` run at boot against `CASSANDRA_KEYSPACE` (default `fintech_transactions`); connecting to Cassandra, applying migrations and opening the keyspace session are retried the same way as the account service (`STARTUP_RETRY_ATTEMPTS` / `STARTUP_RETRY_DELAY`). Configuration reuses the account service's variable names, with `SERVER_PORT` defaulting to `8083`, `KAFKA_GROUP_ID` to `transaction-service` and `CASSANDRA_KEYSPACE` to `fintech_transactions`, plus the reconciliation sweeper's `SWEEPER_ENABLED` (default `true`), `SWEEPER_INTERVAL` (default `1m`), `SWEEPER_STALE_AFTER` (default `5m`), `SWEEPER_MAX_AGE` (default `24h`; must be shorter than `720h`, the `balance_operations` TTL, or the service refuses to start), `SWEEPER_BATCH` (default `100`) and `SWEEPER_FULL_SCAN_INTERVAL` (default `24h`; `0` turns off the periodic index rebuilds but keeps the one at start-up, and a negative value falls back to the default).

Consumes `transaction.commands` and the account service's replies on `account.events`:

| Topic | Group | Handles |
|---|---|---|
| `transaction.commands` | `KAFKA_GROUP_ID` | `transaction.create` (deposit/withdrawal), `transaction.transfer` |
| `account.events` | `KAFKA_GROUP_ID-replies` | `account.credited`, `account.debited`, `account.debit_rejected`, `account.credit_rejected` |

A transaction is recorded as `pending` with its idempotency key reserved first in `transactions_by_account_key`, scoped to the transaction's account (the sender for a transfer) — a key repeated on the same account is a no-op, while another account may use the same key — then driven as a saga over `account.commands`: a deposit or withdrawal asks for a single credit or debit and settles as `completed` or `failed` (`insufficient_funds`, `account_not_active`, `account_not_found`, `balance_limit_exceeded`); a transfer debits the source (`pending` → `debited`), credits the counterparty (`debited` → `completed`), and compensates with a credit back to the source if that credit is rejected (`reversing` → `reversed`, or `reversal_failed` plus a `transaction.dlq` entry if the compensation itself is rejected). Every step carries its own idempotency key (`<transaction id>:debit`, `:credit` or `:reversal`) and every status change is a Cassandra lightweight transaction guarded by the expected status, so a duplicate or stale reply is ignored. Results are published on `transaction.events` (`transaction.created`, `transaction.completed`/`transaction.failed`, `transaction.transfer_completed`/`transaction.transfer_failed`); transient failures are retried with `CONSUMER_RETRY_BACKOFF` and then dead-lettered on `transaction.dlq` as `transaction.command_failed`.

If the account service's reply to a step never arrives, the transaction is left stuck; a reconciliation sweeper (`SWEEPER_ENABLED`, on by default) recovers it. Every `SWEEPER_INTERVAL` it reads the ids in the open-transaction index, loads those transactions in batches of up to 100 ids and picks the non-terminal ones whose `updated_at` is older than `SWEEPER_STALE_AFTER`, up to `SWEEPER_BATCH` per sweep; each one is "touched" with a lightweight transaction conditioned on the status and `updated_at` it read, so only one service instance acts on it and a transaction another instance already touched simply waits another stale window. Once touched, its next step is re-sent: a `pending` deposit asks for another credit, a `pending` withdrawal or transfer asks for another debit, a `debited` transfer credits the counterparty, and a `reversing` transfer re-sends the reversal credit — reusing the transaction's original per-step idempotency keys, which the account service now deduplicates.

Re-sending stops once a transaction is older than `SWEEPER_MAX_AGE` (measured from `created_at`). The first sweep that finds it past that age touches it one last time and raises a single alert instead of re-sending: a `transaction.command_failed` event on `transaction.dlq` with error code `reconciliation_exhausted` naming the transaction and its status, plus an error-level `reconciliation exhausted` log with the id, status and age. Later sweeps skip it without touching it, so the alert is raised once per transaction.

The sweep never scans the `transactions` table. It reads `open_transactions` (`migrations/007_open_transactions.cql`), an index of the transactions that are not terminal yet, spread over 16 partitions by the first hex character of the transaction id. A transaction's index row is written in the same batch as the transaction itself and deleted when a transition moves it to `completed`, `failed`, `reversed` or `reversal_failed`. That delete is best effort: a sweep that loads a transaction already terminal deletes its leftover row, while a row whose transaction can't be read yet is kept for the next sweep. The table's `gc_grace_seconds` is one hour (`migrations/008_open_transactions_gc_grace.cql`), so the tombstones those deletes leave become purgeable by compaction after one hour instead of the default ten days. A sweep therefore costs 16 partition reads plus at most one `IN` query per 100 open ids, however long the history gets; a transaction that reached `SWEEPER_MAX_AGE` stays in the index, still non-terminal, until it is resolved. Because a lost index write would hide a stuck transaction from the sweeper, each instance also rebuilds the index with a full scan of `transactions`, re-adding a row for every non-terminal transaction: once at start-up, before its first sweep, and again every `SWEEPER_FULL_SCAN_INTERVAL`. The rebuild runs inside the sweeper, so it only happens with `SWEEPER_ENABLED=true`, sweeps wait while it runs, and it logs `open index rebuilt` with the count, or `open index rebuild failed`.

**Known limitations.** Non-terminal transactions (`pending`, `debited` or `reversing`) can be found through `GET /accounts/{account_id}/transactions`; the sweeper described above resolves most of them on its own once they go stale. The exception is a step the account service dead-lettered as `ambiguous_write`: its idempotency key stays `pending` in `balance_operations` until the 30-day TTL expires, so every re-send hits the same `ambiguous_write` outcome until the sweeper reaches `SWEEPER_MAX_AGE` and raises the `reconciliation_exhausted` alert. Such a transaction needs manual resolution: before replaying anything, check the account's balance and the `balance_operations` row for the step's key (`<transaction id>:debit`, `:credit` or `:reversal`) to find out whether the balance change was applied, and resolve it well within the 30 days, since a replay after the key expired is applied again. As with the account service, a dead-lettered event replayed as-is is skipped as a duplicate, so a replay needs a new event id. On first deployment the `-replies` group reads `account.events` from the beginning; that is harmless and one-off, since a reply that does not match a known step is ignored.

Idempotency keys must be 1 to 64 printable ASCII characters (`0x21`–`0x7E`), with no spaces or control characters; the gateway already rejects anything else, and a command that still carries such a key is dead-lettered as `invalid_idempotency_key`.

**Upgrading from a release before Sprint 10.** `migrations/007_open_transactions.cql` and `008_open_transactions_gc_grace.cql` are applied automatically at start-up. Transactions that were already open before the upgrade have no index row yet; the first start of the new release rebuilds the index with a full scan before its first sweep, so the sweeper finds them without any manual step (with `SWEEPER_ENABLED=false`, the rebuild waits until the sweeper is turned on).

**Upgrading from a release before Sprint 8.** Client idempotency keys moved from `transactions_by_key`, one namespace shared by every account, to `transactions_by_account_key` (`migrations/006_transactions_by_account_key.cql`), keyed by account and key. Keys reserved in the old table before the upgrade are not seen by the new one, so a client that sends a request before the deploy and retries it after could create a second transaction. Stop or drain client retries across the deploy: let the requests sent before it settle, and don't retry them afterwards. The old table is no longer read or written.

**Upgrading from a release before Sprint 6.** Transactions that were already stuck before `balance_operations` existed may have had their balance change applied without a key being recorded, so the account service would apply a re-send again. Resolve them by hand before enabling the sweeper (deploy with `SWEEPER_ENABLED=false` until they are).

### Payment Service

```bash
cd services/payment-service
cp .env.example .env
docker compose up -d                  # hot reload with Air, published on :8084
curl http://localhost:8084/health     # {"success":true,"data":{"status":"healthy","cassandra":"up"}}
```

Migrations in `migrations/*.cql` run at boot against `CASSANDRA_KEYSPACE` (default `fintech_payments`); connecting to Cassandra, applying migrations and opening the keyspace session are retried the same way as the account service (`STARTUP_RETRY_ATTEMPTS` / `STARTUP_RETRY_DELAY`). Configuration reuses the transaction service's variable names, with `SERVER_PORT` defaulting to `8084`, `KAFKA_GROUP_ID` to `payment-service`, `CASSANDRA_KEYSPACE` to `fintech_payments` and the same `SWEEPER_*` reconciliation settings (including `SWEEPER_MAX_AGE`, default `24h`, which must be shorter than `720h`, and `SWEEPER_FULL_SCAN_INTERVAL`, default `24h`), plus `PAYMENT_WEBHOOK_SECRET` (required, at least 16 characters), `PAYMENT_WEBHOOK_URL` (default `http://localhost:8084/webhooks/gateway`), `PAYMENT_WEBHOOK_TOLERANCE` (default `5m`) and `PAYMENT_SETTLEMENT_DELAY` (default `2s`). The `dev-webhook-secret` value in `docker-compose.yml` and `.env.example` is for local development only; anywhere else, replace it with a random secret of at least 16 characters.

Consumes `payment.commands` and the account service's replies on `account.events`:

| Topic | Group | Handles |
|---|---|---|
| `payment.commands` | `KAFKA_GROUP_ID` | `payment.process`, `payment.submit`, `payment.settle` |
| `account.events` | `KAFKA_GROUP_ID-replies` | `account.credited`, `account.debited`, `account.debit_rejected`, `account.credit_rejected` |

A payment is recorded as `pending` with its idempotency key reserved first, then debits the account (`pending` → `debited`) and is submitted to the sandbox provider: PIX settles right away (`completed`), TED and boleto are bound to an external id and wait for a webhook (`debited` → `submitted` → `completed`). A rejection at the debit step fails the payment outright (`failed`, `insufficient_funds`, `account_not_active` or `account_not_found`); a rejection at submission or settlement instead refunds the debit (`refunding`) and lands on `refunded`, or on `refund_failed` plus a `payment.dlq` entry if the refund itself is rejected. Every status change is a Cassandra lightweight transaction guarded by the expected status, and results are published on `payment.events` (`payment.created`, `payment.processed`, `payment.completed`, `payment.failed`).

The sandbox provider reports settlement through a signed webhook: `POST /webhooks/gateway` with body `{external_id, status: settled|rejected, reason}`, headers `X-Timestamp` (unix seconds) and `X-Signature` (hex HMAC-SHA256 of `timestamp + "." + body`), accepted within `PAYMENT_WEBHOOK_TOLERANCE` of the current time. Responses: `202` with the command id, `401 INVALID_SIGNATURE`, `413 PAYLOAD_TOO_LARGE` (body over 64 KiB), `400 INVALID_JSON`, `422 VALIDATION_ERROR`, `503 PUBLISH_FAILED`.

**Sandbox values.**

| Input | Outcome |
|---|---|
| PIX key ending in `@reject.test` | rejected, `pix_key_not_found` |
| TED `bank_code` `999` | rejected, `invalid_destination` |
| Boleto code starting with `999` | rejected, `boleto_not_found` |
| Anything else | settles; TED and boleto settle after `PAYMENT_SETTLEMENT_DELAY` |

If a step's outcome never arrives, a payment is left stuck in `pending`, `debited`, `submitted` or `refunding` — for example when the sandbox loses a scheduled callback because the service restarted before `PAYMENT_SETTLEMENT_DELAY` elapsed. A reconciliation sweeper (`SWEEPER_ENABLED`, on by default) recovers it the same way as the transaction service's: every `SWEEPER_INTERVAL` it reads the open-payment index and picks the non-terminal payments stale for `SWEEPER_STALE_AFTER`, up to `SWEEPER_BATCH` per sweep, "touching" each one with a lightweight transaction conditioned on the status and `updated_at` it read so only one instance acts on it. A touched `pending` payment is re-debited; a `debited` one gets another `payment.submit`; a `submitted` one is checked against the sandbox provider again — settled completes it, rejected starts a refund, and one still pending simply gets its callback redelivered by the sandbox; a `refunding` payment has its refund credit re-sent. Re-sent commands reuse the payment's original idempotency keys, which the account service now deduplicates.

As in the transaction service, re-sending stops once a payment is older than `SWEEPER_MAX_AGE`: the first sweep past that age touches it one last time and raises a single `payment.command_failed` alert on `payment.dlq` with error code `reconciliation_exhausted` naming the payment and its status, plus an error-level `reconciliation exhausted` log with the id, status and age, and later sweeps skip it without touching it. The index works as in the transaction service: `open_payments` (`migrations/007_open_payments.cql`, `gc_grace_seconds` of one hour in `008_open_payments_gc_grace.cql`) holds the payments that are not terminal yet in 16 partitions by the first hex character of the payment id, a row is written with the payment and deleted, best effort, when it reaches `completed`, `failed`, `refunded` or `refund_failed`, sweeps prune the rows of payments already terminal and keep the rows of payments that can't be read yet, and each instance rebuilds the index with a full scan of `payments` at start-up and every `SWEEPER_FULL_SCAN_INTERVAL` (`0` keeps only the start-up rebuild).

**Known limitations.** Non-terminal payments can be found through `GET /accounts/{account_id}/payments`; the sweeper above resolves most of them on its own once they go stale. The exception is a step the account service dead-lettered as `ambiguous_write`: its idempotency key stays `pending` in `balance_operations` until the 30-day TTL expires, so every re-send hits the same `ambiguous_write` outcome until the sweeper reaches `SWEEPER_MAX_AGE` and raises the `reconciliation_exhausted` alert. Such a payment needs manual resolution: before replaying anything, check the account's balance and the `balance_operations` row for the step's key (`payment:<payment id>:debit` or `:refund`) to find out whether the balance change was applied, and resolve it well within the 30 days, since a replay after the key expired is applied again. Re-submitting a `debited` or `submitted` payment relies on the provider deduplicating submissions by payment id: the sandbox does, but a real provider must be given the payment id as an idempotency key, or a re-sent submission could pay twice. A settlement that arrives before its external id is bound, or while the submission is still being recorded, is retried and dead-lettered as `conflict` if it never lands. As with the other services, a dead-lettered event replayed as-is is skipped as a duplicate, so a replay needs a new event id. On first deployment the service reads `payment.commands` and `account.events` from the beginning.

**Upgrading from a release before Sprint 10.** `migrations/007_open_payments.cql` and `008_open_payments_gc_grace.cql` are applied automatically at start-up, and the first start of the new release rebuilds the index with a full scan before its first sweep, so payments that were already open before the upgrade are found by the sweeper without any manual step (with `SWEEPER_ENABLED=false`, the rebuild waits until the sweeper is turned on).

**Upgrading from a release before Sprint 6.** Payments that were already stuck before `balance_operations` existed may have had their debit or refund applied without a key being recorded, so the account service would apply a re-send again. Resolve them by hand before enabling the sweeper (deploy with `SWEEPER_ENABLED=false` until they are).

### Notification Service

```bash
cd services/notification-service
cp .env.example .env
docker compose up -d                  # hot reload with Air, published on :8085
curl http://localhost:8085/health     # {"success":true,"data":{"status":"healthy","redis":"up"}}
```

The Redis ping is retried at boot up to `STARTUP_RETRY_ATTEMPTS` times (default 30), waiting `STARTUP_RETRY_DELAY` between attempts (default `2s`), before the service gives up and exits. Configuration: `SERVER_*`, `KAFKA_BROKERS` / `KAFKA_GROUP_ID` (default `notification-service`) / `KAFKA_*_TIMEOUT` / `KAFKA_MAX_ATTEMPTS`, `CONSUMER_RETRY_BACKOFF` / `CONSUMER_DRAIN_TIMEOUT`, `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_DB`, `ACCOUNT_SERVICE_URL` / `ACCOUNT_DIRECTORY_TTL` (default `5m`) / `ACCOUNT_DIRECTORY_TIMEOUT` (default `3s`) / `ACCOUNT_DIRECTORY_MAX_ENTRIES` (default `10000`; values below `1` fall back to the default), `SMTP_ADDR` / `SMTP_FROM` (default `no-reply@fintech.local`) / `SMTP_TIMEOUT` (default `10s`), `NOTIFICATION_HISTORY_SIZE` (default `100`), `NOTIFICATION_MAX_EVENT_AGE` (default `1h`, `0` disables), `STARTUP_RETRY_ATTEMPTS` / `STARTUP_RETRY_DELAY`, `METRICS_ENABLED` / `OTEL_EXPORTER_OTLP_ENDPOINT` / `OTEL_SAMPLER_RATIO`, `LOG_LEVEL` / `LOG_PRETTY`.

The service runs two kinds of consumer: routing turns domain result events into delivery commands, and delivery sends them and records history.

| Topic | Group | Handles |
|---|---|---|
| `account.events` | `KAFKA_GROUP_ID-accounts` | routes `account.created` |
| `transaction.events` | `KAFKA_GROUP_ID-transactions` | routes `transaction.completed`, `transaction.failed`, `transaction.transfer_completed`, `transaction.transfer_failed` |
| `payment.events` | `KAFKA_GROUP_ID-payments` | routes `payment.completed`, `payment.failed` |
| `notification.events` | `KAFKA_GROUP_ID` | delivers `notification.email`, `notification.sms`, `notification.push`, keyed by user id |

Routing renders one of eight Portuguese templates and turns each result event into the channels below, looking up the account's owner through the account service and skipping accounts it doesn't know about:

| Event | Recipient | Channels |
|---|---|---|
| `account.created` | account owner | e-mail (welcome) |
| `transaction.completed` | account owner | push |
| `transaction.failed` | account owner | push + e-mail |
| `transaction.transfer_completed` | sender and counterparty | push to both |
| `transaction.transfer_failed` | sender | push + e-mail |
| `payment.completed` | account owner | push + e-mail |
| `payment.failed` | account owner | push + e-mail, plus SMS when `status` is `refund_failed` and the owner has a phone |

Transient routing and delivery failures are retried with `CONSUMER_RETRY_BACKOFF` and then dead-lettered as `notification.command_failed` on `notification.dlq`. Contacts come from the account service's internal `GET /accounts/{id}/owner` (`{account_id, user_id, name, email, phone?}`, `404 ACCOUNT_NOT_FOUND`) — not exposed by the gateway — cached in memory for `ACCOUNT_DIRECTORY_TTL`. The cache holds at most `ACCOUNT_DIRECTORY_MAX_ENTRIES` accounts: when it is full, expired entries are dropped first and then the one closest to expiring. Concurrent lookups of the same account share a single request, and a failed lookup, a `404` included, is not cached, so the next one asks the account service again.

Delivery sends e-mail over SMTP — Mailpit in development, with its web UI at http://localhost:8025 — and SMS and push through sandbox providers that just log the message; amounts in templates render as `R$ 1.234,56`, formatted from the event's exact amount. A malformed e-mail recipient is dead-lettered instead of retried, and every SMTP delivery — dial and whole conversation — is bounded by `SMTP_TIMEOUT`. Each event is processed at most once across crashes, because the processed marker is written before delivery: a crash mid-delivery can drop a notification, while an in-process retry after an ambiguous provider error can repeat one.

Redis keeps a `notification:processed:<event id>` marker for 7 days to dedupe deliveries and a `notification:history:<user id>` list, newest first, capped at `NOTIFICATION_HISTORY_SIZE` and expiring 90 days after the latest notification; the root compose runs Redis with `--maxmemory 256mb --maxmemory-policy noeviction`. History API: `GET /users/{user_id}/notifications?limit=` (default 20, 1–100, `422` otherwise) → `[{id, channel, recipient, subject?, body, source_event_id?, sent_at}]`, with the recipient masked (`a***@example.com`, `+55*******7766`; push keeps the user id).

Routing consumers read from the earliest retained offset and skip source events older than `NOTIFICATION_MAX_EVENT_AGE` (default `1h`), so a new consumer group or a long outage does not flood customers with stale notifications; the delivery group on `notification.events` also starts from the earliest offset, so a produced delivery command is never missed on redeploy; commands already routed before an outage have no age check and are still delivered late. The 7-day processed-marker TTL must stay longer than Kafka's 24 h log retention, so a replayed event is still recognised as processed.

**Redis sizing.** Redis never evicts keys to make room: when it reaches `maxmemory`, writes fail instead. Evicting was worse for this service, because an evicted processed marker silently turns a redelivered or replayed event back into a new one and its notification goes out again. With `noeviction`, a marker that can't be written is retried with `CONSUMER_RETRY_BACKOFF` and then handed back to the consumer uncommitted, so the consumer restarts and the event is processed again once Redis has room: notifications stall instead of repeating, and nothing is sent without its marker. A history entry that can't be written is logged as `notification history not recorded` and lost, since the notification itself already went out. Keys still expire on their own TTL (7 days for markers, 90 days for history). Size `maxmemory` for your notification volume — every event the routing consumers read on `account.events`, `transaction.events` and `payment.events`, routed or not, and every delivery command keeps a marker for 7 days, and every user keeps up to `NOTIFICATION_HISTORY_SIZE` history entries — and watch `used_memory` against `maxmemory` in `redis-cli INFO memory` so the limit is raised before it is reached. After upgrading from a release before Sprint 10, recreate the Redis container (`docker compose up -d redis` at the root) to apply the new limit and policy.

**Known limitations.** The history endpoint itself has no authentication: through the gateway it answers only for the user in the access token (see [Authentication and authorization](#authentication-and-authorization)), but the service trusts whoever reaches its own port. The account service's internal `GET /accounts/{id}/owner` is unauthenticated too and not proxied by the gateway. Both ports are published only for development.

#### Authentication and authorization

Every `/api/v1` route except `POST /api/v1/auth/register`, `POST /api/v1/auth/login` and `GET /api/v1/openapi.yaml` requires an access token in `Authorization: Bearer <token>`. Users register and log in with an e-mail and a password: the gateway checks them through the account service's internal identity endpoints (see [Account Service](#account-service)) and signs the token itself.

| Method | Path | Body | Answer |
|---|---|---|---|
| `POST` | `/api/v1/auth/register` | `{email, password}` | `201` `{user_id, access_token, token_type: "Bearer", expires_in}`, `409 EMAIL_TAKEN`, `422 VALIDATION_ERROR`, `503 SERVICE_BUSY` |
| `POST` | `/api/v1/auth/login` | `{email, password}` | `200` `{access_token, token_type: "Bearer", expires_in}`, `401 INVALID_CREDENTIALS` (with `WWW-Authenticate: Bearer`), `422 VALIDATION_ERROR`, `503 SERVICE_BUSY` |

The e-mail is trimmed and lower-cased, so `Ana@Example.com` and `ana@example.com` are the same user. Registration takes a valid e-mail of at most 254 characters (`details` `email`: `required`, `email` or `max`) and a password of 8 to 72 bytes (`password`: `required` or `length`), and answers with a new random user id. Login only requires both fields (`required`) and gives the same `401 INVALID_CREDENTIALS` for an unknown e-mail, a wrong password, an e-mail that could never have been registered and a password longer than 72 bytes, so the answer doesn't tell which one it was. Registration, on the other hand, does reveal whether an e-mail is already registered: a second registration answers `409 EMAIL_TAKEN`, which is why both routes sit behind the stricter rate limit below. Both answer `413 PAYLOAD_TOO_LARGE` for a body over 16 KiB, `503 SERVICE_BUSY` when the account service is already hashing as many passwords as it allows (retry after a short pause) and `502 UPSTREAM_UNAVAILABLE` when the account service can't be reached or answers anything unexpected, a redirect included (the gateway never follows one).

**Tokens.** The access token is a JWT signed with HMAC-SHA256 (HS256) and `JWT_SECRET`, carrying `sub` (the user id), `iss` (`fintech-gateway`), `iat`, `exp` and `jti` (a random id). It lives for `JWT_TTL` (default `1h`, at most `24h`); `expires_in` is that lifetime in seconds. The gateway accepts only HS256 tokens it issued itself: any other `alg` (`none` included), another issuer, a bad signature, a missing or past `exp` (with 30 s of leeway for clock skew) or a `sub` that isn't a user id is refused. There are no refresh tokens and no revocation: when a token expires, the client logs in again, and a token that leaks stays valid until it expires. A missing, malformed or refused token answers `401 UNAUTHORIZED` with `WWW-Authenticate: Bearer`. The gateway removes the `Authorization` header before it proxies a read, so the domain services never see the token.

**Rate limit.** On top of the general limit, `/auth/register` and `/auth/login` share a stricter one: `AUTH_RATE_LIMIT_REQUESTS` (default `10`) per `AUTH_RATE_LIMIT_WINDOW` (default `1m`), counted per client address exactly like the general one (an IPv6 client by its /64 network) and answering `429 RATE_LIMIT_EXCEEDED`. It slows down password guessing; raise it only for test runs (see [End-to-end and load tests](#end-to-end-and-load-tests)). The limit is per client address only: there is no per-account lockout, so a guesser spread over many addresses is slowed only by bcrypt and the hashing bound above.

**Ownership.** A user reaches only their own data. The gateway enforces it on every route; the domain services don't check who is calling.

| Route | Allowed when |
|---|---|
| `POST /accounts` | `user_id` is omitted — the account is created for the token's user — or equals the token's user |
| `GET`, `PATCH`, `DELETE /accounts/{id}` | the account belongs to the token's user |
| `GET /accounts/{account_id}/transactions`, `GET /accounts/{account_id}/payments` | the account belongs to the token's user |
| `POST /transactions`, `POST /payments` | the body's `account_id` belongs to the token's user |
| `POST /transfers` | the body's `from_account_id` belongs to the token's user; `to_account_id` may be anyone's |
| `GET /transactions/{id}` | the transaction's account belongs to the token's user, or, for a transfer, its counterparty does — the counterparty gets it without the sender's `description` and `idempotency_key` |
| `GET /payments/{id}` | the payment's account belongs to the token's user |
| `GET /users/{user_id}/accounts`, `GET /users/{user_id}/notifications` | `user_id` is the token's user |

Anything else answers `403 FORBIDDEN`. An account's owner comes from the account service's internal `GET /accounts/{id}/owner` and is kept in the gateway's memory for `OWNER_CACHE_TTL` (default `1m`, at most 10,000 accounts); an account's owner never changes, so the cache can't hand an account to someone else. An account the account service doesn't know answers `404 ACCOUNT_NOT_FOUND` from the gateway, whether it is named in the path or in a command body, and that answer isn't cached, so an account created a moment ago is found as soon as it exists; a failed lookup answers `502 UPSTREAM_UNAVAILABLE`. Command bodies are validated first, so an invalid command still answers `422` before its account is looked up. An account id in the path of `GET /accounts/{id}`, `GET /accounts/{account_id}/transactions` or `GET /accounts/{account_id}/payments` that is not a UUID answers `422 VALIDATION_ERROR` (`details` `{"id":"uuid"}` or `{"account_id":"uuid"}`) from the gateway itself, before any lookup; a percent-encoded id is decoded for the ownership check and then refused the same way, so it never reaches a service. `GET /transactions/{id}` and `GET /payments/{id}` fetch the record and hand it over only to its owner, so an unknown id still answers `404 TRANSACTION_NOT_FOUND` or `404 PAYMENT_NOT_FOUND`, while another user's record answers `403`. The recipient of a transfer can read it by id, but the gateway removes `description` and `idempotency_key` first, since those were chosen by the sender.

The services behind the gateway trust it: there is no service-to-service authentication, so their own ports (`8082`–`8085`), with the account service's internal identity and owner endpoints, must not be reachable from outside. The compose files publish them only for development; the payment service's webhook, which checks its own signature, is the one route a provider needs to reach.

```bash
curl -s -X POST localhost:8081/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@example.com","password":"correct-horse-battery"}'
# {"success":true,"data":{"user_id":"5d1e7c2a-0a6b-4c1e-9f4e-2b6f7a8c9d01","access_token":"eyJhbGciOiJIUzI1NiIs…","token_type":"Bearer","expires_in":3600}}

TOKEN=$(curl -s -X POST localhost:8081/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@example.com","password":"correct-horse-battery"}' | jq -r .data.access_token)

curl -s localhost:8081/api/v1/users/5d1e7c2a-0a6b-4c1e-9f4e-2b6f7a8c9d01/accounts -H "Authorization: Bearer $TOKEN"
# {"success":true,"data":[]}

curl -s localhost:8081/api/v1/users/5d1e7c2a-0a6b-4c1e-9f4e-2b6f7a8c9d01/accounts
# {"success":false,"error":{"code":"UNAUTHORIZED","message":"authentication required"}}
```

**Upgrading from a release before Sprint 11.** Every client now has to register or log in and send the token on every call, and `POST /accounts` no longer needs `user_id`. Existing customers have no password, so they register like anyone else — with the same e-mail if they like, since identities live in their own table — and get a new user id: accounts created earlier with a user id the client picked are not linked to it, so they can no longer be reached through the API, although they stay in the database untouched. Commands and statement reads that name an account that doesn't exist are now refused by the gateway with `404 ACCOUNT_NOT_FOUND`. Set `JWT_SECRET` to a random value of at least 32 bytes before deploying (the gateway refuses to start without one); the development value in the compose file and `.env.example` is public. The account service applies `008_identities_by_email.cql` at start-up; deploy it before or together with the gateway, because registration and login call its new identity endpoints and answer `502 UPSTREAM_UNAVAILABLE` until they exist.

#### Command endpoints

Every write is accepted asynchronously: the gateway validates the body, checks that the caller may act on the account (see [Authentication and authorization](#authentication-and-authorization)), publishes a command to Kafka and answers `202` with the command id and the trace id (`X-Request-ID`).

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
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"account_type":"checking","name":"Ana Souza","email":"ana@example.com","document":"52998224725"}'
# {"success":true,"data":{"command_id":"…","trace_id":"…"}}
```

Errors: `400 INVALID_JSON`, `401 UNAUTHORIZED`, `403 FORBIDDEN`, `404 ACCOUNT_NOT_FOUND`, `413 PAYLOAD_TOO_LARGE` (body over 1 MiB), `422 VALIDATION_ERROR` (with per-field `details`), `422 EMPTY_UPDATE` (PATCH without fields), `429 RATE_LIMIT_EXCEEDED`, `502 UPSTREAM_UNAVAILABLE` when the account owner can't be looked up, `503 PUBLISH_FAILED` when the broker is unreachable or the circuit is open. A handler that panics — in the gateway or in any service — answers `500 INTERNAL_ERROR` in the same JSON envelope, unless it had already started its response, and is logged at error level as `handler panicked` with the panic value, stack, request id, method, path and trace ids.

`idempotency_key`, required on `POST /transactions`, `/transfers` and `/payments`, must be 1 to 64 printable ASCII characters with no spaces or control characters (`422 idempotency_key: idempotency_key` otherwise).

**Transaction flow**: `POST /transactions` (deposit/withdrawal) and `POST /transfers` are accepted with `202`; the transaction service records the transaction as `pending`, asks the account service to debit/credit, and settles it as `completed` or `failed` (`insufficient_funds`, `account_not_active`, `account_not_found`, `balance_limit_exceeded`); a transfer whose credit is rejected after the debit is compensated (`reversed`, or `reversal_failed` + `transaction.dlq` when the compensation itself is rejected). The same `idempotency_key` never creates a second transaction on the same account — the transaction's account for a deposit or withdrawal, the sender's for a transfer; different accounts may use the same key independently.

**Payment flow**: `POST /payments` requires a `ted` object (`bank_code`, `branch`, `account`, `document`) for TED payments — rejected on any other method (`422 ted: excluded`) — and validates `boleto_code`'s check digits (`422 boleto_code: boleto`) and, when the code encodes a nonzero amount, that `amount` matches it to the cent (`422 amount: boleto_amount`); the payment service debits the account, submits to a sandbox provider, and settles as `completed` (PIX right away, TED and boleto after a signed webhook) or refunds the debit and settles as `failed`. The same `idempotency_key` never creates a second payment on the same account.

#### Amounts

Money is exact end to end. Every `amount` in a request, every money field in a response and every money field in a Kafka event is a decimal string in BRL with two decimal places (`"1234.50"`); inside the services it is a `domain.Amount`, an integer number of cents, and Cassandra keeps the same cents in `bigint` columns. No amount is ever a binary floating-point number, so three deposits of `"0.10"` followed by a withdrawal of `"0.30"` leave the balance at exactly `"0.00"`.

Requests should send `amount` as a string with up to two decimal places (`"1234.5"` and `"1234.50"` are the same amount). A JSON number is still accepted for compatibility and is read from its literal text, so `1234.56` is exactly 123456 cents; send strings anyway, since the client's own JSON encoder may already have rounded a number. The rules, on `POST /transactions`, `/transfers` and `/payments`:

| Amount | Answer |
|---|---|
| Not a decimal with at most two decimal places — `"1.234"`, `1.234`, `"1,00"`, `"abc"` | `422 VALIDATION_ERROR`, `details` `{"amount":"amount"}`, reported on its own, before any other field is checked |
| Missing, zero or negative | `422`, `{"amount":"gt"}` |
| Above `9999999999999.99` | `422`, `{"amount":"lte"}` |
| A boleto whose code encodes a different amount | `422`, `{"amount":"boleto_amount"}`, compared in cents |

The `gt` and `lte` checks run after the per-field tag validation, so a request that also has other invalid fields reports those instead and is rejected either way.

```bash
curl -s -X POST localhost:8081/api/v1/transactions \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"account_id":"7e47434d-40d8-4288-afc8-358b73999896","type":"deposit","amount":"1234.50","currency":"BRL","description":"Salary","idempotency_key":"salary-2026-09"}'
# {"success":true,"data":{"command_id":"…","trace_id":"…"}}

curl -s localhost:8081/api/v1/accounts/7e47434d-40d8-4288-afc8-358b73999896 -H "Authorization: Bearer $TOKEN"
# {"success":true,"data":{"account_id":"7e47434d-…","status":"active","currency":"BRL","balance":"1234.50",…}}

curl -s localhost:8081/api/v1/accounts/7e47434d-40d8-4288-afc8-358b73999896/transactions -H "Authorization: Bearer $TOKEN"
# {"success":true,"data":[{"transaction_id":"f7c4b83f-…","type":"deposit","status":"completed","amount":"1234.50","currency":"BRL","to_balance_after":"1234.50",…}]}

curl -s -X POST localhost:8081/api/v1/transactions \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"account_id":"7e47434d-40d8-4288-afc8-358b73999896","type":"deposit","amount":"1.234","currency":"BRL","idempotency_key":"too-precise"}'
# {"success":false,"error":{"code":"VALIDATION_ERROR","message":"request validation failed","details":{"amount":"amount"}}}
```

Responses carry `balance` (accounts), `amount`, `from_balance_after` and `to_balance_after` (transactions), and `amount` and `balance_after` (payments) as strings. Events on Kafka carry every money field (`amount`, `balance`, `balance_after`) the same way; consumers also accept a JSON number, which older releases published: the envelope is decoded keeping each number's literal text, so a plain decimal literal such as `1234.56` or `92233720368547758.07` is converted to cents exactly at any size, never through a binary float, and an envelope followed by any other data is dead-lettered as `invalid_event`; an event whose amount has more than two decimal places or does not fit in 64-bit cents is dead-lettered as `bad_payload`.

**Upgrading from a release before Sprint 9.** Read responses used to return money as JSON numbers and now return decimal strings, so clients must parse `balance`, `amount`, `balance_after`, `from_balance_after` and `to_balance_after` as strings; requests with numeric amounts keep working. Events written by older releases carry numbers and remain readable, as do the replies stored in `balance_operations` before the upgrade, and nothing in Cassandra changes. Older services can't decode the string amounts this release publishes, though, so every service has to run a release that reads `Amount` before any of them publishes one. Sprint 9 is the first release that reads both forms, so upgrading from an earlier one is a single coordinated deploy: stop the old gateway and all four domain services, start the new release everywhere, and only then let traffic back in, so that no old instance is left to consume an event a new one published.

#### Read endpoints

Reads are proxied to the account service via `ACCOUNT_SERVICE_URL`, to the transaction service via `TRANSACTION_SERVICE_URL`, to the payment service via `PAYMENT_SERVICE_URL` and to the notification service via `NOTIFICATION_SERVICE_URL` (default `http://localhost:8085`); `502 UPSTREAM_UNAVAILABLE` when the upstream is down. Reads need the same bearer token and pass the ownership rules above before they are proxied, so on top of the answers below every read can answer `401 UNAUTHORIZED` and `403 FORBIDDEN`, and the account-scoped ones `404 ACCOUNT_NOT_FOUND` from the gateway.

| Method | Path | Upstream |
|---|---|---|
| `GET` | `/api/v1/accounts/{id}` | `GET /accounts/{id}` → `200` account (`balance` as a decimal string in BRL), `404 ACCOUNT_NOT_FOUND`, `422` |
| `GET` | `/api/v1/users/{user_id}/accounts` | `GET /users/{user_id}/accounts` → `200` list |
| `GET` | `/api/v1/transactions/{id}` | `GET /transactions/{id}` → `200` transaction (`status` pending/debited/completed/failed/reversing/reversed/reversal_failed, no balances), `404 TRANSACTION_NOT_FOUND`, `422` |
| `GET` | `/api/v1/accounts/{account_id}/transactions` | `GET /accounts/{account_id}/transactions?limit=50&before=` → `200` newest first (`limit` 1–200), with the balances seen from `account_id`, `X-Next-Before` when the page is full, `422` |
| `GET` | `/api/v1/payments/{id}` | `GET /payments/{id}` → `200` payment (`status` pending/debited/submitted/completed/failed/refunding/refunded/refund_failed, TED `document` masked), `404 PAYMENT_NOT_FOUND`, `422` |
| `GET` | `/api/v1/accounts/{account_id}/payments` | `GET /accounts/{account_id}/payments?limit=50&before=` → `200` newest first (`limit` 1–200), TED `document` masked, `X-Next-Before` when the page is full, `422` |
| `GET` | `/api/v1/users/{user_id}/notifications` | `GET /users/{user_id}/notifications?limit=20` → `200` newest first (`limit` 1–100), `422` |

Balances after a transaction are only shown to the account they belong to. In an account's transaction list, a deposit carries `to_balance_after` and a withdrawal `from_balance_after` once the balance has moved; a transfer carries `from_balance_after` in the sender's list and `to_balance_after` in the recipient's list, never both, so neither side sees the other's balance. `GET /transactions/{id}` returns no balances at all. A TED payment's `document` (CPF or CNPJ) is returned with every character but the last two replaced by `*` (`*********25`), both by id and in lists.

An account's transaction or payment list reads the newest ids from the account's index table (`transactions_by_account`, `payments_by_account`) and then fetches the records themselves with batched `IN` queries of at most 100 ids each, so a full 200-item page costs three Cassandra queries instead of one read per row; the page keeps the index's newest-first order, and an id whose record is missing is skipped.

**Paging a statement.** Both lists accept `before`, an RFC 3339 timestamp with or without fractional seconds (`2026-09-20T10:00:00Z`, `2026-09-20T10:00:00.123Z`), and then return only rows created strictly before it; an unparsable value answers `422 VALIDATION_ERROR` with `details` `{"before":"datetime"}`. When a page is full (as many rows as `limit`), the response carries an `X-Next-Before` header with the last row's `created_at` in RFC 3339 with nanoseconds: send it back, URL-encoded, as `before` to get the next, older page, and stop when a response comes without it (a last page that happens to be full still carries the header, and the request after it returns an empty list). The gateway forwards `before` and `X-Next-Before` unchanged and lists `X-Next-Before` in `CORS_EXPOSED_HEADERS` by default, so browser clients can read it; a value you set yourself replaces that list, so keep `X-Next-Before` in it (a `services/api-gateway/.env` that sets `CORS_EXPOSED_HEADERS=Link` hides the header from browsers).

```bash
curl -s -D - -H "Authorization: Bearer $TOKEN" "localhost:8081/api/v1/accounts/7e47434d-40d8-4288-afc8-358b73999896/transactions?limit=2"
# X-Next-Before: 2026-09-20T10:00:00.123Z
curl -s -H "Authorization: Bearer $TOKEN" "localhost:8081/api/v1/accounts/7e47434d-40d8-4288-afc8-358b73999896/transactions?limit=2&before=2026-09-20T10%3A00%3A00.123Z"
```

The cursor is a timestamp, and Cassandra keeps `created_at` to the millisecond, so a row created in the same millisecond as the last row of a page, but left off that page, is skipped by the next one. That takes two records of one account created within the same millisecond, right at a page boundary.

## Observability

Every service exposes Prometheus metrics on `GET /metrics` of its own HTTP port (`:8081` for the gateway from the host, `:8082`–`:8085` for the domain services) and propagates OpenTelemetry traces over HTTP and Kafka. All five read the same three variables:

| Variable | Default | Effect |
|---|---|---|
| `METRICS_ENABLED` | `true` | `false` leaves `/metrics` unmounted |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | empty | OTLP/HTTP endpoint for spans, such as `http://jaeger:4318`; empty exports nothing but still propagates trace context and logs trace ids |
| `OTEL_SAMPLER_RATIO` | `1.0` | Root sampling ratio, parent-based; `<= 0` or `> 1` means `1` |

**Metrics**, all labelled with `service`: `http_requests_total{method,route,status}` and `http_request_duration_seconds{method,route}` on every router; `messages_processed_total{type,outcome}` (`ok`, `duplicate`, `dead_lettered`), `message_processing_duration_seconds{type}` and `message_retries_total{type}` from every message processor; `messages_published_total{topic,outcome}` (`ok`, `error`) from every producer; `kafka_consumer_lag{topic,group}` (lag of the latest fetched partition) from every consumer; `circuit_breaker_state` on the gateway (`0` closed, `1` half-open, `2` open); `reconciliation_resent_total{status}` and `reconciliation_exhausted_total` from the transaction and payment sweepers; `notifications_sent_total{channel,outcome}` from notification delivery; plus the Go runtime and process collectors.

**Traces.** Each HTTP request gets a server span named after its route, the gateway's read proxy propagates `traceparent` to the domain services, every Kafka publish injects `traceparent` into the message headers and every consumer continues it in a `process <event type>` span, so one command is one trace across the services it touches. The gateway starts that trace itself: a client's `traceparent` is only recorded as a link and its `baggage` is dropped. `/health` and `/metrics` are not traced. Request and processor log lines carry `otel_trace_id` and `otel_span_id`. The envelope's `trace_id` (the gateway's `X-Request-ID`) is a separate business correlation id.

**Running it.** The root compose's `observability` profile adds Prometheus, Grafana and Jaeger, configured from `observability/`. The simplest way to get everything wired is the stack script, which starts the profile and exports `OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4318` to the services (unless already set):

```bash
STACK_OBSERVABILITY=1 scripts/stack.sh up
STACK_OBSERVABILITY=1 scripts/stack.sh wait   # also waits for Prometheus, Grafana and Jaeger
```

Starting services one by one works too: `docker compose --profile observability up -d` at the root, then `OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4318 docker compose up -d` in each service directory. Prometheus scrapes the services by their compose hostnames, so they have to run in their compose stacks.

| UI | URL | Notes |
|---|---|---|
| Prometheus | http://localhost:9090 (`PROMETHEUS_PORT`) | Five scrape jobs every 15 s, alert rules on the Alerts page |
| Grafana | http://localhost:3000 (`GRAFANA_PORT`) | Anonymous read-only access; `admin` / `GRAFANA_ADMIN_PASSWORD` (default `admin`) to edit. Home dashboard "Platform overview" (uid `fintech-overview`): health, HTTP rate, 5xx ratio and p95 latency, messages by outcome, dead letters, consumer lag, reconciliation and notifications, with a link to the service's traces |
| Jaeger | http://localhost:16686 (`JAEGER_UI_PORT`) | `jaegertracing/jaeger:2.20.0` (2.21 removed the `/api/*` query API that Grafana's Jaeger data source uses); OTLP on 4317/4318 inside the compose network only |

**Alerts** (`observability/prometheus/alerts.yml`, evaluated by Prometheus, no Alertmanager configured):

| Alert | Fires when | Severity |
|---|---|---|
| `ServiceDown` | a scrape target is down for 1 minute | critical |
| `HighErrorRate` | more than 5 % of a service's requests answer 5xx over 5 minutes, for 5 minutes | warning |
| `DeadLettersGrowing` | a message processor dead-lettered a message in the last 10 minutes | warning |
| `ConsumerLagHigh` | a consumer is more than 1000 messages behind for 5 minutes | warning |
| `ReconciliationExhausted` | a sweeper gave up on a transaction or payment in the last 15 minutes | critical |
| `CircuitBreakerOpen` | the gateway's circuit breaker is open for 1 minute | critical |

The full metric table, span layout, dashboard panels and the two hops that start a new trace (sweeper re-sends and the sandbox webhook) are in [`docs/architecture.md`](docs/architecture.md#observability).

## Development

The service containers rebuild themselves with Air when a file of their own module changes, but Air does not watch `pkg/`, which is mounted next to the module: after changing a shared package, restart the service containers that use it (`docker restart fintech-transaction-service`, or `docker compose restart` in the service directory) so they rebuild with the new code.

[`CONTRIBUTING.md`](CONTRIBUTING.md) has the Docker-only way to run every gate below (a `golang:1.25-alpine` container per module, `gofmt`, `go vet`, coverage and integration tests), the commit style and the checklist for adding a service. With Go installed locally, the Makefiles do the same:

```bash
# shared packages
cd pkg && make test && make lint            # go test ./... · gofmt check

# api gateway
cd services/api-gateway
make test                                   # unit + feature, coverage of ./internal/...
make test-coverage                          # writes coverage.html
make test-integration                       # needs KAFKA_BROKERS pointing at a broker

# account service
cd services/account-service
make test                                   # unit + feature, coverage of ./internal/app/...
make test-coverage                          # writes coverage.html
make test-integration                       # needs KAFKA_BROKERS and CASSANDRA_HOSTS

# transaction service
cd services/transaction-service
make test                                   # unit + feature, coverage of ./internal/app/...
make test-coverage                          # writes coverage.html
make test-integration                       # needs KAFKA_BROKERS and CASSANDRA_HOSTS

# payment service
cd services/payment-service
make test                                   # unit + feature, coverage of ./internal/app/...
make test-coverage                          # writes coverage.html
make test-integration                       # needs KAFKA_BROKERS and CASSANDRA_HOSTS

# notification service
cd services/notification-service
make test                                   # unit + feature, coverage of ./internal/app/...
make test-coverage                          # writes coverage.html
make test-integration                       # needs KAFKA_BROKERS, REDIS_ADDR, and SMTP_ADDR / MAILPIT_URL pointing at Mailpit
```

### End-to-end and load tests

`scripts/stack.sh` runs the whole platform out of process, for tests that go through the gateway instead of a single service: `up` starts the root infrastructure (Kafka, Cassandra, Redis, Mailpit, plus the `kafka-init` one-shot) under the `fintech-bank-platform` project name and then every service's own compose stack; `wait` polls the gateway's, every domain service's and Mailpit's health endpoints until they answer or `STACK_TIMEOUT` (default 600s) elapses; `logs` dumps the last 200 lines of every service container; `down` tears everything down. It loads the root `.env` without overriding variables already set in the environment, and honours `GATEWAY_URL`, `MAILPIT_URL` (built from `MAILPIT_UI_PORT` if unset) and the root compose's own `REDIS_PORT` / `MAILPIT_SMTP_PORT` / `MAILPIT_UI_PORT`. With `STACK_OBSERVABILITY=1`, `up` also starts the `observability` profile and exports `OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4318` to the services, and `wait` also polls Prometheus, Grafana and Jaeger (`PROMETHEUS_URL`, `GRAFANA_URL` and `JAEGER_URL`, built from their ports if unset).

```bash
scripts/stack.sh up
STACK_TIMEOUT=900 scripts/stack.sh wait
```

`tests/e2e` is a separate Go module (build tag `e2e`) that drives the running stack through the gateway, registering a user for every customer it creates and sending that user's token on every call: registration and login (`401 UNAUTHORIZED` without a token or with a bad one, `INVALID_CREDENTIALS` for a wrong password or an unknown e-mail, `EMAIL_TAKEN` for a second registration), ownership (another user's accounts, statements, transactions, payments and notifications answer `403`, as do commands on another user's account, and unknown accounts answer `404`), account creation, deposits/withdrawals/transfers (including a transfer that gets reversed, which balances each side sees, and idempotency keys scoped per account), payments (including sandbox rejections, webhook settlement, TED document masking and a boleto whose amount doesn't match its code), exact decimal amounts (string amounts compared exactly, a numeric amount still accepted, three `"0.10"` deposits and a `"0.30"` withdrawal leaving `"0.00"`, and the `amount` and `lte` rejections), statement pagination (five deposits read two at a time by following `X-Next-Before`), key format validation and the notifications they trigger. `TestMain` skips the suite if the gateway isn't healthy, unless `E2E_REQUIRED=1` makes that a hard failure instead; `GATEWAY_URL` and `MAILPIT_URL` point it at the stack. The suite fires enough requests to hit the gateway's default rate limit and registers far more users than the auth limit's 10 a minute, so `services/api-gateway/docker-compose.yml` passes `RATE_LIMIT_REQUESTS` and `AUTH_RATE_LIMIT_REQUESTS` through from the environment — raise both when starting the stack, since `scripts/stack.sh up` otherwise leaves the auth limit at 10 a minute (the suite defaults to `GATEWAY_URL=http://localhost:8081` and `MAILPIT_URL=http://localhost:8025`):

```bash
export RATE_LIMIT_REQUESTS=100000 AUTH_RATE_LIMIT_REQUESTS=100000
scripts/stack.sh up
scripts/stack.sh wait
(cd tests/e2e && E2E_REQUIRED=1 go test -count=1 -tags e2e ./... -v)
scripts/stack.sh down
```

`tests/load` has k6 scenarios exercising the gateway end to end — commands, reads and a raw deposit-throughput benchmark — that register a user per customer and send its token, so they need both limits raised the same way; see [`tests/load/README.md`](tests/load/README.md) for the scripts, scenarios and how to run them.

CI (`.github/workflows/ci.yml`) runs on every push and pull request as seven jobs — `pkg`, `api-gateway` (with a Kafka service container, also linting `services/api-gateway/api/openapi.yaml` with `redocly/cli`), `account-service`, `transaction-service` and `payment-service` (with Kafka and Cassandra service containers), `notification-service` (with Kafka, Redis and Mailpit service containers) and `e2e` (with `RATE_LIMIT_REQUESTS` and `AUTH_RATE_LIMIT_REQUESTS` raised, running `scripts/stack.sh up`/`wait`, the `tests/e2e` suite and `scripts/stack.sh down`) — running `gofmt` check and the test suites for `pkg`, `api-gateway`, `account-service`, `transaction-service`, `payment-service` and `notification-service`, failing the build if coverage drops below 100 % (of `internal/app` for the account, transaction, payment and notification services).

## Project structure

```
fintech-bank-platform/
├── docker-compose.yml         Kafka · Cassandra · Redis · Mailpit (+ ui and observability profiles)
├── .env.example               every variable the platform reads
├── .github/workflows/ci.yml   gofmt + tests + coverage gate + OpenAPI lint
├── CONTRIBUTING.md            prerequisites, checks with Docker, commit style, adding a service
├── redocly.yaml               OpenAPI lint rules
├── docs/architecture.md       components, topics, sequence diagrams, idempotency, failures, observability
├── observability/
│   ├── prometheus/            prometheus.yml (scrape jobs) · alerts.yml (alert rules)
│   └── grafana/               provisioning/ (data sources, dashboards) · dashboards/platform-overview.json
├── scripts/stack.sh           up/down/wait/logs for the whole stack (used by the e2e job)
├── pkg/                       shared Go module
│   ├── logger/  errors/  response/  validation/  events/
│   ├── env/  middleware/  messaging/
│   ├── domain/  cassandra/  processor/  retry/
│   ├── metrics/  tracing/
│   ├── Makefile · Dockerfile · docker-compose.yml
│   └── README.md
├── services/
│   ├── api-gateway/
│   │   ├── cmd/main.go                    entry point
│   │   ├── api/openapi.yaml               OpenAPI 3.1 document, embedded and served
│   │   ├── internal/
│   │   │   ├── config/                    env → typed Config
│   │   │   ├── contracts/                 interfaces for config, context, http and auth
│   │   │   ├── app/handlers/              command endpoints, register/login, ownership guard, read proxy, OpenAPI handler
│   │   │   └── infrastructure/
│   │   │       ├── auth/                  JWT issuer and verifier, bearer middleware
│   │   │       ├── identity/              account service identity client
│   │   │       ├── owners/                account owner lookup with an in-memory cache
│   │   │       ├── http/                  server, router, handlers, middleware/
│   │   │       └── messaging/             kafka producer, circuit breaker
│   │   ├── tests/  (unit/ · feature/ · integration/)     testify-style TestCase helpers
│   │   ├── Makefile · Dockerfile · docker-compose.yml · .air.toml
│   │   └── .env.example
│   ├── account-service/
│   │   ├── cmd/main.go                    entry point
│   │   ├── migrations/                    numbered .cql files, applied at boot
│   │   ├── internal/
│   │   │   ├── config/                    env → typed Config
│   │   │   ├── contracts/                 interfaces for config, messaging and repositories
│   │   │   ├── app/
│   │   │   │   ├── models/                domain types
│   │   │   │   ├── services/              account, customer and identity use cases
│   │   │   │   └── handlers/              command dispatcher, DLQ, read and identity endpoints
│   │   │   └── infrastructure/
│   │   │       ├── database/              Cassandra repositories and migrations
│   │   │       └── http/                  server, router, health, read handlers
│   │   ├── tests/  (unit/ · feature/ · integration/)
│   │   ├── Makefile · Dockerfile · docker-compose.yml · .air.toml
│   │   └── .env.example
│   ├── transaction-service/
│   │   ├── cmd/main.go                    entry point
│   │   ├── migrations/                    numbered .cql files, applied at boot
│   │   ├── internal/
│   │   │   ├── config/                    env → typed Config
│   │   │   ├── contracts/                 interfaces for config, messaging and repositories
│   │   │   ├── app/
│   │   │   │   ├── models/                domain types
│   │   │   │   ├── services/              transaction use cases and saga transitions
│   │   │   │   └── handlers/              command and reply dispatchers, read endpoints
│   │   │   └── infrastructure/
│   │   │       ├── database/              Cassandra repositories and migrations
│   │   │       └── http/                  server, router, health, read handlers
│   │   ├── tests/  (unit/ · feature/ · integration/)
│   │   ├── Makefile · Dockerfile · docker-compose.yml · .air.toml
│   │   └── .env.example
│   ├── payment-service/
│   │   ├── cmd/main.go                    entry point
│   │   ├── migrations/                    numbered .cql files, applied at boot
│   │   ├── internal/
│   │   │   ├── config/                    env → typed Config
│   │   │   ├── contracts/                 interfaces for config, messaging and repositories
│   │   │   ├── app/
│   │   │   │   ├── models/                domain types
│   │   │   │   ├── services/              payment use cases and saga transitions
│   │   │   │   └── handlers/              command, reply and webhook dispatchers, read endpoints
│   │   │   └── infrastructure/
│   │   │       ├── database/              Cassandra repositories and migrations
│   │   │       ├── gateway/               sandbox provider simulator
│   │   │       └── http/                  server, router, health, read handlers
│   │   ├── tests/  (unit/ · feature/ · integration/)
│   │   ├── Makefile · Dockerfile · docker-compose.yml · .air.toml
│   │   └── .env.example
│   └── notification-service/
│       ├── cmd/main.go                    entry point
│       ├── internal/
│       │   ├── config/                    env → typed Config
│       │   ├── contracts/                 interfaces for config, messaging, senders and storage
│       │   ├── app/
│       │   │   ├── models/                domain types
│       │   │   ├── services/              routing, rendering and delivery, templates/
│       │   │   └── handlers/              history read endpoint
│       │   └── infrastructure/
│       │       ├── directory/             account service owner client, in-memory cache
│       │       ├── senders/               SMTP and sandbox SMS/push senders
│       │       ├── storage/               Redis client, idempotency store and history
│       │       └── http/                  server, router, health, history handler
│       ├── tests/  (unit/ · feature/ · integration/)
│       ├── Makefile · Dockerfile · docker-compose.yml · .air.toml
│       └── .env.example
└── tests/
    ├── e2e/                    Go module (build tag e2e): auth, accounts, transactions, statements, payments, notifications
    └── load/                   k6 scenarios (commands, reads, throughput) · README.md
```

Each future service follows the same layout: `cmd/`, `internal/{config,contracts,infrastructure,app}` and `tests/`, with `migrations/` (CQL) for the ones that persist to Cassandra; [`CONTRIBUTING.md`](CONTRIBUTING.md#adding-a-service) lists everything a new service needs.

## Roadmap

- [x] **Sprint 0** — Docker Compose infrastructure (Kafka KRaft, Cassandra, Redis, debug UIs)
- [x] **Shared packages** — logger, errors, response, validation, events, with CI and 100 % coverage
- [x] **Sprint 1 — API Gateway** — HTTP skeleton, middleware, config, Kafka producer with circuit breaker and command endpoints
- [x] **Sprint 2 — Account Service** — customers and accounts in Cassandra, balance with compare-and-set, result events, read API proxied by the gateway
- [x] **Sprint 3 — Transaction Service** — deposits, withdrawals and transfers as sagas over the account service, idempotency keys, compensation, read API proxied by the gateway
- [x] **Sprint 4 — Payment Service** — PIX, TED and boleto as sagas over the account service, sandbox provider with signed webhooks, refunds, read API proxied by the gateway
- [x] **Sprint 5 — Notification Service** — e-mail, SMS and push from result events, templates, Redis-backed idempotency and history, proxied by the gateway
- [x] **Sprint 6** — end-to-end tests, k6 load tests, idempotent balance operations and a reconciliation sweeper for stuck transactions and payments
- [x] **Sprint 7** — observability (Prometheus metrics, Grafana dashboard and alerts, OpenTelemetry traces in Jaeger), OpenAPI and architecture docs
- [x] **Sprint 8** — hardening: rate-limit key spoofing, panic handling, per-account idempotency, validation alignment and read privacy
- [x] **Sprint 9** — exact decimal money in the API and events, batched statements and trusted proxy hops
- [x] **Sprint 10** — operational robustness: open-record sweeps, statement pagination, exact legacy numbers and Redis without eviction
- [x] **Sprint 11** — authentication: e-mail/password identities, JWT access tokens and owner-only access on every route

## License

[MIT](LICENSE) © [Gabriel Silva](https://github.com/GabeSilvaDev)

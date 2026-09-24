# Contributing

Thanks for looking into the project. This guide covers what you need installed, how the repository is laid out, how to run every check CI runs, how commits are written and how to add a new service. The design itself is described in [`docs/architecture.md`](docs/architecture.md).

## Prerequisites

- **Docker** with Compose v2 — the only hard requirement. Every Go command below runs in the `golang:1.25-alpine` image, so a local Go toolchain is optional (Go 1.25 if you want one).
- **bash** and **curl** for `scripts/stack.sh`.
- About 4 GB of RAM for the full stack (Kafka, Cassandra, Redis, Mailpit and the five services).

```bash
cp .env.example .env
docker compose up -d                          # Kafka, Cassandra, Redis, Mailpit (+ kafka-init)
docker compose --profile observability up -d  # optional: Prometheus, Grafana, Jaeger
```

## Repository layout

```
docker-compose.yml     root infrastructure (profiles: ui, observability)
.env.example           root variables (ports, rate limits, JWT secret, observability)
observability/         Prometheus scrape jobs and alert rules, Grafana provisioning and dashboard
scripts/stack.sh       up/down/wait/logs for the whole platform
redocly.yaml           OpenAPI lint rules
pkg/                   shared Go module (github.com/fintech-bank-platform/pkg)
services/<name>/       one Go module per service
tests/e2e/             Go module driving the running stack through the gateway (build tag e2e)
tests/load/            k6 scenarios
docs/                  architecture
```

Every Go module is independent: `pkg`, the five services and `tests/e2e` each have their own `go.mod`. The services import the shared module through `replace github.com/fintech-bank-platform/pkg => ../../pkg`, so Docker commands must mount the repository root, not just the module.

A service is laid out as:

```
services/<name>/
├── cmd/main.go                 wiring: config, logger, tracing, metrics, consumers, HTTP server
├── internal/
│   ├── config/                 env → typed Config
│   ├── contracts/              interfaces (config, messaging, repositories, …)
│   ├── app/
│   │   ├── models/             domain types
│   │   ├── services/           use cases, saga transitions, sweepers
│   │   └── handlers/           command/reply dispatchers, read endpoints
│   └── infrastructure/         Cassandra/Redis repositories, HTTP server and router, clients
├── migrations/                 numbered .cql files applied at boot (Cassandra services only)
├── tests/
│   ├── unit/  feature/         testify-style suites, run by the coverage gate
│   └── integration/            build tag integration, need real infrastructure
├── Makefile · Dockerfile · docker-compose.yml · .air.toml
└── .env.example
```

## Running the checks

Run everything from the repository root. The two named volumes keep the module and build caches between runs, and coverage profiles are written to `/tmp` inside the container so nothing lands in the working tree:

```bash
docker run --rm \
  -v "$PWD":/src \
  -v fintech-go-mod:/go/pkg/mod \
  -v fintech-go-build:/root/.cache/go-build \
  -w /src/services/account-service \
  golang:1.25-alpine sh -c '
    test -z "$(gofmt -l .)" &&
    go vet ./... &&
    go vet -tags integration ./... &&
    go test ./tests/... -coverprofile=/tmp/cover.out -coverpkg=./internal/app/... &&
    go tool cover -func=/tmp/cover.out | tail -1'
```

Change `-w` and the test line per module:

| Module (`-w /src/...`) | Tests and coverage | Gate |
|---|---|---|
| `pkg` | `go test -cover ./...` | 100.0 % for every package |
| `services/api-gateway` | `go test ./tests/... -coverprofile=/tmp/cover.out -coverpkg=./internal/...` | 100.0 % total of `./internal/...` |
| `services/account-service`, `transaction-service`, `payment-service`, `notification-service` | `go test ./tests/... -coverprofile=/tmp/cover.out -coverpkg=./internal/app/...` | 100.0 % total of `./internal/app/...` |
| `tests/e2e` | `go vet -tags e2e ./...` (the suite itself needs the stack, see below) | vet clean |

For every module, `gofmt -l .` must print nothing and `go vet ./...` must pass, including `go vet -tags integration ./...` where integration tests exist; `tests/e2e` has only tagged files, so it is vetted with `-tags e2e` alone. CI (`.github/workflows/ci.yml`) runs the formatting check and the tests with the coverage gate for `pkg` and every service, plus each service's integration suite against service containers, lints the OpenAPI document in the `api-gateway` job, and runs the end-to-end suite last.

With Go installed locally, the Makefiles offer the same through `make test`, `make test-coverage` (writes `coverage.html`), `make test-unit`, `make test-feature` and `make test-integration` in each service, and `make test`, `make test-coverage` and `make lint` (gofmt check) in `pkg`.

### Integration tests

Integration tests (`tests/integration`, build tag `integration`) talk to real infrastructure. Start the root compose, then run the suite on the host network with the variables CI uses:

| Module | Variables |
|---|---|
| `api-gateway` | `KAFKA_BROKERS=localhost:9092` |
| `account-service`, `transaction-service`, `payment-service` | `KAFKA_BROKERS=localhost:9092`, `CASSANDRA_HOSTS=localhost:9042` |
| `notification-service` | `KAFKA_BROKERS=localhost:9092`, `REDIS_ADDR=localhost:6379`, `SMTP_ADDR=localhost:1025`, `MAILPIT_URL=http://localhost:8025` |

```bash
docker compose up -d
docker run --rm --network host \
  -v "$PWD":/src -v fintech-go-mod:/go/pkg/mod -v fintech-go-build:/root/.cache/go-build \
  -e KAFKA_BROKERS=localhost:9092 -e CASSANDRA_HOSTS=localhost:9042 \
  -w /src/services/payment-service \
  golang:1.25-alpine go test -tags integration ./tests/integration/... -v
```

The Kafka integration tests of the account, transaction, payment and notification services don't need an empty broker: before producing, they give every reader a fresh consumer group whose offsets are committed at the current end of its topic, so messages already on the topic are skipped, and they retry that positioning for up to 30 s while a broker that has just started can't answer yet. They can run against the same root compose the rest of the stack uses.

`--network host` works on Linux. Elsewhere, join the compose network instead (`--network fintech-bank-platform_fintech-network`) and use the in-network addresses: `kafka:29092`, `cassandra:9042`, `redis:6379`, `mailpit:1025` and `http://mailpit:8025`.

### End-to-end tests

`scripts/stack.sh` starts the root infrastructure and every service's compose stack. The suite exceeds the gateway's default rate limit and registers a user for every customer it creates, far more than the 10 a minute the auth limit allows, so raise both `RATE_LIMIT_REQUESTS` and `AUTH_RATE_LIMIT_REQUESTS` before starting it (without them, `scripts/stack.sh up` leaves the auth limit at 10 a minute and the suite runs into `429 RATE_LIMIT_EXCEEDED`); add `STACK_OBSERVABILITY=1` to get Prometheus, Grafana and Jaeger with traces exported.

```bash
export RATE_LIMIT_REQUESTS=100000 AUTH_RATE_LIMIT_REQUESTS=100000
scripts/stack.sh up
scripts/stack.sh wait
docker run --rm --network host \
  -v "$PWD":/src -v fintech-go-mod:/go/pkg/mod -v fintech-go-build:/root/.cache/go-build \
  -e E2E_REQUIRED=1 -w /src/tests/e2e \
  golang:1.25-alpine go test -count=1 -tags e2e ./... -v
scripts/stack.sh logs   # when something fails
scripts/stack.sh down
```

Without `E2E_REQUIRED=1` the suite skips itself when the gateway is not healthy. `GATEWAY_URL` (default `http://localhost:8081`) and `MAILPIT_URL` (default `http://localhost:8025`) point it elsewhere.

### Load tests

k6 scenarios live in `tests/load`; they register users too, so they need both rate limits raised. See [`tests/load/README.md`](tests/load/README.md) for the scripts, scenarios and the pinned `grafana/k6` image.

### OpenAPI

The public API is described in `services/api-gateway/api/openapi.yaml`, embedded in the gateway binary and served at `GET /api/v1/openapi.yaml`. Update it with every change to a gateway route, request or response, and lint it the way CI does:

```bash
docker run --rm -v "$PWD:/spec" redocly/cli:2.54.2 lint /spec/services/api-gateway/api/openapi.yaml
```

The rules come from `redocly.yaml` (`recommended`, with `no-server-example.com` off).

## Commits

Commits follow gitmoji with a conventional type and scope, subject line only:

```
<emoji> <type>(<scope>): <subject>
```

- English, imperative mood, lower case, no trailing period, no body.
- The scope is the module or area touched: a service (`account-service`), a shared package (`pkg/metrics`), `observability`, `e2e`, `load`, `scripts`, `docker`; omit it for repository-wide changes.
- Common pairs: `✨ feat`, `🐛 fix`, `🥅 fix`/`feat` (error handling), `♻️ refactor`, `✅ test`, `📝 docs`, `🔧 chore`, `👷 ci`.

Examples from the history:

```
✨ feat(payment-service): expose metrics and propagate traces
🐛 fix(pkg/env): ignore non-finite floats
✅ test(api-gateway): assert the half-open circuit breaker state
🔧 chore(observability): add prometheus, grafana and jaeger to the stack
♻️ refactor(transaction-service): touch stale transactions only once per observed update
🥅 fix(pkg/cassandra): treat unknown lightweight transaction outcomes as ambiguous
👷 ci: run the end-to-end suite against the compose stack
📝 docs: document reconciliation, start-up retries and the test suites
```

Keep a commit to one module or concern, and keep the coverage gate green in each one.

## Adding a service

1. **Module.** Create `services/<name>/` with the layout above and a `go.mod` for `github.com/fintech-bank-platform/<name>` that requires `github.com/fintech-bank-platform/pkg v0.0.0` with `replace github.com/fintech-bank-platform/pkg => ../../pkg`.
2. **Wiring.** In `cmd/main.go`, follow the existing services: typed config from the environment (including `METRICS_ENABLED`, `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_SAMPLER_RATIO`), `tracing.Init` and `metrics.New("<name>")`, `tracing.Middleware` and the metrics middleware on the router with `GET /metrics` when metrics are enabled, `GET /health`, `pkg/processor` processors behind its consumers with the service's own `<domain>.command_failed` type and DLQ topic, and `retry.Do` around start-up dependencies.
3. **Topics.** Add new topics and event types to `pkg/events` and to the `kafka-init` loop in the root `docker-compose.yml`.
4. **Compose.** Add `Dockerfile`, `.air.toml` (with `send_interrupt = true` and a `kill_delay` of `35s`, like the other services, so a hot reload lets the HTTP server and the consumers drain before the process is killed) and a `docker-compose.yml` whose container is `fintech-<name>`, mounts `../../pkg`, passes `OTEL_EXPORTER_OTLP_ENDPOINT=${OTEL_EXPORTER_OTLP_ENDPOINT:-}` through and joins the external network `fintech-bank-platform_fintech-network`. Add the service to `SERVICES`, the health URLs and the log containers in `scripts/stack.sh`.
5. **CI.** Add a job to `.github/workflows/ci.yml` modelled on an existing one (service containers for its infrastructure, gofmt check, unit and feature tests with `-coverpkg=./internal/app/...` and the 100 % gate, integration tests) and list it in the `e2e` job's `needs`.
6. **Observability.** Add a scrape job for `<name>:<port>` to `observability/prometheus/prometheus.yml`; the dashboard and alerts pick it up through the `service` label.
7. **Docs.** Add `.env.example` with every variable the service reads, its section in both READMEs, and its topics and flows in `docs/architecture.md`.

## Secrets

Never commit secrets. `.env` files are git-ignored; commit only `.env.example` files, with empty values or values that are safe for local development. The values that exist today — `PAYMENT_WEBHOOK_SECRET=dev-webhook-secret`, `JWT_SECRET=dev-only-jwt-secret-change-me-0123456789` and `GRAFANA_ADMIN_PASSWORD=admin` — are development defaults only and must be replaced anywhere else; the gateway refuses to start with a `JWT_SECRET` shorter than 32 bytes. When you add a variable, add it to the relevant `.env.example` and document it in the README.

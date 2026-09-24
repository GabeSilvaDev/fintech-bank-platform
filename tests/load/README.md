# Load tests

k6 scripts exercising the gateway (`:8081`) end to end: commands (writes through Kafka and the sagas), reads (proxied to the account, transaction and payment services) and a raw throughput benchmark for deposits.

## Prerequisite: raise the rate limits

The gateway rate-limits requests per IP (`RATE_LIMIT_REQUESTS`, default 100/min) and applies a stricter, separate limit to `POST /auth/register` and `POST /auth/login` (`AUTH_RATE_LIMIT_REQUESTS`, default 10/min). Load scenarios exceed the first in seconds, and every script's `setup()` registers one user per customer (20 for `commands.js` / `reads.js`, at least 10 for `throughput.js`), which trips the second, so recreate the gateway with both limits raised before running anything here:

```bash
cd services/api-gateway && RATE_LIMIT_REQUESTS=100000 AUTH_RATE_LIMIT_REQUESTS=100000 docker compose up -d
```

## Authentication

Every `/api/v1` route except `/auth/register` and `/auth/login` requires `Authorization: Bearer <token>`, and each customer can only touch its own accounts. `setup()` therefore registers a fresh user per customer (unique `@load.test` e-mail), keeps the returned access token next to the account id, and creates the account without `user_id` (the gateway takes it from the token). Every request then sends the owning customer's token: reads and deposits use the customer's own token, and a transfer uses the sender's token. Tokens live for `JWT_TTL` (default 1 h), longer than any scenario here.

## Running

```bash
docker run --rm -i --network host -e BASE_URL=http://localhost:8081 -e SCENARIO=load -v "$PWD/tests/load:/scripts" grafana/k6:2.3.0 run /scripts/commands.js
```

`grafana/k6:2.3.0` is the pinned image (k6 v2.3.0) used for every run in this document; it exposes the Web Crypto `crypto.randomUUID()` global, which `lib.js` uses directly (falling back to a `Math.random` v4 generator if it's ever missing).

- `BASE_URL` — gateway URL, defaults to `http://localhost:8081`.
- `SCENARIO` — one of `smoke`, `load`, `stress`, `spike`, `soak` (defaults to `smoke`). Applies to `commands.js` and `reads.js`.
- `N` — deposit count for `throughput.js`, defaults to `500`.

```bash
docker run --rm -i --network host -e BASE_URL=http://localhost:8081 -e SCENARIO=smoke -v "$PWD/tests/load:/scripts" grafana/k6:2.3.0 run /scripts/reads.js
docker run --rm -i --network host -e BASE_URL=http://localhost:8081 -e N=500 -v "$PWD/tests/load:/scripts" grafana/k6:2.3.0 run /scripts/throughput.js
```

## Scripts

Every payload sends `amount` as a decimal string with two decimals (`"1000000.00"`, `"42.50"`), the representation the API recommends and returns.

- **`lib.js`** — shared helpers: `BASE_URL`, `uuid()`, JSON headers, `authHeaders(token)`, `registerUser()` (registers a fresh user and returns its id and access token), `createCustomer()` (registers a user, creates its account and polls `GET /users/{user_id}/accounts` until it shows up, returning `{ userId, token, accountId }`), `fundAccount()` / `createFundedCustomer()` (deposit, poll until settled, and throw if the deposit didn't settle as `completed` or its `amount` doesn't come back as the exact decimal string that was sent), `waitForStatus()`, `scenarios(name)` and the shared `thresholds`.
- **`commands.js`** — `setup()` registers 20 users, creates an account for each and funds it with a 1,000,000 BRL deposit; the default function randomly fires a deposit, a transfer between two setup customers (sent with the sender's token), or a PIX payment to `ana@example.com`, each with a fresh idempotency key, and checks for `202`.
- **`reads.js`** — `setup()` creates the same 20 funded customers; the default function reads the account, its transaction list and its payment list, checking for `200`.
- **`throughput.js`** — submits `N` deposits as fast as possible across 10 VUs (`shared-iterations` executor) against 10 pre-created accounts (one more account per 200 deposits once `N` exceeds 2,000, so no account holds more deposits than the 200-item page the transaction list allows), then polls every account's transaction list until all `N` deposits reach a final status. Counts real `202`s (`commands_accepted`), deposits that actually settled as `completed` (`completed_deposits`, feeding `completed_per_second`) separately from any that settled as a non-`completed` terminal status (`settlement_failures`) or never settled within the poll timeout (`unresolved_after_timeout`); thresholds fail the run if `settlement_failures` or `unresolved_after_timeout` is ever above zero.

## Scenarios (`scenarios(name)` in `lib.js`)

| Name | Executor | Shape |
|---|---|---|
| `smoke` | `constant-vus` | 1 VU for 30 s |
| `load` | `constant-arrival-rate` | 50 req/s for 2 m |
| `stress` | `ramping-arrival-rate` | 10 → 200 req/s over 3 m |
| `spike` | `ramping-arrival-rate` | 10 → 300 req/s (30 s), holds 300 for 20 s, back to 10 (30 s) |
| `soak` | `constant-arrival-rate` | 30 req/s for 10 m |

## Thresholds

```js
{ http_req_failed: ['rate<0.01'], http_req_duration: ['p(95)<300'] }
```

Applied to `commands.js` and `reads.js`. `throughput.js` is a raw throughput measurement with two correctness thresholds instead: `settlement_failures: ['count==0']` and `unresolved_after_timeout: ['count==0']` — it fails the run if any submitted deposit settles as anything other than `completed` or has not settled when the 120 s poll ends.

## Results (local run)

Machine: laptop, all five services running under Docker Compose with Air hot reload, plus Kafka, Cassandra, Redis and Mailpit — and other unrelated projects' containers sharing the same host. Numbers are indicative, not a clean benchmark environment.

| Script | Scenario | Requests/s | p95 | p99 | Error rate | End-to-end completions/s |
|---|---|---|---|---|---|---|
| `commands.js` | `smoke` (1 VU × 30 s) | 62.0 req/s | 11.77 ms | 13.59 ms | 0.00% | n/a |
| `commands.js` | `load` (50 req/s × 2 m) | 40.1 req/s¹ | 11.85 ms | 12.89 ms | 0.00% | n/a |
| `reads.js` | `smoke` (1 VU × 30 s) | 136.2 req/s | 4.23 ms | 4.83 ms | 0.00% | n/a |
| `reads.js` | `load` (50 req/s × 2 m) | 122.0 req/s¹ | 4.37 ms | 4.87 ms | 0.00% | n/a |
| `throughput.js` | N=500, 10 VUs | 797.4 commands/s² (500/500 accepted) | 55.81 ms | 75.96 ms | 0.00% | 33.82 (500/500 completed) |

¹ Overall average including the ~35 s `setup()` phase (customer creation + funding), which is not part of the `load` scenario's own timer; inside the 2-minute constant-arrival-rate window the configured 50 req/s was sustained (6,001/6,001 iterations completed, no drops).
² Rate at which the 500 deposit commands were verified as accepted (`202`, all 500 of them) during the ~0.6 s submission burst across 10 VUs; `end-to-end completions/s` is the more meaningful number — it's the rate at which those same deposits actually settled as `completed` (500/500, zero `settlement_failures`, zero `unresolved_after_timeout`) once the account and transaction services processed them off Kafka.

Both `http_req_failed < 1%` and `p(95) < 300 ms` thresholds passed on every `commands.js` and `reads.js` run above, with real 0.00% error rates — no threshold loosening was needed. All five services stayed healthy (`GET /health` on `808{1..5}`) during and after every run, and none needed a restart.

`stress`, `spike` and `soak` were not run locally for this task (they're multi-minute, higher-throughput scenarios not worth burning laptop resources on outside a dedicated benchmarking pass) — the scripts support all five scenarios via `SCENARIO=stress|spike|soak`.

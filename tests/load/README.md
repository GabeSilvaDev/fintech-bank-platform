# Load tests

k6 scripts exercising the gateway (`:8081`) end to end: commands (writes through Kafka and the sagas), reads (proxied to the account, transaction and payment services) and a raw throughput benchmark for deposits.

## Prerequisite: raise the rate limit

The gateway rate-limits requests per IP (`RATE_LIMIT_REQUESTS`, default 100/min). Load scenarios exceed that in seconds, so recreate the gateway with a much higher limit before running anything here:

```bash
cd services/api-gateway && RATE_LIMIT_REQUESTS=100000 docker compose up -d
```

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

- **`lib.js`** — shared helpers: `BASE_URL`, `uuid()`, JSON headers, `createCustomer()` (creates an account and polls `GET /users/{user_id}/accounts` until it shows up), `fundAccount()` / `createFundedCustomer()` (deposit and poll until `completed`), `waitForStatus()`, `scenarios(name)` and the shared `thresholds`.
- **`commands.js`** — `setup()` creates 20 customers and funds each with a 1,000,000 BRL deposit; the default function randomly fires a deposit, a transfer between two setup customers, or a PIX payment to `ana@example.com`, each with a fresh idempotency key, and checks for `202`.
- **`reads.js`** — `setup()` creates the same 20 funded customers; the default function reads the account, its transaction list and its payment list, checking for `200`.
- **`throughput.js`** — submits `N` deposits as fast as possible across 10 VUs (`shared-iterations` executor) against 10 pre-created accounts, then polls every account's transaction list until all `N` deposits reach a final status, reporting `commands_accepted_per_second` and `completed_per_second`.

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

Applied to `commands.js` and `reads.js`. `throughput.js` has no pass/fail thresholds — it's a raw throughput measurement.

## Results (local run)

Machine: laptop, all five services running under Docker Compose with Air hot reload, plus Kafka, Cassandra, Redis and Mailpit — and other unrelated projects' containers sharing the same host. Numbers are indicative, not a clean benchmark environment.

| Script | Scenario | Requests/s | p95 | p99 | Error rate | End-to-end completions/s |
|---|---|---|---|---|---|---|
| `commands.js` | `smoke` (1 VU × 30 s) | 62.0 req/s | 11.77 ms | 13.59 ms | 0.00% | n/a |
| `commands.js` | `load` (50 req/s × 2 m) | 40.1 req/s¹ | 11.85 ms | 12.89 ms | 0.00% | n/a |
| `reads.js` | `smoke` (1 VU × 30 s) | 136.2 req/s | 4.23 ms | 4.83 ms | 0.00% | n/a |
| `reads.js` | `load` (50 req/s × 2 m) | 122.0 req/s¹ | 4.37 ms | 4.87 ms | 0.00% | n/a |
| `throughput.js` | N=500, 10 VUs | 803.9 commands/s² | 59.65 ms | 76.84 ms | 0.00% | 33.68 |

¹ Overall average including the ~35 s `setup()` phase (customer creation + funding), which is not part of the `load` scenario's own timer; inside the 2-minute constant-arrival-rate window the configured 50 req/s was sustained (6,001/6,001 iterations completed, no drops).
² Rate at which the 500 deposit commands themselves were accepted (`202`) during the ~0.6 s submission burst across 10 VUs; `end-to-end completions/s` is the more meaningful number — it's the rate at which those same 500 deposits actually settled as `completed` once the account and transaction services processed them off Kafka.

Both `http_req_failed < 1%` and `p(95) < 300 ms` thresholds passed on every `commands.js` and `reads.js` run above, with real 0.00% error rates — no threshold loosening was needed. All five services stayed healthy (`GET /health` on `808{1..5}`) during and after every run, and none needed a restart.

`stress`, `spike` and `soak` were not run locally for this task (they're multi-minute, higher-throughput scenarios not worth burning laptop resources on outside a dedicated benchmarking pass) — the scripts support all five scenarios via `SCENARIO=stress|spike|soak`.

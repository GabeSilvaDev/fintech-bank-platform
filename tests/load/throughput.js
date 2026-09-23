import http from 'k6/http';
import { check, sleep } from 'k6';
import { scenario } from 'k6/execution';
import { Counter, Trend } from 'k6/metrics';
import { BASE_URL, jsonHeaders, uuid, createCustomer, TRANSACTION_FINAL_STATUSES, summaryTrendStats } from './lib.js';

const N = __ENV.N ? parseInt(__ENV.N, 10) : 500;
const VUS = 10;
const ACCOUNT_COUNT = 10;
const POLL_TIMEOUT_MS = 120000;

const commandsAccepted = new Counter('commands_accepted');
const commandsAcceptedPerSecond = new Trend('commands_accepted_per_second');
const completedPerSecond = new Trend('completed_per_second');
const unresolvedAfterTimeout = new Counter('unresolved_after_timeout');

export const options = {
  scenarios: {
    throughput: {
      executor: 'shared-iterations',
      vus: VUS,
      iterations: N,
      maxDuration: '5m',
    },
  },
  setupTimeout: '180s',
  summaryTrendStats,
};

export function setup() {
  const accounts = [];
  for (let i = 0; i < ACCOUNT_COUNT; i += 1) {
    accounts.push(createCustomer());
  }

  const plan = [];
  for (let i = 0; i < N; i += 1) {
    plan.push({
      accountId: accounts[i % accounts.length].accountId,
      idempotencyKey: uuid(),
    });
  }

  return { plan, accounts, start: Date.now() };
}

export default function (data) {
  const item = data.plan[scenario.iterationInTest];
  const res = http.post(`${BASE_URL}/api/v1/transactions`, JSON.stringify({
    account_id: item.accountId,
    type: 'deposit',
    amount: 10,
    currency: 'BRL',
    idempotency_key: item.idempotencyKey,
  }), { headers: jsonHeaders });
  const ok = check(res, { 'deposit accepted (202)': (r) => r.status === 202 });
  if (ok) {
    commandsAccepted.add(1);
  }
}

function pollUntilAllSettled(plan, timeoutMs) {
  const remaining = new Map();
  plan.forEach((item) => remaining.set(item.idempotencyKey, true));
  const accountIds = Array.from(new Set(plan.map((item) => item.accountId)));
  const deadline = Date.now() + timeoutMs;

  while (remaining.size > 0 && Date.now() < deadline) {
    accountIds.forEach((accountId) => {
      const res = http.get(`${BASE_URL}/api/v1/accounts/${accountId}/transactions?limit=200`, { headers: jsonHeaders });
      if (res.status === 200) {
        const parsed = JSON.parse(res.body);
        const items = parsed.data || [];
        items.forEach((tx) => {
          if (remaining.has(tx.idempotency_key) && TRANSACTION_FINAL_STATUSES.indexOf(tx.status) !== -1) {
            remaining.delete(tx.idempotency_key);
          }
        });
      }
    });
    if (remaining.size > 0) {
      sleep(0.5);
    }
  }
  return remaining.size;
}

export function teardown(data) {
  const submissionEnd = Date.now();
  const submissionSeconds = (submissionEnd - data.start) / 1000;
  commandsAcceptedPerSecond.add(N / submissionSeconds);

  const stillUnresolved = pollUntilAllSettled(data.plan, POLL_TIMEOUT_MS);
  const completionEnd = Date.now();
  const completionSeconds = (completionEnd - data.start) / 1000;
  completedPerSecond.add((N - stillUnresolved) / completionSeconds);
  if (stillUnresolved > 0) {
    unresolvedAfterTimeout.add(stillUnresolved);
  }
}

export function handleSummary(data) {
  const metric = (name, field) => {
    const m = data.metrics[name];
    if (!m || !m.values || m.values[field] === undefined) {
      return 'n/a';
    }
    return m.values[field];
  };

  const lines = [];
  lines.push('');
  lines.push('=== throughput.js summary ===');
  lines.push(`N (planned deposits): ${N}`);
  lines.push(`http_reqs total: ${metric('http_reqs', 'count')}`);
  lines.push(`http_req_duration p(95): ${metric('http_req_duration', 'p(95)')} ms`);
  lines.push(`http_req_duration p(99): ${metric('http_req_duration', 'p(99)')} ms`);
  lines.push(`http_req_failed rate: ${metric('http_req_failed', 'rate')}`);
  lines.push(`checks rate: ${metric('checks', 'rate')}`);
  lines.push(`commands accepted/s: ${metric('commands_accepted_per_second', 'avg')}`);
  lines.push(`end-to-end completions/s: ${metric('completed_per_second', 'avg')}`);
  lines.push(`unresolved after timeout: ${metric('unresolved_after_timeout', 'count')}`);
  lines.push('');

  return { stdout: lines.join('\n') };
}

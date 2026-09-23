import http from 'k6/http';
import { check, sleep } from 'k6';
import { scenario } from 'k6/execution';
import { Counter, Trend } from 'k6/metrics';
import { BASE_URL, jsonHeaders, uuid, createCustomer, TRANSACTION_FINAL_STATUSES, summaryTrendStats } from './lib.js';

const N = __ENV.N ? parseInt(__ENV.N, 10) : 500;
const VUS = 10;
const MAX_LIST_LIMIT = 200;
const ACCOUNT_COUNT = Math.max(10, Math.ceil(N / MAX_LIST_LIMIT));
const PER_ACCOUNT = Math.ceil(N / ACCOUNT_COUNT);
const POLL_TIMEOUT_MS = 120000;

const commandsAccepted = new Counter('commands_accepted');
const completedDeposits = new Counter('completed_deposits');
const settlementFailures = new Counter('settlement_failures');
const unresolvedAfterTimeout = new Counter('unresolved_after_timeout');
const submissionDurationSeconds = new Trend('submission_duration_seconds');
const completedPerSecond = new Trend('completed_per_second');

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
  thresholds: {
    settlement_failures: ['count==0'],
    unresolved_after_timeout: ['count==0'],
  },
};

export function setup() {
  if (!(N > 0)) {
    throw new Error(`N must be a positive integer, got ${__ENV.N}`);
  }

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
  const resolvedStatus = new Map();
  const accountIds = Array.from(new Set(plan.map((item) => item.accountId)));
  const deadline = Date.now() + timeoutMs;

  while (remaining.size > 0 && Date.now() < deadline) {
    accountIds.forEach((accountId) => {
      const res = http.get(`${BASE_URL}/api/v1/accounts/${accountId}/transactions?limit=${PER_ACCOUNT}`, { headers: jsonHeaders });
      if (res.status === 200) {
        const parsed = JSON.parse(res.body);
        const items = parsed.data || [];
        items.forEach((tx) => {
          if (remaining.has(tx.idempotency_key) && TRANSACTION_FINAL_STATUSES.indexOf(tx.status) !== -1) {
            resolvedStatus.set(tx.idempotency_key, tx.status);
            remaining.delete(tx.idempotency_key);
          }
        });
      }
    });
    if (remaining.size > 0) {
      sleep(0.5);
    }
  }
  return { resolvedStatus, unresolvedCount: remaining.size };
}

export function teardown(data) {
  const submissionEnd = Date.now();
  submissionDurationSeconds.add((submissionEnd - data.start) / 1000);

  const { resolvedStatus, unresolvedCount } = pollUntilAllSettled(data.plan, POLL_TIMEOUT_MS);
  const completionEnd = Date.now();
  const completionSeconds = (completionEnd - data.start) / 1000;

  let completedCount = 0;
  let failedCount = 0;
  resolvedStatus.forEach((status) => {
    if (status === 'completed') {
      completedCount += 1;
    } else {
      failedCount += 1;
    }
  });

  completedDeposits.add(completedCount);
  completedPerSecond.add(completedCount / completionSeconds);
  if (failedCount > 0) {
    settlementFailures.add(failedCount);
  }
  if (unresolvedCount > 0) {
    unresolvedAfterTimeout.add(unresolvedCount);
  }
}

export function handleSummary(data) {
  const metric = (name, field) => {
    const m = data.metrics[name];
    if (!m || !m.values || m.values[field] === undefined) {
      return 0;
    }
    return m.values[field];
  };

  const acceptedCount = metric('commands_accepted', 'count');
  const submissionSeconds = metric('submission_duration_seconds', 'avg');
  const acceptedPerSecond = submissionSeconds > 0 ? acceptedCount / submissionSeconds : 0;
  const completedCount = metric('completed_deposits', 'count');
  const failuresCount = metric('settlement_failures', 'count');
  const unresolvedCount = metric('unresolved_after_timeout', 'count');
  const completedRate = metric('completed_per_second', 'avg');

  const lines = [];
  lines.push('');
  lines.push('=== throughput.js summary ===');
  lines.push(`N (planned deposits): ${N}`);
  lines.push(`http_reqs total: ${metric('http_reqs', 'count')}`);
  lines.push(`http_req_duration p(95): ${metric('http_req_duration', 'p(95)')} ms`);
  lines.push(`http_req_duration p(99): ${metric('http_req_duration', 'p(99)')} ms`);
  lines.push(`http_req_failed rate: ${metric('http_req_failed', 'rate')}`);
  lines.push(`checks rate: ${metric('checks', 'rate')}`);
  lines.push(`commands accepted: ${acceptedCount}/${N} (${acceptedPerSecond}/s)`);
  lines.push(`completed deposits: ${completedCount}/${N}`);
  lines.push(`settlement failures (non-completed terminal status): ${failuresCount}`);
  lines.push(`unresolved after timeout: ${unresolvedCount}`);
  lines.push(`end-to-end completions/s: ${completedRate}`);
  lines.push('');

  return { stdout: lines.join('\n') };
}

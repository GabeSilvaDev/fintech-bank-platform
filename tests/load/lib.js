import http from 'k6/http';
import { check, sleep } from 'k6';

export const BASE_URL = __ENV.BASE_URL || 'http://localhost:8081';

export const jsonHeaders = { 'Content-Type': 'application/json' };

export function authHeaders(token) {
  return { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` };
}

const CPFS = ['52998224725', '11144477735', '12345678909', '98765432100'];
let cpfIndex = 0;

export const TRANSACTION_FINAL_STATUSES = ['completed', 'failed', 'reversed', 'reversal_failed'];
export const PAYMENT_FINAL_STATUSES = ['completed', 'failed', 'refunded', 'refund_failed'];

export function uuid() {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

function nextCpf() {
  const cpf = CPFS[cpfIndex % CPFS.length];
  cpfIndex += 1;
  return cpf;
}

export function registerUser() {
  const email = uuid() + '@load.test';
  const password = 'Load-' + uuid();
  const res = http.post(`${BASE_URL}/api/v1/auth/register`, JSON.stringify({ email, password }), { headers: jsonHeaders });
  check(res, { 'registration created (201)': (r) => r.status === 201 });
  if (res.status !== 201) {
    throw new Error(`registration of ${email} failed with status ${res.status}: ${res.body}`);
  }
  const data = JSON.parse(res.body).data;
  return { userId: data.user_id, token: data.access_token, email };
}

export function createCustomer() {
  const user = registerUser();
  const body = {
    account_type: 'checking',
    name: 'Load Test Customer',
    email: user.email,
    document: nextCpf(),
  };
  const res = http.post(`${BASE_URL}/api/v1/accounts`, JSON.stringify(body), { headers: authHeaders(user.token) });
  check(res, { 'account creation accepted (202)': (r) => r.status === 202 });

  let accountId = null;
  for (let attempt = 0; attempt < 120; attempt += 1) {
    const listRes = http.get(`${BASE_URL}/api/v1/users/${user.userId}/accounts`, { headers: authHeaders(user.token) });
    if (listRes.status === 200) {
      const parsed = JSON.parse(listRes.body);
      const accounts = parsed.data || [];
      if (accounts.length > 0) {
        accountId = accounts[0].account_id;
        break;
      }
    }
    sleep(0.25);
  }
  if (!accountId) {
    throw new Error(`account for user ${user.userId} was not created within timeout`);
  }
  return { userId: user.userId, token: user.token, accountId };
}

export function waitForStatus(token, path, idempotencyKey, finalStatuses, maxAttempts, intervalSeconds) {
  const attempts = maxAttempts || 120;
  const interval = intervalSeconds || 0.25;
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    const res = http.get(`${BASE_URL}${path}`, { headers: authHeaders(token) });
    if (res.status === 200) {
      const parsed = JSON.parse(res.body);
      const items = parsed.data || [];
      const match = items.find((item) => item.idempotency_key === idempotencyKey);
      if (match && finalStatuses.indexOf(match.status) !== -1) {
        return match;
      }
    }
    sleep(interval);
  }
  return null;
}

export function fundAccount(customer, amount) {
  const { accountId, token } = customer;
  const idempotencyKey = uuid();
  const res = http.post(`${BASE_URL}/api/v1/transactions`, JSON.stringify({
    account_id: accountId,
    type: 'deposit',
    amount,
    currency: 'BRL',
    idempotency_key: idempotencyKey,
  }), { headers: authHeaders(token) });
  check(res, { 'funding deposit accepted (202)': (r) => r.status === 202 });
  const settled = waitForStatus(token, `/api/v1/accounts/${accountId}/transactions`, idempotencyKey, TRANSACTION_FINAL_STATUSES);
  if (!settled) {
    throw new Error(`funding deposit for account ${accountId} did not settle within timeout (idempotency_key=${idempotencyKey})`);
  }
  if (settled.status !== 'completed') {
    throw new Error(`funding deposit for account ${accountId} settled as ${settled.status}, expected completed (idempotency_key=${idempotencyKey})`);
  }
  if (settled.amount !== amount) {
    throw new Error(`funding deposit for account ${accountId} settled with amount ${JSON.stringify(settled.amount)}, expected ${JSON.stringify(amount)} (idempotency_key=${idempotencyKey})`);
  }
  return settled;
}

export function createFundedCustomer(amount) {
  const customer = createCustomer();
  fundAccount(customer, amount);
  return customer;
}

export function scenarios(name) {
  switch (name) {
    case 'smoke':
      return {
        smoke: {
          executor: 'constant-vus',
          vus: 1,
          duration: '30s',
        },
      };
    case 'load':
      return {
        load: {
          executor: 'constant-arrival-rate',
          rate: 50,
          timeUnit: '1s',
          duration: '2m',
          preAllocatedVUs: 50,
          maxVUs: 150,
        },
      };
    case 'stress':
      return {
        stress: {
          executor: 'ramping-arrival-rate',
          startRate: 10,
          timeUnit: '1s',
          stages: [
            { target: 200, duration: '3m' },
          ],
          preAllocatedVUs: 50,
          maxVUs: 300,
        },
      };
    case 'spike':
      return {
        spike: {
          executor: 'ramping-arrival-rate',
          startRate: 10,
          timeUnit: '1s',
          stages: [
            { target: 300, duration: '30s' },
            { target: 300, duration: '20s' },
            { target: 10, duration: '30s' },
          ],
          preAllocatedVUs: 50,
          maxVUs: 400,
        },
      };
    case 'soak':
      return {
        soak: {
          executor: 'constant-arrival-rate',
          rate: 30,
          timeUnit: '1s',
          duration: '10m',
          preAllocatedVUs: 30,
          maxVUs: 100,
        },
      };
    default:
      throw new Error('unknown scenario: ' + name);
  }
}

export const thresholds = {
  http_req_failed: ['rate<0.01'],
  http_req_duration: ['p(95)<300'],
};

export const summaryTrendStats = ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'];

import http from 'k6/http';
import { check } from 'k6';
import { BASE_URL, authHeaders, createFundedCustomer, scenarios, thresholds, summaryTrendStats } from './lib.js';

const CUSTOMER_COUNT = 20;
const FUND_AMOUNT = '1000000.00';

export const options = {
  scenarios: scenarios(__ENV.SCENARIO || 'smoke'),
  thresholds,
  setupTimeout: '180s',
  summaryTrendStats,
};

export function setup() {
  const customers = [];
  for (let i = 0; i < CUSTOMER_COUNT; i += 1) {
    customers.push(createFundedCustomer(FUND_AMOUNT));
  }
  return { customers };
}

export default function (data) {
  const customer = data.customers[Math.floor(Math.random() * data.customers.length)];
  const headers = authHeaders(customer.token);

  const accountRes = http.get(`${BASE_URL}/api/v1/accounts/${customer.accountId}`, { headers });
  check(accountRes, { 'account read (200)': (r) => r.status === 200 });

  const transactionsRes = http.get(`${BASE_URL}/api/v1/accounts/${customer.accountId}/transactions`, { headers });
  check(transactionsRes, { 'transactions read (200)': (r) => r.status === 200 });

  const paymentsRes = http.get(`${BASE_URL}/api/v1/accounts/${customer.accountId}/payments`, { headers });
  check(paymentsRes, { 'payments read (200)': (r) => r.status === 200 });
}

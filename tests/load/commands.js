import http from 'k6/http';
import { check } from 'k6';
import { BASE_URL, jsonHeaders, uuid, createFundedCustomer, scenarios, thresholds, summaryTrendStats } from './lib.js';

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

function randomCustomer(customers) {
  return customers[Math.floor(Math.random() * customers.length)];
}

function randomAmount(min, max) {
  const cents = Math.round((min + Math.random() * (max - min)) * 100);
  return (cents / 100).toFixed(2);
}

function postDeposit(customers) {
  const customer = randomCustomer(customers);
  return http.post(`${BASE_URL}/api/v1/transactions`, JSON.stringify({
    account_id: customer.accountId,
    type: 'deposit',
    amount: randomAmount(10, 500),
    currency: 'BRL',
    idempotency_key: uuid(),
  }), { headers: jsonHeaders });
}

function postTransfer(customers) {
  const from = randomCustomer(customers);
  let to = randomCustomer(customers);
  while (to.accountId === from.accountId) {
    to = randomCustomer(customers);
  }
  return http.post(`${BASE_URL}/api/v1/transfers`, JSON.stringify({
    from_account_id: from.accountId,
    to_account_id: to.accountId,
    amount: randomAmount(5, 100),
    currency: 'BRL',
    idempotency_key: uuid(),
  }), { headers: jsonHeaders });
}

function postPayment(customers) {
  const customer = randomCustomer(customers);
  return http.post(`${BASE_URL}/api/v1/payments`, JSON.stringify({
    account_id: customer.accountId,
    payment_method: 'pix',
    amount: randomAmount(5, 100),
    currency: 'BRL',
    recipient: 'Ana Souza',
    pix_key: 'ana@example.com',
    idempotency_key: uuid(),
  }), { headers: jsonHeaders });
}

export default function (data) {
  const roll = Math.floor(Math.random() * 3);
  let res;
  if (roll === 0) {
    res = postDeposit(data.customers);
  } else if (roll === 1) {
    res = postTransfer(data.customers);
  } else {
    res = postPayment(data.customers);
  }
  check(res, { 'command accepted (202)': (r) => r.status === 202 });
}

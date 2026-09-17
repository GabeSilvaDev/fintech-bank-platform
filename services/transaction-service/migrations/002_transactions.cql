CREATE TABLE IF NOT EXISTS {{keyspace}}.transactions (
  transaction_id uuid PRIMARY KEY,
  type text,
  status text,
  account_id uuid,
  counterparty_id uuid,
  amount_cents bigint,
  currency text,
  description text,
  idempotency_key text,
  failure_reason text,
  from_balance_cents bigint,
  to_balance_cents bigint,
  created_at timestamp,
  updated_at timestamp,
  completed_at timestamp
);

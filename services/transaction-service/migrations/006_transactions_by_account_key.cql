CREATE TABLE IF NOT EXISTS {{keyspace}}.transactions_by_account_key (
  account_id uuid,
  idempotency_key text,
  transaction_id uuid,
  PRIMARY KEY ((account_id), idempotency_key)
);

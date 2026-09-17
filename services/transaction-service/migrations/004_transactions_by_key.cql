CREATE TABLE IF NOT EXISTS {{keyspace}}.transactions_by_key (
  idempotency_key text PRIMARY KEY,
  transaction_id uuid
);

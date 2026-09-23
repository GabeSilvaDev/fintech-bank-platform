CREATE TABLE IF NOT EXISTS {{keyspace}}.payments_by_key (
  account_id uuid,
  idempotency_key text,
  payment_id uuid,
  PRIMARY KEY ((account_id, idempotency_key))
);

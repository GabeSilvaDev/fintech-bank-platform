CREATE TABLE IF NOT EXISTS {{keyspace}}.balance_operations (
  account_id uuid,
  idempotency_key text,
  kind text,
  status text,
  result text,
  created_at timestamp,
  updated_at timestamp,
  PRIMARY KEY ((account_id), idempotency_key)
) WITH default_time_to_live = 2592000;

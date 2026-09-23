CREATE TABLE IF NOT EXISTS {{keyspace}}.payments_by_account (
  account_id uuid,
  created_at timestamp,
  payment_id uuid,
  PRIMARY KEY ((account_id), created_at, payment_id)
) WITH CLUSTERING ORDER BY (created_at DESC, payment_id ASC);

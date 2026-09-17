CREATE TABLE IF NOT EXISTS {{keyspace}}.transactions_by_account (
  account_id uuid,
  created_at timestamp,
  transaction_id uuid,
  PRIMARY KEY ((account_id), created_at, transaction_id)
) WITH CLUSTERING ORDER BY (created_at DESC, transaction_id ASC);

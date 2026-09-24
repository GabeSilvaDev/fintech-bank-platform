CREATE TABLE IF NOT EXISTS {{keyspace}}.open_transactions (
  bucket text,
  transaction_id uuid,
  PRIMARY KEY ((bucket), transaction_id)
);

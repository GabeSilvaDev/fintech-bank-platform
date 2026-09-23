CREATE TABLE IF NOT EXISTS {{keyspace}}.payments_by_external_id (
  external_id text PRIMARY KEY,
  payment_id uuid
);

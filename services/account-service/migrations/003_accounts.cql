CREATE TABLE IF NOT EXISTS {{keyspace}}.accounts (
  account_id uuid PRIMARY KEY,
  user_id uuid,
  agency text,
  number text,
  type text,
  status text,
  currency text,
  balance_cents bigint,
  created_at timestamp,
  updated_at timestamp,
  closed_at timestamp
);

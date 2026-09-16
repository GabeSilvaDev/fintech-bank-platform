CREATE TABLE IF NOT EXISTS {{keyspace}}.customers (
  user_id uuid PRIMARY KEY,
  name text,
  email text,
  document text,
  phone text,
  kyc_status text,
  created_at timestamp,
  updated_at timestamp
);

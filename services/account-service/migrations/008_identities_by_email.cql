CREATE TABLE IF NOT EXISTS {{keyspace}}.identities_by_email (
  email text PRIMARY KEY,
  user_id uuid,
  password_hash text,
  created_at timestamp
);

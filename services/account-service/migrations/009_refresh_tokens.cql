CREATE TABLE IF NOT EXISTS {{keyspace}}.refresh_tokens (
  token_hash text PRIMARY KEY,
  user_id uuid,
  family_id uuid,
  status text,
  expires_at timestamp,
  created_at timestamp
) WITH default_time_to_live = 2592000;

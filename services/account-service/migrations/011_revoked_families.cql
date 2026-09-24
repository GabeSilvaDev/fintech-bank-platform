CREATE TABLE IF NOT EXISTS {{keyspace}}.revoked_families (
  family_id uuid PRIMARY KEY,
  revoked_at timestamp
) WITH default_time_to_live = 2592000;

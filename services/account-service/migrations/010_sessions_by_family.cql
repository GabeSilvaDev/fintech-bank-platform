CREATE TABLE IF NOT EXISTS {{keyspace}}.sessions_by_family (
  family_id uuid,
  token_hash text,
  PRIMARY KEY ((family_id), token_hash)
) WITH default_time_to_live = 2592000;

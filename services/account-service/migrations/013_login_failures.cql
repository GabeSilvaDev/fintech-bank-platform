CREATE TABLE IF NOT EXISTS {{keyspace}}.login_failures (
  email text PRIMARY KEY,
  failures int,
  first_failure timestamp
);

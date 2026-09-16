CREATE TABLE IF NOT EXISTS {{keyspace}}.accounts_by_user (
  user_id uuid,
  account_id uuid,
  PRIMARY KEY (user_id, account_id)
);

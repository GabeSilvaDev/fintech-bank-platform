CREATE TABLE IF NOT EXISTS {{keyspace}}.accounts_by_number (
  agency text,
  number text,
  account_id uuid,
  PRIMARY KEY ((agency, number))
);

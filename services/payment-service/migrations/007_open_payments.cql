CREATE TABLE IF NOT EXISTS {{keyspace}}.open_payments (
  bucket text,
  payment_id uuid,
  PRIMARY KEY ((bucket), payment_id)
);

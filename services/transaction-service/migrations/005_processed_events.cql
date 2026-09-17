CREATE TABLE IF NOT EXISTS {{keyspace}}.processed_events (
  event_id uuid PRIMARY KEY,
  processed_at timestamp
) WITH default_time_to_live = 604800;

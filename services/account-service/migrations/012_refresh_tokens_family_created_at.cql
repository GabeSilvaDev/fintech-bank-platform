ALTER TABLE {{keyspace}}.refresh_tokens ADD IF NOT EXISTS family_created_at timestamp;

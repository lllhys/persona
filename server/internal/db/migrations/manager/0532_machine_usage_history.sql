-- Token usage history (docs/server/machines.md「Token usage」): past a week the ten-minute rows roll up into one row an
-- hour (step 3600), kept 53 weeks for the calendar of tokens a day; the calendar sums every machine's rows by time.
CREATE INDEX IF NOT EXISTS machine_usage_ts ON machine_usage (ts);

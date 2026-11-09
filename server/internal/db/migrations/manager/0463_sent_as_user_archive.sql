-- Read one connector's sends around an archive day without scanning the retained ledger.
CREATE INDEX IF NOT EXISTS sent_as_user_connector_at ON sent_as_user (connector, at);

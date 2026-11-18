-- A phone's PushKit (VoIP) registration for urgent calls (docs/assistant/urgent-call.md): its own device token, the
-- relay grant for that token (topic "<bundle id>.voip", docs/operations/push-relay.md "VoIP pushes"), and why the phone
-- got none, in its own words. All empty on a phone that cannot take calls.
ALTER TABLE devices ADD COLUMN IF NOT EXISTS voip_token text NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN IF NOT EXISTS voip_grant text NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN IF NOT EXISTS voip_error text NOT NULL DEFAULT '';

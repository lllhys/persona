-- Source subscriptions (#2038, docs/extensions/subscriptions.md "Storage"): what the manager pulls from connectors'
-- read-only tools by a recipe.
--
-- subscriptions: one row a subscription. data is the subscription as the manager keeps it (its recipe, switches,
-- interval, rules, run state and the last 20 runs); json like inbox_items, read whole in Go. The columns beside it are
-- what is looked up or locked by: the connector and source, revision (a change is written only at the revision the
-- app saw), and lease_until (a run holds it for 5 minutes, so a run that died is made again after that).
CREATE TABLE IF NOT EXISTS subscriptions (
    id          text PRIMARY KEY,
    connector   text NOT NULL,
    source      text NOT NULL,
    revision    bigint NOT NULL,
    data        json NOT NULL,
    lease_until timestamptz,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS subscriptions_connector ON subscriptions (connector);

-- subscription_seen: the items a subscription has pulled (uid: the item's id, or a hash of its title, time and
-- sender), so a run that reads them again keeps nothing twice. The newest 5000 are kept a subscription, none older
-- than 400 days.
CREATE TABLE IF NOT EXISTS subscription_seen (
    subscription_id text NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    uid             text NOT NULL,
    content_hash    text NOT NULL,
    first_seen_at   timestamptz NOT NULL,
    PRIMARY KEY (subscription_id, uid)
);
CREATE INDEX IF NOT EXISTS subscription_seen_age ON subscription_seen (subscription_id, first_seen_at DESC);

-- subscription_items: new items waiting for the Inbox and the knowledge base (#2039), in seq order. data holds the
-- mapped fields, the body cut to 8000 characters. Written in the same transaction as their subscription_seen rows
-- and the subscription's cursor.
CREATE TABLE IF NOT EXISTS subscription_items (
    seq             bigserial PRIMARY KEY,
    subscription_id text NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    uid             text NOT NULL,
    data            json NOT NULL,
    created_at      timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS subscription_items_of ON subscription_items (subscription_id, seq);

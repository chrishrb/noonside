-- Integrations and signals: per-system webhooks, day-ahead prices, PVOutput
-- upload state and reported power limits (curtailment).

-- Webhooks of a system: a URL that gets signed JSON for the chosen events.
-- The signing secret is sealed with SECRET_KEY; texts are rendered in
-- locale.
CREATE TABLE webhooks (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    system_id   uuid NOT NULL REFERENCES systems (id) ON DELETE CASCADE,
    name        text NOT NULL,
    url         text NOT NULL,
    secret_enc  text NOT NULL DEFAULT '',
    events      text[] NOT NULL
        CHECK (cardinality(events) > 0 AND events <@ '{alert.opened,alert.resolved,daily.summary,surplus.forecast}'),
    locale      text NOT NULL DEFAULT 'en',
    enabled     boolean NOT NULL DEFAULT true,
    created_by  uuid REFERENCES profiles (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX webhooks_system_idx ON webhooks (system_id);
ALTER TABLE webhooks ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON webhooks FROM anon, authenticated;

-- Outbox for webhooks, retried with the same backoff as notification
-- deliveries. dedupe_key makes queuing idempotent (an alert id and state, a
-- date); event_id is sent to receivers so they can drop repeats.
CREATE TABLE webhook_deliveries (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id         uuid NOT NULL DEFAULT gen_random_uuid(),
    webhook_id       uuid NOT NULL REFERENCES webhooks (id) ON DELETE CASCADE,
    event            text NOT NULL,
    dedupe_key       text NOT NULL,
    payload          jsonb NOT NULL DEFAULT '{}',
    status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts         smallint NOT NULL DEFAULT 0,
    next_attempt_at  timestamptz NOT NULL DEFAULT now(),
    last_error       text NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now(),
    sent_at          timestamptz,
    UNIQUE (webhook_id, event, dedupe_key)
);
CREATE INDEX webhook_deliveries_pending_idx ON webhook_deliveries (next_attempt_at) WHERE status = 'pending';
CREATE INDEX webhook_deliveries_webhook_idx ON webhook_deliveries (webhook_id, created_at DESC);
ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON webhook_deliveries FROM anon, authenticated;

-- Day-ahead prices per bidding zone (hourly before October 2025, then 15
-- minutes); fetched by the market_prices job.
CREATE TABLE market_prices (
    zone       text NOT NULL,
    starts_at  timestamptz NOT NULL,
    ends_at    timestamptz NOT NULL,
    eur_mwh    real NOT NULL,
    PRIMARY KEY (zone, starts_at)
);
ALTER TABLE market_prices ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON market_prices FROM anon, authenticated;

-- price_zone selects the day-ahead prices; unpaid_at_negative_prices drops
-- the feed-in revenue while prices are negative (§ 51 EEG, systems from
-- 25 Feb 2025 under the Solarspitzengesetz).
ALTER TABLE systems
    ADD COLUMN price_zone text CHECK (price_zone IN ('DE-LU', 'AT')),
    ADD COLUMN unpaid_at_negative_prices boolean NOT NULL DEFAULT false;

-- Progress of pushing sources (pvoutput_export: what was uploaded).
ALTER TABLE data_sources ADD COLUMN state jsonb NOT NULL DEFAULT '{}';

-- Active power limit a device reported (e.g. § 14a EnWG or feed-in
-- management); part of the event, so a change starts a new one.
ALTER TABLE inverter_events ADD COLUMN power_limit_pct smallint CHECK (power_limit_pct BETWEEN 0 AND 100);
ALTER TABLE inverter_state ADD COLUMN power_limit_pct smallint CHECK (power_limit_pct BETWEEN 0 AND 100);

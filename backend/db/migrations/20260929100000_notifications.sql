-- Alerts (incidents opened and resolved by the alert engine) and per-user
-- notifications delivered in-app, by email and by web push.

-- Language for server-rendered text (emails, push). The SPA sets it.
ALTER TABLE profiles ADD COLUMN locale text NOT NULL DEFAULT 'en';

-- Overrides of the built-in rule defaults (internal/alerts); systems without
-- a row use the defaults.
CREATE TABLE alert_rules (
    system_id   uuid NOT NULL REFERENCES systems (id) ON DELETE CASCADE,
    kind        text NOT NULL,
    enabled     boolean NOT NULL DEFAULT true,
    params      jsonb NOT NULL DEFAULT '{}',
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (system_id, kind)
);
ALTER TABLE alert_rules ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON alert_rules FROM anon, authenticated;

-- subject_key identifies what the alert is about within the system
-- ("" for the system, else an inverter or data source id). At most one open
-- alert per (system, kind, subject).
CREATE TABLE alerts (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    system_id       uuid NOT NULL REFERENCES systems (id) ON DELETE CASCADE,
    kind            text NOT NULL,
    subject_key     text NOT NULL DEFAULT '',
    inverter_id     uuid REFERENCES inverters (id) ON DELETE CASCADE,
    data_source_id  uuid REFERENCES data_sources (id) ON DELETE CASCADE,
    severity        text NOT NULL CHECK (severity IN ('info', 'warning', 'error')),
    details         jsonb NOT NULL DEFAULT '{}',
    opened_at       timestamptz NOT NULL DEFAULT now(),
    last_seen_at    timestamptz NOT NULL DEFAULT now(),
    -- evaluations in a row without the condition; resolves at 2
    missed          smallint NOT NULL DEFAULT 0,
    resolved_at     timestamptz,
    acked_by        uuid REFERENCES profiles (id) ON DELETE SET NULL,
    acked_at        timestamptz,
    snoozed_until   timestamptz,
    reminded_at     timestamptz
);
CREATE UNIQUE INDEX alerts_open_idx ON alerts (system_id, kind, subject_key) WHERE resolved_at IS NULL;
CREATE INDEX alerts_system_idx ON alerts (system_id, opened_at DESC);
ALTER TABLE alerts ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON alerts FROM anon, authenticated;

CREATE TABLE notifications (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    system_id   uuid REFERENCES systems (id) ON DELETE CASCADE,
    alert_id    uuid REFERENCES alerts (id) ON DELETE CASCADE,
    type        text NOT NULL,
    severity    text NOT NULL CHECK (severity IN ('info', 'warning', 'error')),
    payload     jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now(),
    read_at     timestamptz
);
CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC, id DESC);
CREATE INDEX notifications_unread_idx ON notifications (user_id) WHERE read_at IS NULL;
ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON notifications FROM anon, authenticated;

-- Outbox for email and push; the in-app channel is the notification itself.
CREATE TABLE notification_deliveries (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    notification_id  uuid NOT NULL REFERENCES notifications (id) ON DELETE CASCADE,
    channel          text NOT NULL CHECK (channel IN ('email', 'push')),
    status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed', 'skipped')),
    attempts         smallint NOT NULL DEFAULT 0,
    next_attempt_at  timestamptz NOT NULL DEFAULT now(),
    last_error       text NOT NULL DEFAULT '',
    sent_at          timestamptz,
    UNIQUE (notification_id, channel)
);
CREATE INDEX notification_deliveries_pending_idx ON notification_deliveries (next_attempt_at) WHERE status = 'pending';
ALTER TABLE notification_deliveries ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON notification_deliveries FROM anon, authenticated;

-- system_id NULL is the user's default; a row per system overrides it.
CREATE TABLE notification_prefs (
    user_id          uuid NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    system_id        uuid REFERENCES systems (id) ON DELETE CASCADE,
    in_app           boolean NOT NULL DEFAULT true,
    email            boolean NOT NULL DEFAULT true,
    push             boolean NOT NULL DEFAULT true,
    min_severity     text NOT NULL DEFAULT 'warning' CHECK (min_severity IN ('info', 'warning', 'error')),
    notify_resolved  boolean NOT NULL DEFAULT true,
    digest           text NOT NULL DEFAULT 'none' CHECK (digest IN ('none', 'daily', 'weekly', 'monthly')),
    muted_until      timestamptz,
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (user_id, system_id)
);
ALTER TABLE notification_prefs ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON notification_prefs FROM anon, authenticated;

CREATE TABLE push_subscriptions (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          uuid NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    endpoint         text NOT NULL UNIQUE,
    p256dh           text NOT NULL,
    auth             text NOT NULL,
    user_agent       text NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now(),
    last_success_at  timestamptz,
    failures         smallint NOT NULL DEFAULT 0
);
CREATE INDEX push_subscriptions_user_idx ON push_subscriptions (user_id);
ALTER TABLE push_subscriptions ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON push_subscriptions FROM anon, authenticated;

-- Leases for singleton scheduler jobs (like data_sources.locked_until; safe
-- with the transaction pooler).
CREATE TABLE job_leases (
    name          text PRIMARY KEY,
    locked_until  timestamptz,
    last_run_at   timestamptz
);
ALTER TABLE job_leases ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON job_leases FROM anon, authenticated;

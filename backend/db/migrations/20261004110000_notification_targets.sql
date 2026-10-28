-- ntfy and webhook as notification channels. Targets are per user; the
-- prefs switch them on or off per system like email and push.
ALTER TABLE notification_deliveries DROP CONSTRAINT notification_deliveries_channel_check;
ALTER TABLE notification_deliveries ADD CONSTRAINT notification_deliveries_channel_check
    CHECK (channel IN ('email', 'push', 'ntfy', 'webhook'));

ALTER TABLE notification_prefs
    ADD COLUMN ntfy boolean NOT NULL DEFAULT true,
    ADD COLUMN webhook boolean NOT NULL DEFAULT true;

-- The ntfy access token and webhook signing secret are sealed with SECRET_KEY.
CREATE TABLE notification_targets (
    user_id             uuid PRIMARY KEY REFERENCES profiles (id) ON DELETE CASCADE,
    ntfy_url            text NOT NULL DEFAULT '',
    ntfy_token_enc      text NOT NULL DEFAULT '',
    webhook_url         text NOT NULL DEFAULT '',
    webhook_secret_enc  text NOT NULL DEFAULT '',
    updated_at          timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE notification_targets ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON notification_targets FROM anon, authenticated;

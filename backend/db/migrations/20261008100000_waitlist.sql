-- Waitlist of the landing page (docs/plans/validierung.md): sign-ups with
-- double opt-in, and cookieless counts of the pricing page's "fake door"
-- buttons. No IP addresses or other tracking data are stored.

-- One row per email address. confirm_token_hash is the SHA-256 of the token
-- in the confirmation email; unsubscribe_token only allows removing the row,
-- so it is stored as is and can go into every later email. Unconfirmed rows
-- are deleted after 7 days (alerts.Engine.Prune).
CREATE TABLE waitlist_entries (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email              text NOT NULL UNIQUE CHECK (email = lower(email)),
    role               text NOT NULL DEFAULT '',
    plan               text NOT NULL DEFAULT '',
    consent_version    text NOT NULL,
    confirm_token_hash bytea,
    confirm_sent_at    timestamptz,
    confirmed_at       timestamptz,
    unsubscribe_token  text NOT NULL UNIQUE,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX waitlist_entries_confirm_idx ON waitlist_entries (confirm_token_hash);
CREATE INDEX waitlist_entries_sent_idx ON waitlist_entries (confirm_sent_at);
ALTER TABLE waitlist_entries ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON waitlist_entries FROM anon, authenticated;

-- Clicks on the pricing buttons per plan and UTC day.
CREATE TABLE pricing_clicks (
    plan  text NOT NULL,
    day   date NOT NULL,
    count integer NOT NULL DEFAULT 0,
    PRIMARY KEY (plan, day)
);
ALTER TABLE pricing_clicks ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON pricing_clicks FROM anon, authenticated;

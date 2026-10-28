-- Read-only public links to a system's yield (and kiosk mode). Only the
-- token's hash is stored, like invitations and API keys.
CREATE TABLE share_links (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    system_id        uuid NOT NULL REFERENCES systems (id) ON DELETE CASCADE,
    label            text NOT NULL,
    prefix           text NOT NULL,
    token_hash       bytea NOT NULL UNIQUE,
    show_financials  boolean NOT NULL DEFAULT false,
    expires_at       timestamptz,
    created_by       uuid REFERENCES profiles (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    revoked_at       timestamptz
);
CREATE INDEX share_links_system_idx ON share_links (system_id);
ALTER TABLE share_links ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON share_links FROM anon, authenticated;

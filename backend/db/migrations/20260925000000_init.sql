-- solar0 schema. The Go API connects as `postgres` (bypasses RLS); nothing
-- here is meant to be reachable through PostgREST — see the lockdown at the end.

CREATE EXTENSION IF NOT EXISTS citext WITH SCHEMA extensions;

-- identity ------------------------------------------------------------------

-- One row per Auth (GoTrue) user, kept in sync by trigger.
CREATE TABLE profiles (
    id          uuid PRIMARY KEY REFERENCES auth.users (id) ON DELETE CASCADE,
    email       extensions.citext NOT NULL UNIQUE,
    name        text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE FUNCTION public.sync_profile() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = '' AS $$
BEGIN
    INSERT INTO public.profiles (id, email, name)
    VALUES (NEW.id, NEW.email, COALESCE(NEW.raw_user_meta_data ->> 'name', ''))
    ON CONFLICT (id) DO UPDATE SET
        email = EXCLUDED.email,
        name = COALESCE(NULLIF(NEW.raw_user_meta_data ->> 'name', ''), public.profiles.name),
        updated_at = now();
    RETURN NEW;
END;
$$;

CREATE TRIGGER sync_profile
    AFTER INSERT OR UPDATE OF email, raw_user_meta_data ON auth.users
    FOR EACH ROW WHEN (NEW.email IS NOT NULL)
    EXECUTE FUNCTION public.sync_profile();

-- plant metadata ------------------------------------------------------------

CREATE TABLE systems (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                text NOT NULL,
    location            text NOT NULL DEFAULT '',
    latitude            double precision,
    longitude           double precision,
    timezone            text NOT NULL DEFAULT 'Europe/Berlin',
    peak_power_kwp      double precision NOT NULL DEFAULT 0,
    commissioned_at     date,
    orientation         text NOT NULL DEFAULT '',
    module_info         text NOT NULL DEFAULT '',
    operator            text NOT NULL DEFAULT '',
    tariff_per_kwh      double precision NOT NULL DEFAULT 0,
    currency            text NOT NULL DEFAULT 'EUR',
    co2_kg_per_kwh      double precision NOT NULL DEFAULT 0.7,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

-- expected yield per calendar month ("Soll")
CREATE TABLE system_targets (
    system_id   uuid NOT NULL REFERENCES systems (id) ON DELETE CASCADE,
    month       smallint NOT NULL CHECK (month BETWEEN 1 AND 12),
    target_kwh  double precision NOT NULL CHECK (target_kwh >= 0),
    PRIMARY KEY (system_id, month)
);

CREATE TABLE system_members (
    system_id   uuid NOT NULL REFERENCES systems (id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES profiles (id) ON DELETE CASCADE,
    role        text NOT NULL CHECK (role IN ('owner', 'admin', 'viewer')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (system_id, user_id)
);
CREATE INDEX system_members_user_idx ON system_members (user_id);

CREATE TABLE invitations (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    system_id    uuid NOT NULL REFERENCES systems (id) ON DELETE CASCADE,
    email        extensions.citext NOT NULL,
    role         text NOT NULL CHECK (role IN ('owner', 'admin', 'viewer')),
    token_hash   bytea NOT NULL UNIQUE,
    invited_by   uuid REFERENCES profiles (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    accepted_at  timestamptz,
    revoked_at   timestamptz
);
CREATE INDEX invitations_system_idx ON invitations (system_id);

CREATE TABLE api_keys (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    system_id     uuid NOT NULL REFERENCES systems (id) ON DELETE CASCADE,
    name          text NOT NULL,
    prefix        text NOT NULL,
    key_hash      bytea NOT NULL UNIQUE,
    scopes        text[] NOT NULL,
    created_by    uuid REFERENCES profiles (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_used_at  timestamptz,
    revoked_at    timestamptz
);
CREATE INDEX api_keys_system_idx ON api_keys (system_id);

CREATE TABLE inverters (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    system_id       uuid NOT NULL REFERENCES systems (id) ON DELETE CASCADE,
    external_ref    text NOT NULL,
    name            text NOT NULL,
    model           text NOT NULL DEFAULT '',
    serial_number   text NOT NULL DEFAULT '',
    peak_power_kwp  double precision NOT NULL DEFAULT 0,
    color           text NOT NULL DEFAULT '',
    sort_order      integer NOT NULL DEFAULT 0,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (system_id, external_ref)
);

CREATE TABLE data_sources (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    system_id        uuid NOT NULL REFERENCES systems (id) ON DELETE CASCADE,
    kind             text NOT NULL,
    name             text NOT NULL,
    config           jsonb NOT NULL DEFAULT '{}',
    enabled          boolean NOT NULL DEFAULT true,
    poll_interval_s  integer NOT NULL DEFAULT 300 CHECK (poll_interval_s >= 30),
    last_run_at      timestamptz,
    last_success_at  timestamptz,
    last_error       text NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX data_sources_system_idx ON data_sources (system_id);

-- time series ---------------------------------------------------------------

CREATE TABLE measurements (
    time           timestamptz NOT NULL,
    inverter_id    uuid NOT NULL REFERENCES inverters (id) ON DELETE CASCADE,
    pac_w          double precision,
    pdc_w          double precision,
    udc_v          double precision,
    eday_wh        double precision,
    temperature_c  double precision,
    PRIMARY KEY (inverter_id, time)
);
CREATE INDEX measurements_time_idx ON measurements (time);


-- Daily energy per inverter in the system's local calendar. This is the source
-- of truth for month/year/total views; it is fed from minute data (max eday)
-- and from daily history imports, so it also covers periods without minute data.
CREATE TABLE inverter_daily (
    inverter_id  uuid NOT NULL REFERENCES inverters (id) ON DELETE CASCADE,
    day          date NOT NULL,
    energy_wh    double precision NOT NULL DEFAULT 0,
    pac_max_w    double precision,
    PRIMARY KEY (inverter_id, day)
);
CREATE INDEX inverter_daily_day_idx ON inverter_daily (day);

-- latest known state per inverter (for the live view)
CREATE TABLE inverter_state (
    inverter_id   uuid PRIMARY KEY REFERENCES inverters (id) ON DELETE CASCADE,
    time          timestamptz NOT NULL,
    pac_w         double precision,
    pdc_w         double precision,
    status_code   integer,
    status_text   text NOT NULL DEFAULT '',
    error_code    integer,
    error_text    text NOT NULL DEFAULT ''
);

CREATE TABLE inverter_events (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    inverter_id  uuid NOT NULL REFERENCES inverters (id) ON DELETE CASCADE,
    started_at   timestamptz NOT NULL,
    ended_at     timestamptz,
    status_code  integer,
    status_text  text NOT NULL DEFAULT '',
    error_code   integer,
    error_text   text NOT NULL DEFAULT '',
    UNIQUE (inverter_id, started_at)
);
CREATE INDEX inverter_events_started_idx ON inverter_events (started_at DESC);

-- lockdown -------------------------------------------------------------------
-- The former hosting exposed the public schema to the anon/authenticated
-- roles via PostgREST. Every table gets RLS without policies and no grants for them.
-- New tables in later migrations must repeat this (see CLAUDE.md).

DO $$
DECLARE t record;
BEGIN
    FOR t IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', t.tablename);
    END LOOP;
END;
$$;

REVOKE ALL ON ALL TABLES IN SCHEMA public FROM anon, authenticated;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM anon, authenticated;
REVOKE ALL ON FUNCTION public.sync_profile() FROM PUBLIC, anon, authenticated;

-- Tables and functions created later by postgres are not exposed either.
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public REVOKE ALL ON TABLES FROM anon, authenticated;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public REVOKE ALL ON SEQUENCES FROM anon, authenticated;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public REVOKE ALL ON FUNCTIONS FROM PUBLIC, anon, authenticated;

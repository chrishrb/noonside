-- Database bootstrap: the roles, schemas and extensions that the Auth server
-- (GoTrue) and backend/db/migrations expect (GoTrue's own role names). Run
-- once by a superuser in the app database, before GoTrue starts and before
-- `noonside migrate`:
--   - in the cluster by the chart's CNPG Cluster (postInitApplicationSQLRefs),
--   - locally by compose.yaml, which mounts this file into
--     docker-entrypoint-initdb.d.
-- Passwords are set elsewhere (compose, CNPG managed roles). Idempotent.

DO $$
BEGIN
    -- The migrations revoke from these; nothing logs in as them.
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'anon') THEN
        CREATE ROLE anon NOLOGIN NOINHERIT;
    END IF;
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'authenticated') THEN
        CREATE ROLE authenticated NOLOGIN NOINHERIT;
    END IF;
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'service_role') THEN
        CREATE ROLE service_role NOLOGIN NOINHERIT BYPASSRLS;
    END IF;
    -- GoTrue connects as this role and owns the auth schema.
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'supabase_auth_admin') THEN
        CREATE ROLE supabase_auth_admin LOGIN NOINHERIT CREATEROLE;
    END IF;
END
$$;

CREATE SCHEMA IF NOT EXISTS extensions;
CREATE EXTENSION IF NOT EXISTS citext WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA extensions;
GRANT USAGE ON SCHEMA extensions TO PUBLIC;

CREATE SCHEMA IF NOT EXISTS auth AUTHORIZATION supabase_auth_admin;
ALTER ROLE supabase_auth_admin SET search_path = auth;
DO $$
BEGIN
    EXECUTE format('GRANT CREATE ON DATABASE %I TO supabase_auth_admin', current_database());
END
$$;

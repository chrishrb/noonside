-- Local development data. `mise run db:reset` runs it on a fresh database
-- (after GoTrue's and our migrations); `mise run dev:seed` applies it to a
-- running database. It is never applied in production.
--
-- Accounts (password noonside-dev-pw for all; the login page lists them in dev):
--   dev@noonside.local   superadmin, sees every system (also test leftovers)
--   demo@noonside.local  owner of "Demo PV" and "Demo Balcony" only; the e2e
--                      and layout checks sign in with it
--   demo-de@noonside.local  the same in German (layout checks in DE; server-
--                      rendered texts follow the account's language)
--   admin@noonside.local   admin of "Demo PV"
--   viewer@noonside.local  viewer of "Demo PV"
-- Public share link of Demo PV: /p/s0p_local-dev-demo-link
--
-- Safe to run again: fixed ids, and generated data is replaced. (Plain temp
-- tables: this runs locally over one session.)

SELECT setseed(0.31);

-- dev account ---------------------------------------------------------------
-- Created like the admin API does (the token columns must be '' not NULL,
-- or GoTrue fails to load the user). The sync_profile trigger adds the profile.
INSERT INTO auth.users (
    instance_id, id, aud, role, email, encrypted_password, email_confirmed_at,
    confirmation_token, recovery_token, email_change_token_new, email_change,
    email_change_token_current, reauthentication_token, phone_change, phone_change_token,
    raw_app_meta_data, raw_user_meta_data, created_at, updated_at
)
SELECT '00000000-0000-0000-0000-000000000000', u.id, 'authenticated', 'authenticated', u.email,
    extensions.crypt('noonside-dev-pw', extensions.gen_salt('bf')), now(),
    '', '', '', '', '', '', '', '',
    jsonb_build_object('provider', 'email', 'providers', jsonb_build_array('email'), 'solar0_superadmin', u.superadmin),
    jsonb_build_object('name', u.name, 'email_verified', true), now(), now()
FROM (VALUES
    ('d0000000-0000-4000-8000-000000000001'::uuid, 'dev@noonside.local', 'Dev', true),
    ('d0000000-0000-4000-8000-000000000002'::uuid, 'demo@noonside.local', 'Demo', false),
    ('d0000000-0000-4000-8000-000000000003'::uuid, 'demo-de@noonside.local', 'Demo DE', false),
    ('d0000000-0000-4000-8000-000000000004'::uuid, 'admin@noonside.local', 'Ada Admin', false),
    ('d0000000-0000-4000-8000-000000000005'::uuid, 'viewer@noonside.local', 'Vic Viewer', false)
) AS u(id, email, name, superadmin)
ON CONFLICT (id) DO NOTHING;

INSERT INTO auth.identities (provider_id, user_id, provider, identity_data, created_at, updated_at)
SELECT id::text, id, 'email',
    jsonb_build_object('sub', id::text, 'email', email, 'email_verified', true, 'phone_verified', false), now(), now()
FROM auth.users WHERE id IN ('d0000000-0000-4000-8000-000000000001', 'd0000000-0000-4000-8000-000000000002',
    'd0000000-0000-4000-8000-000000000003', 'd0000000-0000-4000-8000-000000000004', 'd0000000-0000-4000-8000-000000000005')
ON CONFLICT (provider_id, provider) DO NOTHING;

UPDATE public.profiles SET locale = CASE id WHEN 'd0000000-0000-4000-8000-000000000003' THEN 'de' ELSE 'en' END
WHERE id IN ('d0000000-0000-4000-8000-000000000002', 'd0000000-0000-4000-8000-000000000003',
    'd0000000-0000-4000-8000-000000000004', 'd0000000-0000-4000-8000-000000000005');

-- demo system ---------------------------------------------------------------
INSERT INTO public.systems (id, name, location, latitude, longitude, timezone, peak_power_kwp, commissioned_at,
    orientation, module_info, operator, tariff_per_kwh, currency, co2_kg_per_kwh) VALUES
    ('d0000000-0000-4000-8000-000000000010', 'Demo PV', 'Marienplatz 1, 80331 München', 48.1374, 11.5755,
        'Europe/Berlin', 28, (date_trunc('year', now()) - interval '3 years')::date + 95,
        'South and west roof', '70 × 400 Wp mono', 'Local dev', 0.082, 'EUR', 0.4),
    ('d0000000-0000-4000-8000-000000000011', 'Demo Balcony', 'Leopoldstraße 50, 80802 München', 48.1603, 11.5860,
        'Europe/Berlin', 0.8, (now() - interval '18 months')::date,
        'Balcony railing', '2 × 400 Wp', 'Local dev', 0.30, 'EUR', 0.4)
ON CONFLICT (id) DO NOTHING;

INSERT INTO public.system_members (system_id, user_id, role)
SELECT s, u, 'owner'
FROM unnest(ARRAY['d0000000-0000-4000-8000-000000000010', 'd0000000-0000-4000-8000-000000000011']::uuid[]) AS s,
    unnest(ARRAY['d0000000-0000-4000-8000-000000000001', 'd0000000-0000-4000-8000-000000000002',
        'd0000000-0000-4000-8000-000000000003']::uuid[]) AS u
ON CONFLICT DO NOTHING;

INSERT INTO public.system_members (system_id, user_id, role) VALUES
    ('d0000000-0000-4000-8000-000000000010', 'd0000000-0000-4000-8000-000000000004', 'admin'),
    ('d0000000-0000-4000-8000-000000000010', 'd0000000-0000-4000-8000-000000000005', 'viewer')
ON CONFLICT DO NOTHING;

INSERT INTO public.system_planes (id, system_id, name, kwp, tilt_deg, azimuth_deg, sort_order) VALUES
    ('d0000000-0000-4000-8000-000000000021', 'd0000000-0000-4000-8000-000000000010', 'South', 20, 30, 0, 0),
    ('d0000000-0000-4000-8000-000000000022', 'd0000000-0000-4000-8000-000000000010', 'West', 8, 25, 90, 1)
ON CONFLICT (id) DO NOTHING;

INSERT INTO public.inverters (id, system_id, external_ref, name, model, serial_number, peak_power_kwp,
    max_ac_power_w, plane_id, color, sort_order) VALUES
    ('d0000000-0000-4000-8000-000000000031', 'd0000000-0000-4000-8000-000000000010', '1', 'WR 1', 'SMA STP 10.0', 'DEV-0001', 10, 10000, 'd0000000-0000-4000-8000-000000000021', '#2f6feb', 0),
    ('d0000000-0000-4000-8000-000000000032', 'd0000000-0000-4000-8000-000000000010', '2', 'WR 2', 'SMA STP 10.0', 'DEV-0002', 10, 10000, 'd0000000-0000-4000-8000-000000000021', '#0e9f6e', 1),
    ('d0000000-0000-4000-8000-000000000033', 'd0000000-0000-4000-8000-000000000010', '3', 'WR 3', 'SMA STP 8.0', 'DEV-0003', 8, 8000, 'd0000000-0000-4000-8000-000000000022', '#d97706', 2),
    ('d0000000-0000-4000-8000-000000000034', 'd0000000-0000-4000-8000-000000000011', '1', 'Micro inverter', 'Hoymiles HMS-800', 'DEV-0004', 0.8, 800, NULL, '#2f6feb', 0)
ON CONFLICT (id) DO NOTHING;

-- Monthly targets (kWh), roughly PVGIS for Munich and 28 kWp.
INSERT INTO public.system_targets (system_id, month, target_kwh)
SELECT 'd0000000-0000-4000-8000-000000000010', m, t
FROM unnest(ARRAY[1,2,3,4,5,6,7,8,9,10,11,12], ARRAY[750,1300,2400,3200,3700,3800,3900,3500,2700,1700,850,600]) AS x(m, t)
ON CONFLICT DO NOTHING;

-- Per inverter: kWp and a yield factor. WR 3 (west) underperforms in the
-- last days, so the underperformance alert stays open.
DROP TABLE IF EXISTS seed_inv;
CREATE TEMP TABLE seed_inv AS
SELECT v.*, s.commissioned_at FROM (VALUES
    ('d0000000-0000-4000-8000-000000000031'::uuid, 'd0000000-0000-4000-8000-000000000010'::uuid, 10.0, 1.00),
    ('d0000000-0000-4000-8000-000000000032'::uuid, 'd0000000-0000-4000-8000-000000000010'::uuid, 10.0, 0.97),
    ('d0000000-0000-4000-8000-000000000033'::uuid, 'd0000000-0000-4000-8000-000000000010'::uuid, 8.0, 0.88),
    ('d0000000-0000-4000-8000-000000000034'::uuid, 'd0000000-0000-4000-8000-000000000011'::uuid, 0.8, 0.80)
) AS v(id, system_id, kwp, factor) JOIN public.systems s ON s.id = v.system_id;

-- daily energy --------------------------------------------------------------
-- History from commissioning (over three years ago) until three days ago (local dates): a seasonal
-- curve times the day's weather, a few days without any production, and a
-- weak spell of WR 3 last spring. The last three days follow from the
-- minute data below.
DROP TABLE IF EXISTS seed_days;
CREATE TEMP TABLE seed_days AS
SELECT d::date AS day,
    CASE WHEN random() < 0.006 THEN 0 ELSE 0.35 + 0.65 * sqrt(random()) END AS weather,
    3.3 - 2.8 * cos(2 * pi() * (extract(doy FROM d) + 10) / 365.25) AS kwh_per_kwp
FROM generate_series((SELECT min(commissioned_at) FROM seed_inv),
    (now() AT TIME ZONE 'Europe/Berlin')::date - 3, interval '1 day') AS d;

INSERT INTO public.inverter_daily (inverter_id, day, energy_wh, pac_max_w)
SELECT i.id, d.day,
    round((d.kwh_per_kwp * d.weather * i.kwp * i.factor * 1000
        * CASE WHEN i.id = 'd0000000-0000-4000-8000-000000000033'
            AND d.day BETWEEN date_trunc('year', now())::date + 90 AND date_trunc('year', now())::date + 130 THEN 0.6 ELSE 1 END)::numeric),
    round((i.kwp * 1000 * (0.35 + 0.45 * d.weather) * (0.6 + 0.4 * d.kwh_per_kwp / 6.1))::numeric)
FROM seed_days d JOIN seed_inv i ON d.day >= i.commissioned_at
ON CONFLICT (inverter_id, day) DO UPDATE SET energy_wh = EXCLUDED.energy_wh, pac_max_w = EXCLUDED.pac_max_w;

-- minute data ---------------------------------------------------------------
-- The last three UTC days (today until now): a bell-shaped curve around
-- solar noon (11:15 UTC) with passing clouds. measurement_days holds one
-- row per inverter and UTC day with the samples as parallel arrays.
DROP TABLE IF EXISTS seed_samples;
CREATE TEMP TABLE seed_samples AS
WITH days AS (
    SELECT d::date AS day, 3.3 - 2.8 * cos(2 * pi() * (extract(doy FROM d) + 10) / 365.25) AS season
    FROM generate_series(current_date - 2, current_date, interval '1 day') AS d
), minutes AS (
    SELECT day, season, s, day::timestamp AT TIME ZONE 'UTC' + s * interval '1 second' AS t,
        -- half the day length in seconds: about 4 h in winter, 8 h in summer
        (4 + 4 * (season - 0.5) / 5.6) * 3600 AS half,
        0.75 + 0.25 * cos(s / 1800.0 + extract(doy FROM day)) * random() AS cloud
    FROM days CROSS JOIN generate_series(0, 86399, 60) AS s
)
SELECT i.id AS inverter_id, m.day, m.s, m.t,
    greatest(0, i.kwp * 1000 * 0.8 * (0.5 + 0.5 * m.season / 6.1) * i.factor * m.cloud
        * CASE WHEN i.id = 'd0000000-0000-4000-8000-000000000033' THEN 0.6 ELSE 1 END
        * power(greatest(0, cos(pi() / 2 * (m.s - 40500) / m.half)), 1.4)) AS pac
FROM minutes m CROSS JOIN seed_inv i
WHERE abs(m.s - 40500) < m.half AND m.t <= now();

INSERT INTO public.measurement_days (inverter_id, day, secs, pac_w, pdc_w, udc_v, eday_wh, temperature_c)
SELECT inverter_id, day, array_agg(s ORDER BY s), array_agg(round(pac) ORDER BY s),
    array_agg(round(pac * 1.035) ORDER BY s), array_agg(round(640 - 60 * pac / 8000 + 5 * random()) ORDER BY s),
    array_agg(round(e) ORDER BY s), array_agg(round((11 + 24 * pac / 9000)::numeric, 1) ORDER BY s)
FROM (SELECT *, sum(pac / 60) OVER (PARTITION BY inverter_id, day ORDER BY s) AS e FROM seed_samples) x
GROUP BY inverter_id, day
ON CONFLICT (inverter_id, day) DO UPDATE SET secs = EXCLUDED.secs, pac_w = EXCLUDED.pac_w, pdc_w = EXCLUDED.pdc_w,
    udc_v = EXCLUDED.udc_v, eday_wh = EXCLUDED.eday_wh, temperature_c = EXCLUDED.temperature_c;

-- Daily energy of those days from the minute data (production happens
-- during the day, so UTC and local dates agree).
INSERT INTO public.inverter_daily (inverter_id, day, energy_wh, pac_max_w)
SELECT inverter_id, day, round(sum(pac) / 60), round(max(pac)) FROM seed_samples GROUP BY inverter_id, day
ON CONFLICT (inverter_id, day) DO UPDATE SET energy_wh = EXCLUDED.energy_wh, pac_max_w = EXCLUDED.pac_max_w;

INSERT INTO public.inverter_state (inverter_id, time, pac_w, pdc_w, status_code, status_text)
SELECT i.id, now(), coalesce(l.pac, 0), coalesce(l.pac, 0) * 1.035,
    CASE WHEN l.pac > 0 THEN 8 ELSE 1 END, CASE WHEN l.pac > 0 THEN 'MPP' ELSE 'Waiting for sun' END
FROM seed_inv i
LEFT JOIN LATERAL (SELECT pac FROM seed_samples s WHERE s.inverter_id = i.id AND s.t > now() - interval '5 minutes'
    ORDER BY s.t DESC LIMIT 1) l ON true
ON CONFLICT (inverter_id) DO UPDATE SET time = EXCLUDED.time, pac_w = EXCLUDED.pac_w, pdc_w = EXCLUDED.pdc_w,
    status_code = EXCLUDED.status_code, status_text = EXCLUDED.status_text, error_code = NULL, error_text = '';

-- day-ahead prices ------------------------------------------------------------
-- Demo PV uses the DE-LU zone. Synthetic 15-minute prices for the minute-data
-- days and tomorrow (negative around midday, a peak in the evening), so the
-- price band and the cheapest window work offline; prices the scheduler
-- fetches from aWATTar replace them.
UPDATE public.systems SET price_zone = 'DE-LU' WHERE id = 'd0000000-0000-4000-8000-000000000010' AND price_zone IS NULL;
INSERT INTO public.market_prices (zone, starts_at, ends_at, eur_mwh)
SELECT 'DE-LU', t, t + interval '15 minutes',
    round((95 - 110 * exp(-power(h - 13, 2) / 8) + 40 * exp(-power(h - 19.5, 2) / 3))::numeric, 2)
FROM generate_series(current_date - 2, current_date + 1, interval '1 day') AS d
CROSS JOIN generate_series(0, 95) AS q
CROSS JOIN LATERAL (SELECT (d::date::timestamp AT TIME ZONE 'Europe/Berlin') + q * interval '15 minutes' AS t, q / 4.0 AS h) x
ON CONFLICT (zone, starts_at) DO NOTHING;

-- alert and share link --------------------------------------------------------
INSERT INTO public.alerts (system_id, kind, subject_key, inverter_id, severity, details, opened_at, last_seen_at)
SELECT 'd0000000-0000-4000-8000-000000000010', 'underperformance', 'd0000000-0000-4000-8000-000000000033',
    'd0000000-0000-4000-8000-000000000033', 'warning', '{"percent": 60}', now() - interval '1 day', now()
WHERE NOT EXISTS (SELECT 1 FROM public.alerts WHERE system_id = 'd0000000-0000-4000-8000-000000000010'
    AND kind = 'underperformance' AND resolved_at IS NULL);

-- Token s0p_local-dev-demo-link (stored as its SHA-256, like auth.HashToken).
INSERT INTO public.share_links (id, system_id, label, prefix, token_hash, created_by)
VALUES ('d0000000-0000-4000-8000-000000000041', 'd0000000-0000-4000-8000-000000000010', 'Local demo link',
    's0p_local-', sha256(convert_to('s0p_local-dev-demo-link', 'UTF8')), 'd0000000-0000-4000-8000-000000000001')
ON CONFLICT (id) DO UPDATE SET revoked_at = NULL, expires_at = NULL;

-- notifications ---------------------------------------------------------------
-- A mixed inbox for both demo accounts: the open alert with a reminder, two
-- resolved alerts, a forecast, a monthly report and announcements. Texts are
-- rendered by the server in the account's language. Fixed ids (from the user
-- and a key), so running this again restores removed ones and read states.
INSERT INTO public.alerts (id, system_id, kind, subject_key, inverter_id, severity, details, opened_at, last_seen_at, resolved_at)
VALUES
    ('d0000000-0000-4000-8000-000000000051', 'd0000000-0000-4000-8000-000000000011', 'no_data', 'system', NULL, 'warning',
        jsonb_build_object('minutes', 95, 'last_seen', to_char((now() - interval '2 days 4 hours') AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')),
        now() - interval '2 days 3 hours', now() - interval '2 days', now() - interval '2 days'),
    ('d0000000-0000-4000-8000-000000000052', 'd0000000-0000-4000-8000-000000000010', 'inverter_error', 'd0000000-0000-4000-8000-000000000032',
        'd0000000-0000-4000-8000-000000000032', 'error',
        jsonb_build_object('code', 7, 'text', 'Isolation fault', 'since', to_char((now() - interval '5 days 1 hour') AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')),
        now() - interval '5 days 1 hour', now() - interval '5 days', now() - interval '5 days')
ON CONFLICT (id) DO NOTHING;

WITH open_alert AS (
    SELECT id FROM public.alerts WHERE system_id = 'd0000000-0000-4000-8000-000000000010'
        AND kind = 'underperformance' AND resolved_at IS NULL LIMIT 1
),
pv AS (SELECT jsonb_build_object('system_name', 'Demo PV', 'timezone', 'Europe/Berlin') AS p),
last_month AS (
    SELECT (date_trunc('month', now() AT TIME ZONE 'Europe/Berlin') - interval '1 month')::date AS from_d,
        date_trunc('month', now() AT TIME ZONE 'Europe/Berlin')::date AS to_d
),
report AS (
    SELECT jsonb_build_object(
        'kind', 'monthly', 'system_id', 'd0000000-0000-4000-8000-000000000010', 'system_name', 'Demo PV',
        'timezone', 'Europe/Berlin', 'currency', 'EUR', 'from', m.from_d, 'to', m.to_d,
        'energy_kwh', round(coalesce(sum(d.energy_wh), 0)::numeric / 1000, 1),
        'revenue', round(coalesce(sum(d.energy_wh), 0)::numeric / 1000 * 0.082, 2),
        'co2_kg', round(coalesce(sum(d.energy_wh), 0)::numeric / 1000 * 0.4, 1),
        'inverters', '[]'::jsonb,
        'link', '/s/d0000000-0000-4000-8000-000000000010/overview?period=month&date=' || m.from_d) AS p,
        m.to_d
    FROM last_month m
    LEFT JOIN public.inverter_daily d ON d.day >= m.from_d AND d.day < m.to_d
        AND d.inverter_id IN (SELECT id FROM public.inverters WHERE system_id = 'd0000000-0000-4000-8000-000000000010')
    GROUP BY m.from_d, m.to_d
),
items (key, system_id, alert_id, type, severity, payload, age, is_read) AS (
    SELECT 'news-swipe', NULL::uuid, NULL::uuid, 'announcement', 'info', jsonb_build_object(
            'title', jsonb_build_object('en', 'New: tidy up your notifications', 'de', 'Neu: Benachrichtigungen aufräumen'),
            'body', jsonb_build_object('en', 'Swipe a notification to the left, or use its × button, to remove it.',
                'de', 'Wischen Sie eine Benachrichtigung nach links oder nutzen Sie ihr ×, um sie zu entfernen.'),
            'link', '/notifications'), interval '20 minutes', false
    UNION ALL SELECT 'underperf-reminder', 'd0000000-0000-4000-8000-000000000010', (SELECT id FROM open_alert), 'alert_reminder', 'warning',
        (SELECT p FROM pv) || jsonb_build_object('kind', 'underperformance', 'subject', 'WR 3', 'details', jsonb_build_object('percent', 60)),
        interval '45 minutes', false
    UNION ALL SELECT 'forecast', 'd0000000-0000-4000-8000-000000000010', NULL, 'forecast', 'info',
        (SELECT p FROM pv) || jsonb_build_object('date', (now() AT TIME ZONE 'Europe/Berlin')::date, 'expected_kwh', 84.6),
        interval '3 hours', true
    UNION ALL SELECT 'underperf-opened', 'd0000000-0000-4000-8000-000000000010', (SELECT id FROM open_alert), 'alert_opened', 'warning',
        (SELECT p FROM pv) || jsonb_build_object('kind', 'underperformance', 'subject', 'WR 3', 'details', jsonb_build_object('percent', 60)),
        interval '1 day', false
    UNION ALL SELECT 'balcony-opened', 'd0000000-0000-4000-8000-000000000011', 'd0000000-0000-4000-8000-000000000051', 'alert_opened', 'warning',
        jsonb_build_object('system_name', 'Demo Balcony', 'timezone', 'Europe/Berlin', 'kind', 'no_data', 'subject', 'Demo Balcony',
            'details', (SELECT details FROM public.alerts WHERE id = 'd0000000-0000-4000-8000-000000000051')),
        interval '2 days 3 hours', true
    UNION ALL SELECT 'balcony-resolved', 'd0000000-0000-4000-8000-000000000011', 'd0000000-0000-4000-8000-000000000051', 'alert_resolved', 'warning',
        jsonb_build_object('system_name', 'Demo Balcony', 'timezone', 'Europe/Berlin', 'kind', 'no_data', 'subject', 'Demo Balcony',
            'details', (SELECT details FROM public.alerts WHERE id = 'd0000000-0000-4000-8000-000000000051')),
        interval '2 days', true
    UNION ALL SELECT 'news-weather', NULL, NULL, 'announcement', 'info', jsonb_build_object(
            'title', jsonb_build_object('en', 'Expected yield now follows the weather', 'de', 'Erwarteter Ertrag jetzt nach Wetter'),
            'body', jsonb_build_object('en', 'Day and month views compare your yield with what the weather allowed, per roof plane.',
                'de', 'Tages- und Monatsansichten vergleichen Ihren Ertrag mit dem, was das Wetter erlaubt hat, je Dachfläche.'),
            'link', '/s/d0000000-0000-4000-8000-000000000010/overview'), interval '4 days', false
    UNION ALL SELECT 'wr2-opened', 'd0000000-0000-4000-8000-000000000010', 'd0000000-0000-4000-8000-000000000052', 'alert_opened', 'error',
        (SELECT p FROM pv) || jsonb_build_object('kind', 'inverter_error', 'subject', 'WR 2',
            'details', (SELECT details FROM public.alerts WHERE id = 'd0000000-0000-4000-8000-000000000052')),
        interval '5 days 1 hour', true
    UNION ALL SELECT 'wr2-resolved', 'd0000000-0000-4000-8000-000000000010', 'd0000000-0000-4000-8000-000000000052', 'alert_resolved', 'error',
        (SELECT p FROM pv) || jsonb_build_object('kind', 'inverter_error', 'subject', 'WR 2',
            'details', (SELECT details FROM public.alerts WHERE id = 'd0000000-0000-4000-8000-000000000052')),
        interval '5 days', true
    UNION ALL SELECT 'report', 'd0000000-0000-4000-8000-000000000010', NULL, 'report', 'info', (SELECT p FROM report),
        now() - ((SELECT to_d FROM report) + time '06:00') AT TIME ZONE 'Europe/Berlin', true
    UNION ALL SELECT 'news-reports', NULL, NULL, 'announcement', 'info', jsonb_build_object(
            'title', jsonb_build_object('en', 'Yield reports by email', 'de', 'Ertragsberichte per E-Mail'),
            'body', jsonb_build_object('en', 'Get a daily, weekly or monthly summary of your systems. Turn it on in your profile.',
                'de', 'Erhalten Sie eine tägliche, wöchentliche oder monatliche Übersicht Ihrer Anlagen. Einschalten im Profil.'),
            'link', '/profile/notifications'), interval '12 days', true
)
INSERT INTO public.notifications (id, user_id, system_id, alert_id, type, severity, payload, created_at, read_at)
SELECT md5(u.id::text || i.key)::uuid, u.id, i.system_id, i.alert_id, i.type, i.severity, i.payload,
    now() - i.age, CASE WHEN i.is_read THEN now() - i.age END
FROM items i,
    unnest(ARRAY['d0000000-0000-4000-8000-000000000002', 'd0000000-0000-4000-8000-000000000003']::uuid[]) AS u(id)
ON CONFLICT (id) DO UPDATE SET payload = EXCLUDED.payload, alert_id = EXCLUDED.alert_id,
    created_at = EXCLUDED.created_at, read_at = EXCLUDED.read_at;

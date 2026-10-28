-- Weather-based expected energy per local day and kWp (from Open-Meteo's
-- historical APIs), for "actual vs expected" in month/year/total views.
CREATE TABLE expected_daily (
    system_id    uuid NOT NULL REFERENCES systems (id) ON DELETE CASCADE,
    day          date NOT NULL,
    kwh_per_kwp  real NOT NULL,
    PRIMARY KEY (system_id, day)
);
ALTER TABLE expected_daily ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON expected_daily FROM anon, authenticated;

-- The coordinates/orientation the rows were computed for; a different key
-- means they are stale and get recomputed.
CREATE TABLE expected_daily_keys (
    system_id  uuid PRIMARY KEY REFERENCES systems (id) ON DELETE CASCADE,
    req_key    text NOT NULL
);
ALTER TABLE expected_daily_keys ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON expected_daily_keys FROM anon, authenticated;

-- Panel orientation for the weather-based expected power (Open-Meteo
-- convention: azimuth 0 = south, -90 = east, 90 = west).
ALTER TABLE systems
    ADD COLUMN tilt_deg    double precision CHECK (tilt_deg BETWEEN 0 AND 90),
    ADD COLUMN azimuth_deg double precision CHECK (azimuth_deg BETWEEN -180 AND 180);

-- Cached weather forecast per system (refreshed on request when stale).
-- req_key encodes the coordinates and orientation it was fetched for.
CREATE TABLE weather_forecasts (
    system_id   uuid PRIMARY KEY REFERENCES systems (id) ON DELETE CASCADE,
    req_key     text NOT NULL,
    fetched_at  timestamptz NOT NULL,
    data        jsonb NOT NULL
);
ALTER TABLE weather_forecasts ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON weather_forecasts FROM anon, authenticated;

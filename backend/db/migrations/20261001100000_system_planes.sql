-- PV planes (module fields with one orientation) of a system, for the
-- weather-based expected power and PVGIS targets. Azimuth: 0 = south,
-- -90 = east, 90 = west. They replace systems.tilt_deg/azimuth_deg; the
-- free-text orientation stays as a description.
CREATE TABLE system_planes (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    system_id    uuid NOT NULL REFERENCES systems (id) ON DELETE CASCADE,
    name         text NOT NULL DEFAULT '',
    kwp          double precision NOT NULL CHECK (kwp >= 0),
    tilt_deg     double precision NOT NULL CHECK (tilt_deg BETWEEN 0 AND 90),
    azimuth_deg  double precision NOT NULL CHECK (azimuth_deg BETWEEN -180 AND 180),
    sort_order   integer NOT NULL DEFAULT 0
);
CREATE INDEX system_planes_system_idx ON system_planes (system_id, sort_order);
ALTER TABLE system_planes ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON system_planes FROM anon, authenticated;

INSERT INTO system_planes (system_id, kwp, tilt_deg, azimuth_deg)
SELECT id, peak_power_kwp, coalesce(tilt_deg, 0), coalesce(azimuth_deg, 0)
FROM systems WHERE tilt_deg IS NOT NULL OR azimuth_deg IS NOT NULL;

ALTER TABLE systems DROP COLUMN tilt_deg, DROP COLUMN azimuth_deg;

-- Plane-of-array irradiation per local day (kWh/m², kWp-weighted over the
-- planes), for the performance ratio. The weather model key changed (v2), so
-- the backfill recomputes every row and fills it.
ALTER TABLE expected_daily ADD COLUMN irradiation_kwh_m2 real;

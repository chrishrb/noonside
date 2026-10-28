-- Manual corrections of daily energy (issue #42). energy_wh stays the value
-- the device reported and ingest keeps updating it; readers use
-- COALESCE(manual_energy_wh, energy_wh). Clearing the correction brings the
-- device value back.
ALTER TABLE inverter_daily
    ADD COLUMN manual_energy_wh double precision CHECK (manual_energy_wh >= 0),
    ADD COLUMN manual_by        uuid REFERENCES profiles (id) ON DELETE SET NULL,
    ADD COLUMN manual_at        timestamptz;

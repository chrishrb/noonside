-- Rated AC power of an inverter (W), for the clipping alert; NULL if unknown.
ALTER TABLE inverters ADD COLUMN max_ac_power_w double precision CHECK (max_ac_power_w > 0);

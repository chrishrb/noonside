-- Minute data (measurement_days) of a system is kept for the current year
-- plus this many full calendar years (system time zone); older minute data
-- is deleted and not imported. NULL keeps everything. Daily energy is not
-- affected.
ALTER TABLE systems ADD COLUMN minute_retention_years integer
    CHECK (minute_retention_years BETWEEN 0 AND 100);

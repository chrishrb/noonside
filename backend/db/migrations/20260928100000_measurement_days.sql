-- Minute data as one row per inverter and UTC day, with the samples as
-- parallel arrays. A row per sample cost ~150 bytes (tuple header, two
-- indexes); the arrays are TOAST-compressed and cut storage by an order of
-- magnitude. secs is sorted and unique; the value arrays have the same
-- length and may contain NULLs.
CREATE TABLE measurement_days (
    inverter_id    uuid NOT NULL REFERENCES inverters (id) ON DELETE CASCADE,
    day            date NOT NULL,
    secs           integer[] NOT NULL,
    pac_w          real[] NOT NULL,
    pdc_w          real[] NOT NULL,
    udc_v          real[] NOT NULL,
    eday_wh        real[] NOT NULL,
    temperature_c  real[] NOT NULL,
    PRIMARY KEY (inverter_id, day)
);
ALTER TABLE measurement_days ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    ALTER TABLE measurement_days
        ALTER COLUMN secs SET COMPRESSION lz4,
        ALTER COLUMN pac_w SET COMPRESSION lz4,
        ALTER COLUMN pdc_w SET COMPRESSION lz4,
        ALTER COLUMN udc_v SET COMPRESSION lz4,
        ALTER COLUMN eday_wh SET COMPRESSION lz4,
        ALTER COLUMN temperature_c SET COMPRESSION lz4;
EXCEPTION WHEN feature_not_supported THEN
    NULL; -- keep the default (pglz)
END $$;

INSERT INTO measurement_days
SELECT inverter_id, (time AT TIME ZONE 'UTC')::date,
    array_agg((floor(extract(epoch FROM time))::bigint % 86400)::int ORDER BY time),
    array_agg(pac_w::real ORDER BY time),
    array_agg(pdc_w::real ORDER BY time),
    array_agg(udc_v::real ORDER BY time),
    array_agg(eday_wh::real ORDER BY time),
    array_agg(temperature_c::real ORDER BY time)
FROM measurements
GROUP BY 1, 2;

DROP TABLE measurements;

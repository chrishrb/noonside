-- Implausible values that ingest dropped (negative or too high power, energy
-- above what the kWp allows, timestamps in the future), counted per inverter,
-- local day and reason. Pruned after 90 days.
CREATE TABLE ingest_rejections (
    system_id    uuid NOT NULL REFERENCES systems (id) ON DELETE CASCADE,
    inverter_id  uuid NOT NULL REFERENCES inverters (id) ON DELETE CASCADE,
    day          date NOT NULL,
    reason       text NOT NULL,
    count        integer NOT NULL,
    sample       jsonb,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (system_id, inverter_id, day, reason)
);
CREATE INDEX ingest_rejections_day_idx ON ingest_rejections (day);
ALTER TABLE ingest_rejections ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON ingest_rejections FROM anon, authenticated;

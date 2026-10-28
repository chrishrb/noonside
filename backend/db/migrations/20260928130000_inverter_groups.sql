-- Inverter groups (e.g. "South", "North") to filter a system's views to a
-- subset of its inverters. An inverter belongs to at most one group.
CREATE TABLE inverter_groups (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    system_id   uuid NOT NULL REFERENCES systems (id) ON DELETE CASCADE,
    name        text NOT NULL,
    color       text NOT NULL DEFAULT '',
    sort_order  integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (system_id, name),
    UNIQUE (system_id, id)
);
ALTER TABLE inverter_groups ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON inverter_groups FROM anon, authenticated;

-- The composite key keeps an inverter in a group of its own system; deleting
-- a group only clears group_id.
ALTER TABLE inverters ADD COLUMN group_id uuid;
ALTER TABLE inverters ADD CONSTRAINT inverters_group_fkey
    FOREIGN KEY (system_id, group_id) REFERENCES inverter_groups (system_id, id)
    ON DELETE SET NULL (group_id);
CREATE INDEX inverters_group_idx ON inverters (group_id) WHERE group_id IS NOT NULL;

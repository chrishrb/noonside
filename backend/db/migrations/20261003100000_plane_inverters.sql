-- Inverters are assigned to module planes, which replace the inverter groups
-- (not migrated). The composite key keeps an inverter on a plane of its own
-- system; deleting a plane only clears plane_id.
ALTER TABLE inverters DROP COLUMN group_id;
DROP TABLE inverter_groups;

ALTER TABLE system_planes ADD CONSTRAINT system_planes_system_id_id_key UNIQUE (system_id, id);

ALTER TABLE inverters ADD COLUMN plane_id uuid;
ALTER TABLE inverters ADD CONSTRAINT inverters_plane_fkey
    FOREIGN KEY (system_id, plane_id) REFERENCES system_planes (system_id, id)
    ON DELETE SET NULL (plane_id);
CREATE INDEX inverters_plane_idx ON inverters (plane_id) WHERE plane_id IS NOT NULL;

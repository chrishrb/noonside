-- Replaces the session advisory lock per data source run. On Lambda a run
-- can be frozen or killed while the pooler keeps its connection (and the
-- lock) open; a lease expires on its own.
ALTER TABLE data_sources ADD COLUMN locked_until timestamptz;

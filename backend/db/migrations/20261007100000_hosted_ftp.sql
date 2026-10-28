-- Hosted FTP (solarlog_hosted): each source owns an upload account on
-- solar0's Storage Box; at most one per system.
CREATE UNIQUE INDEX data_sources_hosted_per_system ON data_sources (system_id)
  WHERE kind = 'solarlog_hosted';

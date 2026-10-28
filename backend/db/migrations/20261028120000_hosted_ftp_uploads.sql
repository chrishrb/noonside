-- Hosted FTP moves to noonside's own SFTPGo server, which reports every
-- upload (POST /hooks/hosted-ftp). solarlog_hosted sources are imported after
-- an upload instead of on their poll interval: upload_at is the last upload,
-- imported_upload_at the newest one a successful run imported.
ALTER TABLE data_sources
  ADD COLUMN upload_at timestamptz,
  ADD COLUMN imported_upload_at timestamptz;

-- Sources of the former Hetzner Storage Box no longer work. Run them once,
-- so they fail with the message to re-create them.
UPDATE data_sources SET upload_at = now()
WHERE kind = 'solarlog_hosted' AND config ? 'subaccount_id';

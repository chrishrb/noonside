-- The solarlog_dir data source kind (export files in a server directory) is
-- gone; the CLI `solar0 import solarlog DIR` still reads a directory.
DELETE FROM data_sources WHERE kind = 'solarlog_dir';

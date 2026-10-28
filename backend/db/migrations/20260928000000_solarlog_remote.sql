-- solarlog_ftp and solarlog_sftp become one kind, solarlog_remote, with
-- config.protocol (ftp, ftps or sftp) in place of config.tls.
UPDATE data_sources
SET kind = 'solarlog_remote',
    config = (config - 'tls') || jsonb_build_object('protocol',
        CASE
            WHEN kind = 'solarlog_sftp' THEN 'sftp'
            WHEN coalesce((config ->> 'tls')::boolean, false) THEN 'ftps'
            ELSE 'ftp'
        END),
    updated_at = now()
WHERE kind IN ('solarlog_ftp', 'solarlog_sftp');

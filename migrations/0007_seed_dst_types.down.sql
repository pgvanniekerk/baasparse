-- Roll back DST_TYPE seed entries.
DELETE FROM DST_TYPE WHERE DST_NAME IN ('LocalFileSystem', 'RemoteFileSystem');

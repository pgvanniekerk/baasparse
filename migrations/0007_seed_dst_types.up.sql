-- Application-defined data source types.
-- DST_CONNECTION_SCHEMA is a JSON Schema (draft 2020-12) describing the
-- connection details required to access a data source of this type.
-- Implementation details (paths, table names, etc.) are not part of this schema.

-- LocalFileSystem: operates as the baasparse OS user on the local
-- filesystem. The only required configuration is the root directory path;
-- the baasparse process must have the necessary OS-level read/write access.
INSERT INTO DST_TYPE (DST_NAME, DST_CONNECTION_SCHEMA, DST_COLLECTION_SCHEMA, DST_DISTRIBUTION_SCHEMA)
VALUES (
    'LocalFileSystem',
    -- No connection details; access is via the baasparse OS user on the local filesystem.
    $json${
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": "baasparse:dst:localfilesystem:connection",
        "title": "LocalFileSystem Connection",
        "description": "LocalFileSystem requires no connection parameters. Access is provided by the OS user running baasparse.",
        "type": "object",
        "additionalProperties": false
    }$json$,
    -- Where to collect files from on the local filesystem.
    $json${
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": "baasparse:dst:localfilesystem:collection",
        "title": "LocalFileSystem Collection",
        "description": "Specifies the local directory to collect files from.",
        "type": "object",
        "additionalProperties": false,
        "required": ["path"],
        "properties": {
            "path": {
                "type": "string",
                "description": "Absolute path to the directory to collect files from.",
                "minLength": 1
            },
            "pattern": {
                "type": "string",
                "description": "Glob pattern for matching file names (e.g. \"*.csv\"). Defaults to all files.",
                "default": "*"
            }
        }
    }$json$,
    -- Where to distribute files to on the local filesystem.
    $json${
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": "baasparse:dst:localfilesystem:distribution",
        "title": "LocalFileSystem Distribution",
        "description": "Specifies the local directory to distribute files to.",
        "type": "object",
        "additionalProperties": false,
        "required": ["path"],
        "properties": {
            "path": {
                "type": "string",
                "description": "Absolute path to the directory to distribute files to.",
                "minLength": 1
            }
        }
    }$json$
);

-- RemoteFileSystem: connects to a remote host via SFTP using
-- username/password authentication. The password is stored in the data
-- source configuration and should be protected at the application layer.
INSERT INTO DST_TYPE (DST_NAME, DST_CONNECTION_SCHEMA, DST_COLLECTION_SCHEMA, DST_DISTRIBUTION_SCHEMA)
VALUES (
    'RemoteFileSystem',
    -- SFTP connection credentials only; no path or pattern.
    $json${
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": "baasparse:dst:remotefilesystem:connection",
        "title": "RemoteFileSystem Connection",
        "description": "SFTP connection credentials for accessing a remote filesystem.",
        "type": "object",
        "additionalProperties": false,
        "required": ["host", "username", "password"],
        "properties": {
            "host": {
                "type": "string",
                "description": "Hostname or IP address of the remote SFTP server.",
                "minLength": 1
            },
            "port": {
                "type": "integer",
                "description": "SFTP port on the remote host.",
                "minimum": 1,
                "maximum": 65535,
                "default": 22
            },
            "username": {
                "type": "string",
                "description": "Username for SFTP authentication.",
                "minLength": 1
            },
            "password": {
                "type": "string",
                "description": "Password for SFTP authentication. Should be protected at the application layer.",
                "minLength": 1
            }
        }
    }$json$,
    -- Where to collect files from on the remote filesystem.
    $json${
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": "baasparse:dst:remotefilesystem:collection",
        "title": "RemoteFileSystem Collection",
        "description": "Specifies the remote directory to collect files from.",
        "type": "object",
        "additionalProperties": false,
        "required": ["path"],
        "properties": {
            "path": {
                "type": "string",
                "description": "Absolute path to the directory on the remote host to collect files from.",
                "minLength": 1
            },
            "pattern": {
                "type": "string",
                "description": "Glob pattern for matching file names (e.g. \"*.csv\"). Defaults to all files.",
                "default": "*"
            }
        }
    }$json$,
    -- Where to distribute files to on the remote filesystem.
    $json${
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": "baasparse:dst:remotefilesystem:distribution",
        "title": "RemoteFileSystem Distribution",
        "description": "Specifies the remote directory to distribute files to.",
        "type": "object",
        "additionalProperties": false,
        "required": ["path"],
        "properties": {
            "path": {
                "type": "string",
                "description": "Absolute path to the directory on the remote host to distribute files to.",
                "minLength": 1
            }
        }
    }$json$
);

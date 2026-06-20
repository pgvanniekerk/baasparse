-- Catalog of data source types defined by the application.
-- Each type carries a DST_CONNECTION_SCHEMA (JSON Schema) that describes
-- the connection details required to access a data source of this type.
-- Implementation details (paths, table names, queue names etc.) are NOT
-- part of this schema. Not editable by end users; seeded at migration time.
CREATE TABLE DST_TYPE (
    DST_UID                UUID  PRIMARY KEY DEFAULT gen_random_uuid(),
    DST_NAME               TEXT  NOT NULL,
    DST_CONNECTION_SCHEMA  JSONB NOT NULL,
    DST_COLLECTION_SCHEMA  JSONB NOT NULL,
    DST_DISTRIBUTION_SCHEMA JSONB NOT NULL
);

-- Enforce global uniqueness of DST_NAME.
CREATE UNIQUE INDEX UIDX_DST_NAME ON DST_TYPE(DST_NAME);

COMMENT ON TABLE  DST_TYPE                           IS 'Application-defined catalog of data source types. Not editable by end users.';
COMMENT ON COLUMN DST_TYPE.DST_UID                   IS 'UUID primary key. Auto-generated via gen_random_uuid() when not supplied on insert.';
COMMENT ON COLUMN DST_TYPE.DST_NAME                  IS 'Human-readable type name (e.g. ''LocalFileSystem''). Globally unique.';
COMMENT ON COLUMN DST_TYPE.DST_CONNECTION_SCHEMA     IS 'JSON Schema (draft 2020-12) describing the connection details required to access a data source of this type (e.g. host, credentials, port). Does NOT include collection or distribution details.';
COMMENT ON COLUMN DST_TYPE.DST_COLLECTION_SCHEMA     IS 'JSON Schema (draft 2020-12) describing where to collect data from once connected (e.g. directory path, glob pattern, table name, queue name).';
COMMENT ON COLUMN DST_TYPE.DST_DISTRIBUTION_SCHEMA   IS 'JSON Schema (draft 2020-12) describing where to distribute data to once connected (e.g. directory path, table name, queue name).';

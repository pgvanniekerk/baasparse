-- A configured data source instance. Bound to a
-- DST_TYPE (DS_DST_UID) and carrying a connection specification whose shape
-- must conform to the referenced type's DST_CONNECTION_SCHEMA.
CREATE TABLE DS_DATA_SOURCE (
    DS_UID                      UUID  PRIMARY KEY DEFAULT gen_random_uuid(),
    DS_DST_UID                  UUID  NOT NULL
        CONSTRAINT FK_DS_DST_UID REFERENCES DST_TYPE(DST_UID),
    DS_NAME                     TEXT  NOT NULL,
    DS_CONNECTION_SPECIFICATION JSONB NOT NULL,
    DS_TA_UID                   UUID  NOT NULL
        CONSTRAINT FK_DS_TA_UID REFERENCES TA_TRANSACTION_AUDIT(TA_UID)
);

-- Enforce global uniqueness of DS_NAME.
CREATE UNIQUE INDEX UIDX_DS_NAME ON DS_DATA_SOURCE(DS_NAME);

-- Index to support lookups by type.
CREATE INDEX IDX_DS_DST_UID ON DS_DATA_SOURCE(DS_DST_UID);

COMMENT ON TABLE  DS_DATA_SOURCE                             IS 'A configured data source instance.';
COMMENT ON COLUMN DS_DATA_SOURCE.DS_UID                      IS 'UUID primary key. Auto-generated via gen_random_uuid() when not supplied on insert.';
COMMENT ON COLUMN DS_DATA_SOURCE.DS_DST_UID                  IS 'Foreign key to DST_TYPE; determines which connection, collection, and distribution schemas apply.';
COMMENT ON COLUMN DS_DATA_SOURCE.DS_NAME                     IS 'Human-friendly name for the data source. Globally unique.';
COMMENT ON COLUMN DS_DATA_SOURCE.DS_CONNECTION_SPECIFICATION IS 'JSONB object containing the connection details for this data source. Must conform to the referenced DST_TYPE.DST_CONNECTION_SCHEMA; structural validation is enforced in the application layer.';
COMMENT ON COLUMN DS_DATA_SOURCE.DS_TA_UID                   IS 'Foreign key to TA_TRANSACTION_AUDIT; identifies the audit row that created this data source.';

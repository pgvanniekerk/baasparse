-- Stores each raw payload retrieved by a collector before any processing.
-- Acts as the durable ingestion record so failed runs can be retried
-- without re-reading from the source.
CREATE TABLE CR_COLLECTION_REQUEST (
    CR_UID         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    CR_P_UID       UUID        NOT NULL
        CONSTRAINT FK_CR_P_UID REFERENCES P_PIPELINE(P_UID),
    CR_C_UID       UUID        NOT NULL
        CONSTRAINT FK_CR_C_UID REFERENCES C_COLLECTOR(C_UID),
    CR_HASH_KEY    TEXT        NOT NULL,
    CR_FILE_NAME   TEXT,
    CR_DATA        BYTEA       NOT NULL,
    CR_TIMESTAMPTZ TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Prevent duplicate ingestion of the same payload for a given (pipeline, collector).
CREATE UNIQUE INDEX UIDX_CR_P_UID_C_UID_HASH_KEY ON CR_COLLECTION_REQUEST(CR_P_UID, CR_C_UID, CR_HASH_KEY);

-- Indexes to support lookups by pipeline and collector.
CREATE INDEX IDX_CR_P_UID ON CR_COLLECTION_REQUEST(CR_P_UID);
CREATE INDEX IDX_CR_C_UID ON CR_COLLECTION_REQUEST(CR_C_UID);

COMMENT ON TABLE  CR_COLLECTION_REQUEST               IS 'Raw payloads retrieved by a collector. Persisted before processing so failed runs can be retried.';
COMMENT ON COLUMN CR_COLLECTION_REQUEST.CR_UID        IS 'UUID primary key. Auto-generated via gen_random_uuid() when not supplied on insert.';
COMMENT ON COLUMN CR_COLLECTION_REQUEST.CR_P_UID      IS 'Foreign key to P_PIPELINE; the pipeline that triggered this collection.';
COMMENT ON COLUMN CR_COLLECTIOthiN_REQUEST.CR_C_UID      IS 'Foreign key to C_COLLECTOR; the collector that retrieved this payload.';
COMMENT ON COLUMN CR_COLLECTION_REQUEST.CR_HASH_KEY   IS 'Content hash of CR_DATA. Unique per (CR_P_UID, CR_C_UID) to prevent duplicate ingestion.';
COMMENT ON COLUMN CR_COLLECTION_REQUEST.CR_FILE_NAME  IS 'Original file name of the collected artefact. NULL for non-file sources (e.g. database records, AMQP messages).';
COMMENT ON COLUMN CR_COLLECTION_REQUEST.CR_DATA       IS 'Raw binary content of the collected artefact (file contents, serialised record, message body, etc.).';
COMMENT ON COLUMN CR_COLLECTION_REQUEST.CR_TIMESTAMPTZ IS 'Timestamp the payload was received from the source.';

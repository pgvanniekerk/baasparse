-- Distribution target attached to a pipeline. A pipeline may have
-- one or more distributors.
CREATE TABLE D_DISTRIBUTOR (
    D_UID                          UUID  PRIMARY KEY DEFAULT gen_random_uuid(),
    D_P_UID                        UUID  NOT NULL
        CONSTRAINT FK_D_P_UID  REFERENCES P_PIPELINE(P_UID),
    D_DS_UID                       UUID  NOT NULL
        CONSTRAINT FK_D_DS_UID REFERENCES DS_DATA_SOURCE(DS_UID),
    D_NAME                         TEXT  NOT NULL,
    D_DISTRIBUTION_SPECIFICATION   JSONB NOT NULL,
    D_T_UID                        UUID
        CONSTRAINT FK_D_T_UID  REFERENCES T_TRANSFORMER(T_UID),
    D_TRANSFORMATION_SPECIFICATION JSONB,
    D_STATUS                       TEXT  NOT NULL DEFAULT 'ACTIVE'
        CHECK (D_STATUS IN ('ACTIVE', 'INACTIVE')),
    D_TA_UID                       UUID  NOT NULL
        CONSTRAINT FK_D_TA_UID REFERENCES TA_TRANSACTION_AUDIT(TA_UID)
);

-- Enforce uniqueness of D_NAME within a pipeline.
CREATE UNIQUE INDEX UIDX_D_P_UID_NAME ON D_DISTRIBUTOR(D_P_UID, D_NAME);

-- Index to support lookups of all distributors for a pipeline.
CREATE INDEX IDX_D_P_UID ON D_DISTRIBUTOR(D_P_UID);

COMMENT ON TABLE  D_DISTRIBUTOR                              IS 'Distribution target attached to a pipeline. A pipeline may have one or more distributors.';
COMMENT ON COLUMN D_DISTRIBUTOR.D_UID                        IS 'UUID primary key. Auto-generated via gen_random_uuid() when not supplied on insert.';
COMMENT ON COLUMN D_DISTRIBUTOR.D_P_UID                      IS 'Foreign key to P_PIPELINE; the pipeline this distributor belongs to.';
COMMENT ON COLUMN D_DISTRIBUTOR.D_DS_UID                     IS 'Foreign key to DS_DATA_SOURCE; the data source to distribute to.';
COMMENT ON COLUMN D_DISTRIBUTOR.D_NAME                       IS 'Human-friendly name for this distributor. Unique within its pipeline.';
COMMENT ON COLUMN D_DISTRIBUTOR.D_DISTRIBUTION_SPECIFICATION IS 'JSONB distribution details (path, table, queue, etc.) conforming to the data source type''s DST_DISTRIBUTION_SCHEMA.';
COMMENT ON COLUMN D_DISTRIBUTOR.D_T_UID                      IS 'Foreign key to T_TRANSFORMER; converts the payload to the target format. NULL for plain pass-through (e.g. file transfer).';
COMMENT ON COLUMN D_DISTRIBUTOR.D_TRANSFORMATION_SPECIFICATION IS 'JSONB transformation configuration conforming to T_TRANSFORMER.T_TRANSFORMATION_SCHEMA. NULL when D_T_UID is NULL.';
COMMENT ON COLUMN D_DISTRIBUTOR.D_STATUS                     IS 'Lifecycle status. ACTIVE: will receive records. INACTIVE: skipped. Default ACTIVE.';
COMMENT ON COLUMN D_DISTRIBUTOR.D_TA_UID                     IS 'Foreign key to TA_TRANSACTION_AUDIT; identifies the audit row that created this distributor.';

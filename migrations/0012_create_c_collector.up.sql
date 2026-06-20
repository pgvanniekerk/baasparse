-- Collection configuration for a pipeline. Exactly one collector per
-- pipeline, enforced by UIDX_C_P_UID.
CREATE TABLE C_COLLECTOR (
    C_UID                          UUID  PRIMARY KEY DEFAULT gen_random_uuid(),
    C_P_UID                        UUID  NOT NULL
        CONSTRAINT FK_C_P_UID  REFERENCES P_PIPELINE(P_UID),
    C_DS_UID                       UUID  NOT NULL
        CONSTRAINT FK_C_DS_UID REFERENCES DS_DATA_SOURCE(DS_UID),
    C_COLLECTION_SPECIFICATION     JSONB NOT NULL,
    C_T_UID                        UUID
        CONSTRAINT FK_C_T_UID  REFERENCES T_TRANSFORMER(T_UID),
    C_TRANSFORMATION_SPECIFICATION JSONB,
    C_STATUS                       TEXT  NOT NULL DEFAULT 'ACTIVE'
        CHECK (C_STATUS IN ('ACTIVE', 'INACTIVE')),
    C_TA_UID                       UUID  NOT NULL
        CONSTRAINT FK_C_TA_UID REFERENCES TA_TRANSACTION_AUDIT(TA_UID)
);

-- Enforce 1:1 relationship with P_PIPELINE.
CREATE UNIQUE INDEX UIDX_C_P_UID ON C_COLLECTOR(C_P_UID);

COMMENT ON TABLE  C_COLLECTOR                              IS 'Collection configuration for a pipeline. One collector per pipeline.';
COMMENT ON COLUMN C_COLLECTOR.C_UID                        IS 'UUID primary key. Auto-generated via gen_random_uuid() when not supplied on insert.';
COMMENT ON COLUMN C_COLLECTOR.C_P_UID                      IS 'Foreign key to P_PIPELINE. Unique — enforces the 1:1 relationship.';
COMMENT ON COLUMN C_COLLECTOR.C_DS_UID                     IS 'Foreign key to DS_DATA_SOURCE; the data source to collect from.';
COMMENT ON COLUMN C_COLLECTOR.C_COLLECTION_SPECIFICATION   IS 'JSONB collection details (path, query, queue, format, etc.) conforming to the data source type''s DST_COLLECTION_SCHEMA.';
COMMENT ON COLUMN C_COLLECTOR.C_T_UID                      IS 'Foreign key to T_TRANSFORMER; the transformer to apply before distribution. NULL for plain pass-through (e.g. file transfer).';
COMMENT ON COLUMN C_COLLECTOR.C_TRANSFORMATION_SPECIFICATION IS 'JSONB transformation configuration conforming to T_TRANSFORMER.T_TRANSFORMATION_SCHEMA. NULL when C_T_UID is NULL.';
COMMENT ON COLUMN C_COLLECTOR.C_STATUS                     IS 'Lifecycle status. ACTIVE: enabled. INACTIVE: disabled. Default ACTIVE.';
COMMENT ON COLUMN C_COLLECTOR.C_TA_UID                     IS 'Foreign key to TA_TRANSACTION_AUDIT; identifies the audit row that created this collector.';

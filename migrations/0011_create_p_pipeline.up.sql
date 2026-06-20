-- Top-level ETL pipeline configuration. Wires together
-- exactly one C_COLLECTOR with one or more D_DISTRIBUTORs.
CREATE TABLE P_PIPELINE (
    P_UID         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    P_NAME        TEXT NOT NULL,
    P_DESCRIPTION TEXT,
    P_STATUS      TEXT NOT NULL DEFAULT 'ACTIVE'
        CHECK (P_STATUS IN ('ACTIVE', 'INACTIVE')),
    P_TA_UID      UUID NOT NULL
        CONSTRAINT FK_P_TA_UID REFERENCES TA_TRANSACTION_AUDIT(TA_UID)
);

-- Enforce global uniqueness of P_NAME.
CREATE UNIQUE INDEX UIDX_P_NAME ON P_PIPELINE(P_NAME);

COMMENT ON TABLE  P_PIPELINE               IS 'Top-level ETL pipeline configuration. Connects one collector to one or more distributors.';
COMMENT ON COLUMN P_PIPELINE.P_UID         IS 'UUID primary key. Auto-generated via gen_random_uuid() when not supplied on insert.';
COMMENT ON COLUMN P_PIPELINE.P_NAME        IS 'Human-friendly pipeline name. Globally unique.';
COMMENT ON COLUMN P_PIPELINE.P_DESCRIPTION IS 'Optional free-text description of the pipeline''s purpose.';
COMMENT ON COLUMN P_PIPELINE.P_STATUS      IS 'Lifecycle status. ACTIVE: eligible for execution. INACTIVE: disabled. Default ACTIVE.';
COMMENT ON COLUMN P_PIPELINE.P_TA_UID      IS 'Foreign key to TA_TRANSACTION_AUDIT; identifies the audit row that created this pipeline.';

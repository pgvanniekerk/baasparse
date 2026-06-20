-- Records each attempt by a distributor to deliver a transformed record.
-- One TD_TRANSFORMED_DATA row fans out to one row here per active distributor.
CREATE TABLE DR_DISTRIBUTION_REQUEST (
    DR_UID            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    DR_D_UID          UUID        NOT NULL
        CONSTRAINT FK_DR_D_UID   REFERENCES D_DISTRIBUTOR(D_UID),
    DR_CR_UID         UUID        NOT NULL
        CONSTRAINT FK_DR_CR_UID  REFERENCES CR_COLLECTION_REQUEST(CR_UID),
    DR_TD_UID         UUID
        CONSTRAINT FK_DR_TD_UID  REFERENCES TD_TRANSFORMED_DATA(TD_UID),
    DR_CONTENT        BYTEA,
    DR_STATUS         TEXT        NOT NULL DEFAULT 'PENDING'
        CHECK (DR_STATUS IN ('PENDING', 'SUCCESS', 'FAILED')),
    DR_FAILURE_REASON TEXT,
    DR_TIMESTAMPTZ    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes to support lookups by distributor, collection request, and transformed record.
CREATE INDEX IDX_DR_D_UID   ON DR_DISTRIBUTION_REQUEST(DR_D_UID);
CREATE INDEX IDX_DR_CR_UID  ON DR_DISTRIBUTION_REQUEST(DR_CR_UID);
CREATE INDEX IDX_DR_TD_UID  ON DR_DISTRIBUTION_REQUEST(DR_TD_UID);

COMMENT ON TABLE  DR_DISTRIBUTION_REQUEST                  IS 'Records each delivery attempt by a distributor for a transformed record.';
COMMENT ON COLUMN DR_DISTRIBUTION_REQUEST.DR_UID           IS 'UUID primary key. Auto-generated via gen_random_uuid() when not supplied on insert.';
COMMENT ON COLUMN DR_DISTRIBUTION_REQUEST.DR_D_UID         IS 'Foreign key to D_DISTRIBUTOR; the distributor that attempted delivery.';
COMMENT ON COLUMN DR_DISTRIBUTION_REQUEST.DR_CR_UID        IS 'Foreign key to CR_COLLECTION_REQUEST; the original collection request this distribution is derived from.';
COMMENT ON COLUMN DR_DISTRIBUTION_REQUEST.DR_TD_UID        IS 'Foreign key to TD_TRANSFORMED_DATA; the transformed record being distributed. NULL for plain pass-through (e.g. file transfer) where no transformation occurs.';
COMMENT ON COLUMN DR_DISTRIBUTION_REQUEST.DR_CONTENT       IS 'Converted payload written (or attempted to be written) to the destination. NULL while PENDING.';
COMMENT ON COLUMN DR_DISTRIBUTION_REQUEST.DR_STATUS        IS 'Delivery status. PENDING: queued. SUCCESS: delivered. FAILED: delivery failed. Default PENDING.';
COMMENT ON COLUMN DR_DISTRIBUTION_REQUEST.DR_FAILURE_REASON IS 'Human-readable error description when DR_STATUS = FAILED. NULL otherwise.';
COMMENT ON COLUMN DR_DISTRIBUTION_REQUEST.DR_TIMESTAMPTZ   IS 'Timestamp this distribution request was created.';

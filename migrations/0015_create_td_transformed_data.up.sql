-- Stores individual records parsed from a CR_COLLECTION_REQUEST and
-- serialised as MessagePack, ready for distribution.
CREATE TABLE TD_TRANSFORMED_DATA (
    TD_UID         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    TD_CR_UID      UUID        NOT NULL
        CONSTRAINT FK_TD_CR_UID REFERENCES CR_COLLECTION_REQUEST(CR_UID),
    TD_C_UID       UUID        NOT NULL
        CONSTRAINT FK_TD_C_UID  REFERENCES C_COLLECTOR(C_UID),
    TD_DATA        BYTEA       NOT NULL,
    TD_TIMESTAMPTZ TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes to support lookups by collection request and collector.
CREATE INDEX IDX_TD_CR_UID ON TD_TRANSFORMED_DATA(TD_CR_UID);
CREATE INDEX IDX_TD_C_UID  ON TD_TRANSFORMED_DATA(TD_C_UID);

COMMENT ON TABLE  TD_TRANSFORMED_DATA               IS 'Individual records parsed from a collection request and serialised as MessagePack, ready for distribution.';
COMMENT ON COLUMN TD_TRANSFORMED_DATA.TD_UID        IS 'UUID primary key. Auto-generated via gen_random_uuid() when not supplied on insert.';
COMMENT ON COLUMN TD_TRANSFORMED_DATA.TD_CR_UID     IS 'Foreign key to CR_COLLECTION_REQUEST; the collection request this record was parsed from.';
COMMENT ON COLUMN TD_TRANSFORMED_DATA.TD_C_UID      IS 'Foreign key to C_COLLECTOR; the collector that retrieved the source payload.';
COMMENT ON COLUMN TD_TRANSFORMED_DATA.TD_DATA       IS 'The transformed record serialised as MessagePack.';
COMMENT ON COLUMN TD_TRANSFORMED_DATA.TD_TIMESTAMPTZ IS 'Timestamp this record was inserted.';

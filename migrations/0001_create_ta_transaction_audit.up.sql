-- Append-only audit log of mutating control-plane transactions.
-- Each row captures who performed an action (TA_USERNAME), when it happened
-- (TA_TIMESTAMPTZ), the payload (TA_DATA), and a correlation ID linking
-- related events across a single logical operation (TA_CORRELATION_ID).
--
-- User identity is recorded as a plain username string extracted from the
-- inbound JWT; baasparse does not manage users internally.
CREATE TABLE TA_TRANSACTION_AUDIT (
    TA_UID              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    TA_TIMESTAMPTZ      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    TA_USERNAME         TEXT         NOT NULL,
    TA_TRANSACTION_TYPE VARCHAR(200) NOT NULL,
    TA_DATA             JSONB        NOT NULL,
    TA_CORRELATION_ID   TEXT         NOT NULL
);

COMMENT ON TABLE  TA_TRANSACTION_AUDIT                       IS 'Append-only audit log of mutating control-plane transactions.';
COMMENT ON COLUMN TA_TRANSACTION_AUDIT.TA_UID                IS 'UUID primary key. Auto-generated via gen_random_uuid() when not supplied on insert.';
COMMENT ON COLUMN TA_TRANSACTION_AUDIT.TA_TIMESTAMPTZ        IS 'Timestamp the audited event occurred.';
COMMENT ON COLUMN TA_TRANSACTION_AUDIT.TA_USERNAME           IS 'Username of the caller extracted from the inbound JWT subject claim. Always required.';
COMMENT ON COLUMN TA_TRANSACTION_AUDIT.TA_TRANSACTION_TYPE   IS 'Category/type of the audited transaction (e.g. ''CreatePipeline'', ''UpdateDataSource''). Used for filtering and reporting.';
COMMENT ON COLUMN TA_TRANSACTION_AUDIT.TA_DATA               IS 'JSONB object describing the audited event payload. Structural shape is enforced in the application layer.';
COMMENT ON COLUMN TA_TRANSACTION_AUDIT.TA_CORRELATION_ID     IS 'Trace/correlation identifier linking related audit events across a single logical operation (e.g. one inbound HTTP request).';

-- System-controlled registry of available data transformations.
-- Each row describes how to convert from one DT_DATA_TYPE to another.
-- Not editable by end users. FK constraint names are disambiguated by
-- semantic role (FROM / TO) because both FKs reference the same table.
CREATE TABLE T_TRANSFORMER (
    T_UID                   UUID  PRIMARY KEY DEFAULT gen_random_uuid(),
    T_FROM_DT_UID           UUID  NOT NULL
        CONSTRAINT FK_T_FROM_DT_UID REFERENCES DT_DATA_TYPE(DT_UID),
    T_TO_DT_UID             UUID  NOT NULL
        CONSTRAINT FK_T_TO_DT_UID   REFERENCES DT_DATA_TYPE(DT_UID),
    T_TRANSFORMATION_SCHEMA JSONB NOT NULL,
    T_DESCRIPTION           TEXT
);

-- At most one transformer per (FROM, TO) pair.
CREATE UNIQUE INDEX UIDX_T_FROM_DT_UID_TO_DT_UID ON T_TRANSFORMER(T_FROM_DT_UID, T_TO_DT_UID);

COMMENT ON TABLE  T_TRANSFORMER                        IS 'Registry of available data format transformations. Each row maps one DT_DATA_TYPE to another.';
COMMENT ON COLUMN T_TRANSFORMER.T_UID                  IS 'UUID primary key. Auto-generated via gen_random_uuid() when not supplied on insert.';
COMMENT ON COLUMN T_TRANSFORMER.T_FROM_DT_UID          IS 'Foreign key to DT_DATA_TYPE; the source data type this transformer reads from.';
COMMENT ON COLUMN T_TRANSFORMER.T_TO_DT_UID            IS 'Foreign key to DT_DATA_TYPE; the target data type this transformer produces.';
COMMENT ON COLUMN T_TRANSFORMER.T_TRANSFORMATION_SCHEMA IS 'JSON Schema describing the configuration options available for this transformation.';
COMMENT ON COLUMN T_TRANSFORMER.T_DESCRIPTION          IS 'Human-friendly description of what this transformer does.';

-- System-controlled lookup table of supported data types/encodings.
-- Not editable by end users; types are seeded by the application.
CREATE TABLE DT_DATA_TYPE (
    DT_UID  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    DT_CODE TEXT NOT NULL
);

-- Enforce global uniqueness of DT_CODE.
CREATE UNIQUE INDEX UIDX_DT_CODE ON DT_DATA_TYPE(DT_CODE);

COMMENT ON TABLE  DT_DATA_TYPE          IS 'System-controlled lookup table of supported data formats/encodings (e.g. CSV, JSON, MESSAGEPACK).';
COMMENT ON COLUMN DT_DATA_TYPE.DT_UID   IS 'UUID primary key. Auto-generated via gen_random_uuid() when not supplied on insert.';
COMMENT ON COLUMN DT_DATA_TYPE.DT_CODE  IS 'Short machine-readable code identifying the data type (e.g. ''CSV'', ''JSON'', ''MESSAGEPACK''). Globally unique.';

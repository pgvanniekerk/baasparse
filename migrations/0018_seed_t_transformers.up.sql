-- Seed the supported data transformers.
-- DT_UIDs are resolved by DT_CODE subquery so this migration is independent
-- of the auto-generated UUIDs produced by 0017_seed_dt_data_types.

-- DSV → MESSAGEPACK
-- Reads delimiter-separated rows and encodes each as a MessagePack map.
INSERT INTO T_TRANSFORMER (T_FROM_DT_UID, T_TO_DT_UID, T_TRANSFORMATION_SCHEMA, T_DESCRIPTION)
VALUES (
    (SELECT DT_UID FROM DT_DATA_TYPE WHERE DT_CODE = 'DSV'),
    (SELECT DT_UID FROM DT_DATA_TYPE WHERE DT_CODE = 'MESSAGEPACK'),
    $json${
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": "baasparse:transformer:dsv-to-messagepack",
        "title": "DSV to MessagePack",
        "description": "Configuration for parsing a delimiter-separated values file and encoding each data row as a MessagePack map.",
        "type": "object",
        "additionalProperties": false,
        "required": ["columns"],
        "properties": {
            "delimiter": {
                "type": "string",
                "description": "Column delimiter character.",
                "default": ","
            },
            "header_rows": {
                "type": "integer",
                "description": "Number of leading rows to skip as headers before processing data rows.",
                "minimum": 0,
                "default": 0
            },
            "columns": {
                "type": "array",
                "description": "Mapping of zero-based column indexes to MessagePack field names.",
                "minItems": 1,
                "items": {
                    "type": "object",
                    "additionalProperties": false,
                    "required": ["index", "name"],
                    "properties": {
                        "index": {
                            "type": "integer",
                            "description": "Zero-based column index in the DSV row.",
                            "minimum": 0
                        },
                        "name": {
                            "type": "string",
                            "description": "Key name to use for this field in the MessagePack record.",
                            "minLength": 1
                        },
                        "type": {
                            "type": "string",
                            "description": "Data type for MessagePack encoding. Defaults to string.",
                            "enum": ["string", "integer", "float", "boolean"],
                            "default": "string"
                        }
                    }
                }
            }
        }
    }$json$,
    'Parses delimiter-separated values and encodes each data row as a MessagePack map using the configured column index-to-name mappings.'
);

-- MESSAGEPACK → DSV
-- Reads a MessagePack map and writes its fields as a delimiter-separated row.
INSERT INTO T_TRANSFORMER (T_FROM_DT_UID, T_TO_DT_UID, T_TRANSFORMATION_SCHEMA, T_DESCRIPTION)
VALUES (
    (SELECT DT_UID FROM DT_DATA_TYPE WHERE DT_CODE = 'MESSAGEPACK'),
    (SELECT DT_UID FROM DT_DATA_TYPE WHERE DT_CODE = 'DSV'),
    $json${
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": "baasparse:transformer:messagepack-to-dsv",
        "title": "MessagePack to DSV",
        "description": "Configuration for encoding a MessagePack map as a delimiter-separated row.",
        "type": "object",
        "additionalProperties": false,
        "required": ["columns"],
        "properties": {
            "delimiter": {
                "type": "string",
                "description": "Column delimiter character.",
                "default": ","
            },
            "include_header": {
                "type": "boolean",
                "description": "Whether to write a header row as the first line of the output.",
                "default": false
            },
            "columns": {
                "type": "array",
                "description": "Ordered list of fields to write as columns. The array order determines the column order in the output.",
                "minItems": 1,
                "items": {
                    "type": "object",
                    "additionalProperties": false,
                    "required": ["name"],
                    "properties": {
                        "name": {
                            "type": "string",
                            "description": "Field name in the MessagePack record to write as a column.",
                            "minLength": 1
                        },
                        "header": {
                            "type": "string",
                            "description": "Column header label used when include_header is true. Defaults to name."
                        }
                    }
                }
            }
        }
    }$json$,
    'Encodes a MessagePack map as a delimiter-separated row using the configured field-to-column mappings.'
);

-- JSON → MESSAGEPACK
-- Reads a JSON object (or array of objects) and encodes each record as MessagePack.
INSERT INTO T_TRANSFORMER (T_FROM_DT_UID, T_TO_DT_UID, T_TRANSFORMATION_SCHEMA, T_DESCRIPTION)
VALUES (
    (SELECT DT_UID FROM DT_DATA_TYPE WHERE DT_CODE = 'JSON'),
    (SELECT DT_UID FROM DT_DATA_TYPE WHERE DT_CODE = 'MESSAGEPACK'),
    $json${
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": "baasparse:transformer:json-to-messagepack",
        "title": "JSON to MessagePack",
        "description": "Configuration for converting a JSON record or array of records to MessagePack.",
        "type": "object",
        "additionalProperties": false,
        "properties": {
            "root_path": {
                "type": "string",
                "description": "JSONPath expression pointing to the array of records within the document (e.g. \"$.records\"). Omit if the root element is the record or array to process.",
                "minLength": 1
            },
            "fields": {
                "type": "array",
                "description": "Field names to include in the MessagePack output. An empty array includes all fields.",
                "items": { "type": "string", "minLength": 1 },
                "default": []
            }
        }
    }$json$,
    'Converts a JSON object or array of objects to MessagePack. Supports optional field filtering and root path extraction.'
);

-- MESSAGEPACK → JSON
-- Decodes a MessagePack record and encodes it as a JSON object.
INSERT INTO T_TRANSFORMER (T_FROM_DT_UID, T_TO_DT_UID, T_TRANSFORMATION_SCHEMA, T_DESCRIPTION)
VALUES (
    (SELECT DT_UID FROM DT_DATA_TYPE WHERE DT_CODE = 'MESSAGEPACK'),
    (SELECT DT_UID FROM DT_DATA_TYPE WHERE DT_CODE = 'JSON'),
    $json${
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": "baasparse:transformer:messagepack-to-json",
        "title": "MessagePack to JSON",
        "description": "Configuration for decoding a MessagePack record and encoding it as JSON.",
        "type": "object",
        "additionalProperties": false,
        "properties": {
            "pretty": {
                "type": "boolean",
                "description": "Whether to pretty-print the JSON output.",
                "default": false
            },
            "fields": {
                "type": "array",
                "description": "Field names to include in the JSON output. An empty array includes all fields.",
                "items": { "type": "string", "minLength": 1 },
                "default": []
            }
        }
    }$json$,
    'Decodes a MessagePack record and encodes it as a JSON object. Supports optional pretty-printing and field filtering.'
);

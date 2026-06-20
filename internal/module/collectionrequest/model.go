// Package collectionrequest defines the domain model and persistence port for
// CR_COLLECTION_REQUEST — the durable record of each raw payload retrieved by a
// collector before any processing.
//
// Rows are written by the collection engine at ingestion time and are
// immutable: there is no update or delete. Persisting the raw bytes here means
// a failed run can be retried from this record without re-reading the source.
// CR_HASH_KEY is unique per (pipeline, collector) to prevent the same payload
// being ingested twice.
package collectionrequest

import (
	"time"

	"github.com/google/uuid"
)

// CollectionRequest represents a row in CR_COLLECTION_REQUEST.
type CollectionRequest struct {
	// Uid is the database-generated primary key (CR_UID).
	Uid uuid.UUID `json:"uid"`

	// PipelineUid is the FK to P_PIPELINE (CR_P_UID); the pipeline that
	// triggered this collection.
	PipelineUid uuid.UUID `json:"pipeline_uid"`

	// CollectorUid is the FK to C_COLLECTOR (CR_C_UID); the collector that
	// retrieved this payload.
	CollectorUid uuid.UUID `json:"collector_uid"`

	// HashKey is the content hash of Data (CR_HASH_KEY). It is unique per
	// (PipelineUid, CollectorUid) to prevent duplicate ingestion.
	HashKey string `json:"hash_key"`

	// FileName is the original file name of the collected artefact
	// (CR_FILE_NAME). Nil for non-file sources (e.g. database records, AMQP
	// messages).
	FileName *string `json:"file_name"`

	// Data is the raw binary content of the collected artefact (CR_DATA).
	Data []byte `json:"data"`

	// TimestampTz is the time the payload was received from the source
	// (CR_TIMESTAMPTZ). Assigned by the database on insert.
	TimestampTz time.Time `json:"timestamp_tz"`
}

// CreateCommand carries the inputs required to create a new CollectionRequest.
// The content hash (HashKey) is derived by the service from Data; callers do
// not supply it. Uid and TimestampTz are assigned by the database.
type CreateCommand struct {
	// PipelineUid is the pipeline that triggered this collection.
	PipelineUid uuid.UUID `json:"pipeline_uid"`

	// CollectorUid is the collector that retrieved the payload.
	CollectorUid uuid.UUID `json:"collector_uid"`

	// FileName is the optional original file name. Nil for non-file sources.
	FileName *string `json:"file_name"`

	// Data is the raw binary content of the collected artefact.
	Data []byte `json:"data"`
}

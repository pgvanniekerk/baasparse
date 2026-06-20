// Package transformeddata defines the domain model, persistence port, and
// application service for TD_TRANSFORMED_DATA — individual records parsed from a
// collection request and serialised as MessagePack, ready for distribution.
//
// Rows are written by the pipeline engine after it transforms a
// CR_COLLECTION_REQUEST payload (one row per logical record) and are immutable:
// there is no update or delete. Each row links back to both the originating
// collection request and the collector that produced it.
package transformeddata

import (
	"time"

	"github.com/google/uuid"
)

// TransformedData represents a row in TD_TRANSFORMED_DATA.
type TransformedData struct {
	// Uid is the database-generated primary key (TD_UID).
	Uid uuid.UUID `json:"uid"`

	// CollectionRequestUid is the FK to CR_COLLECTION_REQUEST (TD_CR_UID); the
	// collection request whose raw data this record was parsed from.
	CollectionRequestUid uuid.UUID `json:"collection_request_uid"`

	// CollectorUid is the FK to C_COLLECTOR (TD_C_UID); the collector that
	// retrieved the source payload. It always matches the collection request's
	// collector.
	CollectorUid uuid.UUID `json:"collector_uid"`

	// Data is the transformed record serialised as MessagePack (TD_DATA).
	Data []byte `json:"data"`

	// TimestampTz is the time this record was inserted (TD_TIMESTAMPTZ).
	// Assigned by the database on insert.
	TimestampTz time.Time `json:"timestamp_tz"`
}

// CreateCommand carries the inputs required to create a new TransformedData
// record. Uid and TimestampTz are assigned by the database.
type CreateCommand struct {
	// CollectionRequestUid is the collection request this record was parsed from.
	CollectionRequestUid uuid.UUID `json:"collection_request_uid"`

	// CollectorUid is the collector that retrieved the source payload. It must
	// match the referenced collection request's collector.
	CollectorUid uuid.UUID `json:"collector_uid"`

	// Data is the transformed record serialised as MessagePack.
	Data []byte `json:"data"`
}

// Package datasourcetype defines the domain model and persistence port for
// DST_TYPE — a catalog of application-defined data source types such as
// "LocalFileSystem" and "RemoteFileSystem".
//
// DST_TYPE rows are seeded at migration time and are read-only from the
// end-user's perspective. The Repository interface therefore exposes only
// query methods; no create/update/delete operations are supported.
package datasourcetype

import (
	"encoding/json"

	"github.com/google/uuid"
)

// DataSourceType represents a row in DST_TYPE.
// Each type defines three JSON Schema (draft 2020-12) documents that govern
// how a data source of this kind is configured:
//   - ConnectionSchema — credentials/address needed to reach the source.
//   - CollectionSchema — what to collect once connected (path, query, queue…).
//   - DistributionSchema — where to write results once connected.
//
// These schemas are validated in the service layer when a user configures a
// DS_DATA_SOURCE or a C_COLLECTOR / D_DISTRIBUTOR.
type DataSourceType struct {
	// Uid is the database-generated primary key (DST_UID).
	Uid uuid.UUID `json:"uid"`

	// Name is the unique human-readable type identifier, e.g. "LocalFileSystem"
	// (DST_NAME). Used as the stable lookup key in most service-layer calls.
	Name string `json:"name"`

	// ConnectionSchema is a JSON Schema document describing the fields required
	// to establish a connection to this source type (DST_CONNECTION_SCHEMA).
	// Stored verbatim from the database; validate with a JSON Schema library.
	ConnectionSchema json.RawMessage `json:"connection_schema"`

	// CollectionSchema is a JSON Schema document describing where/what to
	// collect from the source once connected (DST_COLLECTION_SCHEMA).
	CollectionSchema json.RawMessage `json:"collection_schema"`

	// DistributionSchema is a JSON Schema document describing where/how to
	// deliver data to the source once connected (DST_DISTRIBUTION_SCHEMA).
	DistributionSchema json.RawMessage `json:"distribution_schema"`
}

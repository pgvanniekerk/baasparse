// Package distributionrequest defines the domain model, persistence port, and
// application service for DR_DISTRIBUTION_REQUEST — a record of each attempt by
// a distributor to deliver a (possibly transformed) record to its target.
//
// The pipeline engine creates one row per (distributor, record) pair with
// StatusPending, then updates it to StatusSuccess or StatusFailed once delivery
// is attempted. TransformedDataUid is optional: a plain pass-through distributor
// (e.g. file transfer) has no transformed data.
package distributionrequest

import (
	"time"

	"github.com/google/uuid"
)

// Status is the delivery status of a distribution request (DR_STATUS). Only
// StatusPending, StatusSuccess, and StatusFailed are valid; the database
// enforces the same set via a CHECK constraint on DR_STATUS.
type Status string

const (
	// StatusPending marks a request that has been queued but not yet attempted.
	StatusPending Status = "PENDING"

	// StatusSuccess marks a request whose payload was delivered.
	StatusSuccess Status = "SUCCESS"

	// StatusFailed marks a request whose delivery failed; see FailureReason.
	StatusFailed Status = "FAILED"
)

// Valid reports whether s is a recognised distribution-request status.
func (s Status) Valid() bool {
	return s == StatusPending || s == StatusSuccess || s == StatusFailed
}

// DistributionRequest represents a row in DR_DISTRIBUTION_REQUEST.
type DistributionRequest struct {
	// Uid is the database-generated primary key (DR_UID).
	Uid uuid.UUID `json:"uid"`

	// DistributorUid is the FK to D_DISTRIBUTOR (DR_D_UID); the distributor that
	// attempted delivery.
	DistributorUid uuid.UUID `json:"distributor_uid"`

	// CollectionRequestUid is the FK to CR_COLLECTION_REQUEST (DR_CR_UID); the
	// original collection request this distribution is derived from.
	CollectionRequestUid uuid.UUID `json:"collection_request_uid"`

	// TransformedDataUid is the optional FK to TD_TRANSFORMED_DATA (DR_TD_UID);
	// the transformed record being distributed. Nil for plain pass-through
	// (e.g. file transfer) where no transformation occurs.
	TransformedDataUid *uuid.UUID `json:"transformed_data_uid"`

	// Content is the converted payload written (or attempted to be written) to
	// the destination (DR_CONTENT). Nil while PENDING.
	Content []byte `json:"content"`

	// Status is the delivery status (DR_STATUS).
	Status Status `json:"status"`

	// FailureReason is the human-readable error description when Status is
	// StatusFailed (DR_FAILURE_REASON). Nil otherwise.
	FailureReason *string `json:"failure_reason"`

	// TimestampTz is the time this request was created (DR_TIMESTAMPTZ).
	// Assigned by the database on insert.
	TimestampTz time.Time `json:"timestamp_tz"`
}

// CreateCommand carries the inputs required to create a new DistributionRequest.
// New requests always start with StatusPending and no content or failure reason;
// Uid and TimestampTz are assigned by the database.
type CreateCommand struct {
	// DistributorUid is the distributor that will attempt delivery.
	DistributorUid uuid.UUID `json:"distributor_uid"`

	// CollectionRequestUid is the originating collection request.
	CollectionRequestUid uuid.UUID `json:"collection_request_uid"`

	// TransformedDataUid optionally references the transformed record being
	// distributed. Nil for plain pass-through. When set, it must belong to the
	// referenced collection request.
	TransformedDataUid *uuid.UUID `json:"transformed_data_uid"`
}

// UpdateCommand carries the inputs required to record the outcome of a
// DistributionRequest.
type UpdateCommand struct {
	// Uid identifies the distribution request to update (DR_UID).
	Uid uuid.UUID `json:"uid"`

	// Status is the new delivery status. Must be a valid Status.
	Status Status `json:"status"`

	// Content is the converted payload that was (or was attempted to be)
	// written. Nil leaves DR_CONTENT NULL.
	Content []byte `json:"content"`

	// FailureReason describes the error. It must be provided if and only if
	// Status is StatusFailed.
	FailureReason *string `json:"failure_reason"`
}

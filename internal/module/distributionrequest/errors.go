package distributionrequest

import "errors"

// ErrNotFound is returned by Repository and Service methods when no matching
// DistributionRequest row exists. GetByUid and Update wrap this with the
// identifier that was looked up; use errors.Is for type checking.
var ErrNotFound = errors.New("DistributionRequest not found")

// ErrInvalidStatus is returned by Service.Update when the supplied status is not
// one of the recognised Status values (StatusPending, StatusSuccess,
// StatusFailed).
var ErrInvalidStatus = errors.New("DistributionRequest status is invalid")

// ErrFailureReasonMismatch is returned by Service.Update when the failure-reason
// pairing is invalid: a failure reason must be provided if and only if the
// status is StatusFailed.
var ErrFailureReasonMismatch = errors.New("DistributionRequest failure reason must be provided if and only if status is FAILED")

// ErrTransformedDataMismatch is returned by Service.Create when the referenced
// transformed data does not belong to the referenced collection request
// (TD_CR_UID must equal DR_CR_UID).
var ErrTransformedDataMismatch = errors.New("Transformed data does not belong to the specified collection request")

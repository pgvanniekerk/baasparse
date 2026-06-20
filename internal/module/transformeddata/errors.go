package transformeddata

import "errors"

// ErrNotFound is returned by Repository.GetByUid and Service.GetByUid when no
// matching TransformedData row exists. It is wrapped with the UID that was
// looked up; use errors.Is for type checking.
var ErrNotFound = errors.New("TransformedData not found")

// ErrCollectorMismatch is returned by Service.Create when the referenced
// collector does not match the collector recorded on the referenced collection
// request. TD_C_UID must equal the collection request's CR_C_UID.
var ErrCollectorMismatch = errors.New("Collector does not match the collection request's collector")

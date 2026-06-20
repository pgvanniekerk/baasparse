package datasource

import "errors"

// ErrNotFound is returned by Repository and Service methods when no matching
// DataSource row exists. GetByUid and GetByName wrap this with the identifier
// that was looked up; use errors.Is for type checking.
var ErrNotFound = errors.New("DataSource not found")

// ErrInvalidConnectionSpec is returned by Service.Create when the supplied
// ConnectionSpecification fails validation against the DataSourceType's
// ConnectionSchema. The error is wrapped with the validator's detail message,
// so err.Error() describes exactly which fields are invalid.
var ErrInvalidConnectionSpec = errors.New("DataSource connection specification is invalid")

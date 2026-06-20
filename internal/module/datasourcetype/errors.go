package datasourcetype

import "errors"

// ErrNotFound is returned by Repository methods when no matching DataSourceType
// row exists. GetByUid and GetByName wrap this error with the identifier that
// was looked up, so callers can use errors.Is for type checking while still
// getting a descriptive message from err.Error().
var ErrNotFound = errors.New("DataSourceType not found")

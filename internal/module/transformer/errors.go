package transformer

import "errors"

// ErrNotFound is returned by Repository methods when no matching Transformer
// row exists. The Get methods wrap this error with the identifiers that were
// looked up, so callers can use errors.Is for type checking while still
// getting a descriptive message from err.Error().
var ErrNotFound = errors.New("Transformer not found")

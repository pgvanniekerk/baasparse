package pipeline

import "errors"

// ErrNotFound is returned by Repository and Service methods when no matching
// Pipeline row exists. GetByUid and Update wrap this with the identifier that
// was looked up; use errors.Is for type checking.
var ErrNotFound = errors.New("Pipeline not found")

// ErrNameExists is returned by Service.Create and Service.Update when the
// supplied name is already used by another pipeline. P_NAME is globally unique.
var ErrNameExists = errors.New("Pipeline name already exists")

// ErrInvalidStatus is returned by Service.Update when the supplied status is
// not one of the recognised Status values (StatusActive, StatusInactive).
var ErrInvalidStatus = errors.New("Pipeline status is invalid")

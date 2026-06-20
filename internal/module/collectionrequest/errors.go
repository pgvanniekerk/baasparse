package collectionrequest

import "errors"

// ErrNotFound is returned by Repository.GetByUid when no matching
// CollectionRequest row exists. It is wrapped with the UID that was looked up;
// use errors.Is for type checking.
var ErrNotFound = errors.New("CollectionRequest not found")

// ErrAlreadyExists is returned by Service.Create when a payload with the same
// content hash has already been ingested for the (pipeline, collector) pair.
// Callers (e.g. HTTP handlers) may treat it as an idempotent no-op since the
// payload is already stored.
var ErrAlreadyExists = errors.New("CollectionRequest already exists for payload")

// ErrCollectorPipelineMismatch is returned by Service.Create when the referenced
// collector exists but belongs to a different pipeline than the one specified on
// the request. C_COLLECTOR.C_P_UID ties a collector to exactly one pipeline.
var ErrCollectorPipelineMismatch = errors.New("Collector does not belong to the specified pipeline")

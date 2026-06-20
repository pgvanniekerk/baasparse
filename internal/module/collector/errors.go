package collector

import "errors"

// ErrNotFound is returned by Repository and Service methods when no matching
// Collector row exists. GetByUid, GetByPUid, and Update wrap this with the
// identifier that was looked up; use errors.Is for type checking.
var ErrNotFound = errors.New("Collector not found")

// ErrCollectorExists is returned by Service.Create when the referenced pipeline
// already has a collector. C_P_UID is unique (1:1 pipeline:collector).
var ErrCollectorExists = errors.New("Collector already exists for pipeline")

// ErrInvalidStatus is returned by Service.Update when the supplied status is
// not one of the recognised Status values (StatusActive, StatusInactive).
var ErrInvalidStatus = errors.New("Collector status is invalid")

// ErrInvalidCollectionSpec is returned by the Service when the supplied
// CollectionSpecification fails validation against the data source type's
// DST_COLLECTION_SCHEMA. The error is wrapped with the validator's detail
// message.
var ErrInvalidCollectionSpec = errors.New("Collector collection specification is invalid")

// ErrInvalidTransformationSpec is returned by the Service when the supplied
// TransformationSpecification fails validation against the referenced
// transformer's T_TRANSFORMATION_SCHEMA. The error is wrapped with the
// validator's detail message.
var ErrInvalidTransformationSpec = errors.New("Collector transformation specification is invalid")

// ErrTransformationSpecMismatch is returned by the Service when the
// transformer/specification pairing is invalid: a transformation specification
// must be provided if and only if a transformer (TransformerUid) is set.
var ErrTransformationSpecMismatch = errors.New("Collector transformation specification must be provided if and only if a transformer is set")

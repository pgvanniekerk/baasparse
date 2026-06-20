package distributor

import "errors"

// ErrNotFound is returned by Repository and Service methods when no matching
// Distributor row exists. GetByUid and Update wrap this with the identifier
// that was looked up; use errors.Is for type checking.
var ErrNotFound = errors.New("Distributor not found")

// ErrNameExists is returned by Service.Create and Service.Update when the
// supplied name is already used by another distributor in the same pipeline.
// D_NAME is unique within a pipeline (UIDX_D_P_UID_NAME).
var ErrNameExists = errors.New("Distributor name already exists in pipeline")

// ErrInvalidStatus is returned by Service.Update when the supplied status is
// not one of the recognised Status values (StatusActive, StatusInactive).
var ErrInvalidStatus = errors.New("Distributor status is invalid")

// ErrInvalidDistributionSpec is returned by the Service when the supplied
// DistributionSpecification fails validation against the data source type's
// DST_DISTRIBUTION_SCHEMA. The error is wrapped with the validator's detail
// message.
var ErrInvalidDistributionSpec = errors.New("Distributor distribution specification is invalid")

// ErrInvalidTransformationSpec is returned by the Service when the supplied
// TransformationSpecification fails validation against the referenced
// transformer's T_TRANSFORMATION_SCHEMA. The error is wrapped with the
// validator's detail message.
var ErrInvalidTransformationSpec = errors.New("Distributor transformation specification is invalid")

// ErrTransformationSpecMismatch is returned by the Service when the
// transformer/specification pairing is invalid: a transformation specification
// must be provided if and only if a transformer (TransformerUid) is set.
var ErrTransformationSpecMismatch = errors.New("Distributor transformation specification must be provided if and only if a transformer is set")

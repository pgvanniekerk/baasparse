package transformer

// All queries select the full column list in the same order that scanRow
// scans them: T_UID, T_FROM_DT_UID, T_TO_DT_UID, T_TRANSFORMATION_SCHEMA,
// T_DESCRIPTION. If the column order ever changes here it must be updated in
// scanRow as well.
const (
	// sqlGetByUid looks up a single row by primary key (T_UID).
	// Returns at most one row because T_UID is the primary key.
	sqlGetByUid = `
		SELECT T_UID, T_FROM_DT_UID, T_TO_DT_UID, T_TRANSFORMATION_SCHEMA, T_DESCRIPTION
		FROM   T_TRANSFORMER
		WHERE  T_UID = $1`

	// sqlGetByFromAndTo looks up the single transformer for a (from, to) data
	// type pair. Returns at most one row because UIDX_T_FROM_DT_UID_TO_DT_UID
	// enforces uniqueness on (T_FROM_DT_UID, T_TO_DT_UID).
	sqlGetByFromAndTo = `
		SELECT T_UID, T_FROM_DT_UID, T_TO_DT_UID, T_TRANSFORMATION_SCHEMA, T_DESCRIPTION
		FROM   T_TRANSFORMER
		WHERE  T_FROM_DT_UID = $1 AND T_TO_DT_UID = $2`
)

package datasourcetype

// All queries select the full column list in the same order that scanRow
// scans them: DST_UID, DST_NAME, DST_CONNECTION_SCHEMA, DST_COLLECTION_SCHEMA,
// DST_DISTRIBUTION_SCHEMA. If the column order ever changes here it must be
// updated in scanRow as well.
const (
	// sqlGetByUid looks up a single row by primary key (DST_UID).
	// Returns at most one row because DST_UID is the primary key.
	sqlGetByUid = `
		SELECT DST_UID, DST_NAME, DST_CONNECTION_SCHEMA, DST_COLLECTION_SCHEMA, DST_DISTRIBUTION_SCHEMA
		FROM   DST_TYPE
		WHERE  DST_UID = $1`

	// sqlGetByName looks up a single row by the unique name column (DST_NAME).
	// Returns at most one row because UIDX_DST_NAME enforces uniqueness.
	sqlGetByName = `
		SELECT DST_UID, DST_NAME, DST_CONNECTION_SCHEMA, DST_COLLECTION_SCHEMA, DST_DISTRIBUTION_SCHEMA
		FROM   DST_TYPE
		WHERE  DST_NAME = $1`

	// sqlGetAll returns every row ordered alphabetically by name.
	// The result set is small and bounded (seeded at migration time), so no
	// pagination is applied.
	sqlGetAll = `
		SELECT DST_UID, DST_NAME, DST_CONNECTION_SCHEMA, DST_COLLECTION_SCHEMA, DST_DISTRIBUTION_SCHEMA
		FROM   DST_TYPE
		ORDER BY DST_NAME`
)

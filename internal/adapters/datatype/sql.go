package datatype

// All queries select the full column list in the same order that scanRow
// scans them: DT_UID, DT_CODE. If the column order ever changes here it must
// be updated in scanRow as well.
const (
	// sqlGetByUid looks up a single row by primary key (DT_UID).
	// Returns at most one row because DT_UID is the primary key.
	sqlGetByUid = `
		SELECT DT_UID, DT_CODE
		FROM   DT_DATA_TYPE
		WHERE  DT_UID = $1`

	// sqlGetByCode looks up a single row by the unique code column (DT_CODE).
	// Returns at most one row because UIDX_DT_CODE enforces uniqueness.
	sqlGetByCode = `
		SELECT DT_UID, DT_CODE
		FROM   DT_DATA_TYPE
		WHERE  DT_CODE = $1`

	// sqlGetAll returns every row ordered alphabetically by code.
	// The result set is small and bounded (seeded at migration time), so no
	// pagination is applied.
	sqlGetAll = `
		SELECT DT_UID, DT_CODE
		FROM   DT_DATA_TYPE
		ORDER BY DT_CODE`
)

package transformeddata

// All SELECT queries return columns in the same order that scanRow scans them:
// TD_UID, TD_CR_UID, TD_C_UID, TD_DATA, TD_TIMESTAMPTZ.
// If the column order ever changes here it must be updated in scanRow as well.
const (
	// sqlCreate inserts a new transformed-data record inside the caller's
	// transaction and returns the database-generated TD_UID and TD_TIMESTAMPTZ.
	sqlCreate = `
		INSERT INTO TD_TRANSFORMED_DATA
			(TD_CR_UID, TD_C_UID, TD_DATA)
		VALUES
			($1, $2, $3)
		RETURNING TD_UID, TD_TIMESTAMPTZ`

	// sqlGetByUid looks up a single row by primary key (TD_UID).
	sqlGetByUid = `
		SELECT TD_UID, TD_CR_UID, TD_C_UID, TD_DATA, TD_TIMESTAMPTZ
		FROM   TD_TRANSFORMED_DATA
		WHERE  TD_UID = $1`

	// sqlGetByCRUid returns all records for a collection request, oldest first.
	sqlGetByCRUid = `
		SELECT TD_UID, TD_CR_UID, TD_C_UID, TD_DATA, TD_TIMESTAMPTZ
		FROM   TD_TRANSFORMED_DATA
		WHERE  TD_CR_UID = $1
		ORDER BY TD_TIMESTAMPTZ`

	// sqlGetByCUid returns all records for a collector, oldest first.
	sqlGetByCUid = `
		SELECT TD_UID, TD_CR_UID, TD_C_UID, TD_DATA, TD_TIMESTAMPTZ
		FROM   TD_TRANSFORMED_DATA
		WHERE  TD_C_UID = $1
		ORDER BY TD_TIMESTAMPTZ`
)

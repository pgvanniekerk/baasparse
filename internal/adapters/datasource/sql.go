package datasource

// All SELECT queries return columns in the same order that scanRow scans them:
// DS_UID, DS_DST_UID, DS_NAME, DS_CONNECTION_SPECIFICATION, DS_TA_UID.
// If the column order ever changes here it must be updated in scanRow as well.
const (
	// sqlCreate inserts a new data source row inside the caller's transaction
	// and returns the database-generated primary key.
	// DS_CONNECTION_SPECIFICATION is a JSONB column; the value is passed as
	// json.RawMessage so pgx uses the JSON codec rather than BYTEA.
	sqlCreate = `
		INSERT INTO DS_DATA_SOURCE
			(DS_DST_UID, DS_NAME, DS_CONNECTION_SPECIFICATION, DS_TA_UID)
		VALUES
			($1, $2, $3, $4)
		RETURNING DS_UID`

	// sqlGetByUid looks up a single row by primary key (DS_UID).
	sqlGetByUid = `
		SELECT DS_UID, DS_DST_UID, DS_NAME, DS_CONNECTION_SPECIFICATION, DS_TA_UID
		FROM   DS_DATA_SOURCE
		WHERE  DS_UID = $1`

	// sqlGetByName looks up a single row by the unique name column (DS_NAME).
	// Returns at most one row because UIDX_DS_NAME enforces uniqueness.
	sqlGetByName = `
		SELECT DS_UID, DS_DST_UID, DS_NAME, DS_CONNECTION_SPECIFICATION, DS_TA_UID
		FROM   DS_DATA_SOURCE
		WHERE  DS_NAME = $1`

	// sqlUpdate modifies the name and connection specification of an existing
	// row. RETURNING DS_UID allows us to detect a missing row via pgx.ErrNoRows.
	sqlUpdate = `
		UPDATE DS_DATA_SOURCE
		SET    DS_NAME = $1, DS_CONNECTION_SPECIFICATION = $2
		WHERE  DS_UID = $3
		RETURNING DS_UID`

	// sqlGetAll returns every row ordered alphabetically by name.
	sqlGetAll = `
		SELECT DS_UID, DS_DST_UID, DS_NAME, DS_CONNECTION_SPECIFICATION, DS_TA_UID
		FROM   DS_DATA_SOURCE
		ORDER BY DS_NAME`
)

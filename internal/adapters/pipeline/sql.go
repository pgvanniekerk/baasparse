package pipeline

// All SELECT queries return columns in the same order that scanRow scans them:
// P_UID, P_NAME, P_DESCRIPTION, P_STATUS, P_TA_UID.
// If the column order ever changes here it must be updated in scanRow as well.
const (
	// sqlCreate inserts a new pipeline row inside the caller's transaction and
	// returns the database-generated primary key. P_STATUS is supplied
	// explicitly by the service (ACTIVE on creation).
	sqlCreate = `
		INSERT INTO P_PIPELINE
			(P_NAME, P_DESCRIPTION, P_STATUS, P_TA_UID)
		VALUES
			($1, $2, $3, $4)
		RETURNING P_UID`

	// sqlUpdate modifies the name, description, and status of an existing row.
	// RETURNING P_UID allows us to detect a missing row via pgx.ErrNoRows.
	sqlUpdate = `
		UPDATE P_PIPELINE
		SET    P_NAME = $1, P_DESCRIPTION = $2, P_STATUS = $3
		WHERE  P_UID = $4
		RETURNING P_UID`

	// sqlGetByUid looks up a single row by primary key (P_UID).
	sqlGetByUid = `
		SELECT P_UID, P_NAME, P_DESCRIPTION, P_STATUS, P_TA_UID
		FROM   P_PIPELINE
		WHERE  P_UID = $1`

	// sqlGetByNameLike returns rows whose name matches the supplied SQL LIKE
	// pattern (wildcards provided by the caller), ordered alphabetically.
	sqlGetByNameLike = `
		SELECT P_UID, P_NAME, P_DESCRIPTION, P_STATUS, P_TA_UID
		FROM   P_PIPELINE
		WHERE  P_NAME LIKE $1
		ORDER BY P_NAME`

	// sqlGetAll returns every row ordered alphabetically by name.
	sqlGetAll = `
		SELECT P_UID, P_NAME, P_DESCRIPTION, P_STATUS, P_TA_UID
		FROM   P_PIPELINE
		ORDER BY P_NAME`

	// sqlGetAllByStatus returns rows with the given status, ordered by name.
	sqlGetAllByStatus = `
		SELECT P_UID, P_NAME, P_DESCRIPTION, P_STATUS, P_TA_UID
		FROM   P_PIPELINE
		WHERE  P_STATUS = $1
		ORDER BY P_NAME`

	// sqlExistsByName reports whether a row with the exact name exists. Used to
	// enforce P_NAME uniqueness before an insert or rename.
	sqlExistsByName = `
		SELECT EXISTS (SELECT 1 FROM P_PIPELINE WHERE P_NAME = $1)`
)

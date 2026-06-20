package distributor

// All SELECT queries return columns in the same order that scanRow scans them:
// D_UID, D_P_UID, D_DS_UID, D_NAME, D_DISTRIBUTION_SPECIFICATION, D_T_UID,
// D_TRANSFORMATION_SPECIFICATION, D_STATUS, D_TA_UID.
// If the column order ever changes here it must be updated in scanRow as well.
const (
	// sqlCreate inserts a new distributor row inside the caller's transaction
	// and returns the database-generated primary key. D_T_UID and
	// D_TRANSFORMATION_SPECIFICATION may be NULL (plain pass-through).
	sqlCreate = `
		INSERT INTO D_DISTRIBUTOR
			(D_P_UID, D_DS_UID, D_NAME, D_DISTRIBUTION_SPECIFICATION, D_T_UID, D_TRANSFORMATION_SPECIFICATION, D_STATUS, D_TA_UID)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING D_UID`

	// sqlUpdate modifies the data source, name, specifications, transformer,
	// and status of an existing row. The pipeline link (D_P_UID) and audit FK
	// (D_TA_UID) are never changed. RETURNING D_UID lets us detect a missing
	// row via pgx.ErrNoRows.
	sqlUpdate = `
		UPDATE D_DISTRIBUTOR
		SET    D_DS_UID = $1,
		       D_NAME = $2,
		       D_DISTRIBUTION_SPECIFICATION = $3,
		       D_T_UID = $4,
		       D_TRANSFORMATION_SPECIFICATION = $5,
		       D_STATUS = $6
		WHERE  D_UID = $7
		RETURNING D_UID`

	// sqlGetByUid looks up a single row by primary key (D_UID).
	sqlGetByUid = `
		SELECT D_UID, D_P_UID, D_DS_UID, D_NAME, D_DISTRIBUTION_SPECIFICATION, D_T_UID, D_TRANSFORMATION_SPECIFICATION, D_STATUS, D_TA_UID
		FROM   D_DISTRIBUTOR
		WHERE  D_UID = $1`

	// sqlGetByPUid returns all distributors for a pipeline, ordered by name.
	// A pipeline may have many distributors, so this can return multiple rows.
	sqlGetByPUid = `
		SELECT D_UID, D_P_UID, D_DS_UID, D_NAME, D_DISTRIBUTION_SPECIFICATION, D_T_UID, D_TRANSFORMATION_SPECIFICATION, D_STATUS, D_TA_UID
		FROM   D_DISTRIBUTOR
		WHERE  D_P_UID = $1
		ORDER BY D_NAME`

	// sqlGetAll returns every row ordered by pipeline UID then name.
	sqlGetAll = `
		SELECT D_UID, D_P_UID, D_DS_UID, D_NAME, D_DISTRIBUTION_SPECIFICATION, D_T_UID, D_TRANSFORMATION_SPECIFICATION, D_STATUS, D_TA_UID
		FROM   D_DISTRIBUTOR
		ORDER BY D_P_UID, D_NAME`

	// sqlGetAllByStatus returns rows with the given status, ordered by pipeline
	// UID then name.
	sqlGetAllByStatus = `
		SELECT D_UID, D_P_UID, D_DS_UID, D_NAME, D_DISTRIBUTION_SPECIFICATION, D_T_UID, D_TRANSFORMATION_SPECIFICATION, D_STATUS, D_TA_UID
		FROM   D_DISTRIBUTOR
		WHERE  D_STATUS = $1
		ORDER BY D_P_UID, D_NAME`

	// sqlExistsByPUidAndName reports whether a distributor with the given name
	// already exists within the given pipeline. Used to enforce per-pipeline
	// name uniqueness before an insert or rename.
	sqlExistsByPUidAndName = `
		SELECT EXISTS (SELECT 1 FROM D_DISTRIBUTOR WHERE D_P_UID = $1 AND D_NAME = $2)`
)

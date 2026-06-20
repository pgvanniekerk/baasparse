package collector

// All SELECT queries return columns in the same order that scanRow scans them:
// C_UID, C_P_UID, C_DS_UID, C_COLLECTION_SPECIFICATION, C_T_UID,
// C_TRANSFORMATION_SPECIFICATION, C_STATUS, C_TA_UID.
// If the column order ever changes here it must be updated in scanRow as well.
const (
	// sqlCreate inserts a new collector row inside the caller's transaction and
	// returns the database-generated primary key. C_T_UID and
	// C_TRANSFORMATION_SPECIFICATION may be NULL (plain pass-through collector).
	sqlCreate = `
		INSERT INTO C_COLLECTOR
			(C_P_UID, C_DS_UID, C_COLLECTION_SPECIFICATION, C_T_UID, C_TRANSFORMATION_SPECIFICATION, C_STATUS, C_TA_UID)
		VALUES
			($1, $2, $3, $4, $5, $6, $7)
		RETURNING C_UID`

	// sqlUpdate modifies the data source, specifications, transformer, and
	// status of an existing row. The pipeline link (C_P_UID) and audit FK
	// (C_TA_UID) are never changed. RETURNING C_UID lets us detect a missing
	// row via pgx.ErrNoRows.
	sqlUpdate = `
		UPDATE C_COLLECTOR
		SET    C_DS_UID = $1,
		       C_COLLECTION_SPECIFICATION = $2,
		       C_T_UID = $3,
		       C_TRANSFORMATION_SPECIFICATION = $4,
		       C_STATUS = $5
		WHERE  C_UID = $6
		RETURNING C_UID`

	// sqlGetByUid looks up a single row by primary key (C_UID).
	sqlGetByUid = `
		SELECT C_UID, C_P_UID, C_DS_UID, C_COLLECTION_SPECIFICATION, C_T_UID, C_TRANSFORMATION_SPECIFICATION, C_STATUS, C_TA_UID
		FROM   C_COLLECTOR
		WHERE  C_UID = $1`

	// sqlGetByPUid looks up the single collector for a pipeline (C_P_UID).
	// Returns at most one row because UIDX_C_P_UID enforces uniqueness.
	sqlGetByPUid = `
		SELECT C_UID, C_P_UID, C_DS_UID, C_COLLECTION_SPECIFICATION, C_T_UID, C_TRANSFORMATION_SPECIFICATION, C_STATUS, C_TA_UID
		FROM   C_COLLECTOR
		WHERE  C_P_UID = $1`

	// sqlGetAll returns every row ordered by pipeline UID.
	sqlGetAll = `
		SELECT C_UID, C_P_UID, C_DS_UID, C_COLLECTION_SPECIFICATION, C_T_UID, C_TRANSFORMATION_SPECIFICATION, C_STATUS, C_TA_UID
		FROM   C_COLLECTOR
		ORDER BY C_P_UID`

	// sqlGetAllByStatus returns rows with the given status, ordered by pipeline UID.
	sqlGetAllByStatus = `
		SELECT C_UID, C_P_UID, C_DS_UID, C_COLLECTION_SPECIFICATION, C_T_UID, C_TRANSFORMATION_SPECIFICATION, C_STATUS, C_TA_UID
		FROM   C_COLLECTOR
		WHERE  C_STATUS = $1
		ORDER BY C_P_UID`

	// sqlExistsByPUid reports whether a collector already exists for a pipeline.
	// Used to enforce the 1:1 pipeline:collector relationship before an insert.
	sqlExistsByPUid = `
		SELECT EXISTS (SELECT 1 FROM C_COLLECTOR WHERE C_P_UID = $1)`
)

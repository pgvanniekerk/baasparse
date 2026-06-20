package collectionrequest

// All SELECT queries return columns in the same order that scanRow scans them:
// CR_UID, CR_P_UID, CR_C_UID, CR_HASH_KEY, CR_FILE_NAME, CR_DATA, CR_TIMESTAMPTZ.
// If the column order ever changes here it must be updated in scanRow as well.
const (
	// sqlCreate inserts a new collection request inside the caller's
	// transaction and returns the database-generated primary key. CR_UID and
	// CR_TIMESTAMPTZ are assigned by the database.
	sqlCreate = `
		INSERT INTO CR_COLLECTION_REQUEST
			(CR_P_UID, CR_C_UID, CR_HASH_KEY, CR_FILE_NAME, CR_DATA)
		VALUES
			($1, $2, $3, $4, $5)
		RETURNING CR_UID, CR_TIMESTAMPTZ`

	// sqlGetByUid looks up a single row by primary key (CR_UID).
	sqlGetByUid = `
		SELECT CR_UID, CR_P_UID, CR_C_UID, CR_HASH_KEY, CR_FILE_NAME, CR_DATA, CR_TIMESTAMPTZ
		FROM   CR_COLLECTION_REQUEST
		WHERE  CR_UID = $1`

	// sqlGetByPUid returns all requests for a pipeline, oldest first.
	sqlGetByPUid = `
		SELECT CR_UID, CR_P_UID, CR_C_UID, CR_HASH_KEY, CR_FILE_NAME, CR_DATA, CR_TIMESTAMPTZ
		FROM   CR_COLLECTION_REQUEST
		WHERE  CR_P_UID = $1
		ORDER BY CR_TIMESTAMPTZ`

	// sqlGetByCUid returns all requests for a collector, oldest first.
	sqlGetByCUid = `
		SELECT CR_UID, CR_P_UID, CR_C_UID, CR_HASH_KEY, CR_FILE_NAME, CR_DATA, CR_TIMESTAMPTZ
		FROM   CR_COLLECTION_REQUEST
		WHERE  CR_C_UID = $1
		ORDER BY CR_TIMESTAMPTZ`

	// sqlExistsByPUidAndCUidAndHashKey reports whether a payload with the given
	// content hash has already been ingested for the (pipeline, collector)
	// pair. Backed by UIDX_CR_P_UID_C_UID_HASH_KEY.
	sqlExistsByPUidAndCUidAndHashKey = `
		SELECT EXISTS (SELECT 1 FROM CR_COLLECTION_REQUEST WHERE CR_P_UID = $1 AND CR_C_UID = $2 AND CR_HASH_KEY = $3)`
)

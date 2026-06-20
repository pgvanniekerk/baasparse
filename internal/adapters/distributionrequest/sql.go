package distributionrequest

// All SELECT queries return columns in the same order that scanRow scans them:
// DR_UID, DR_D_UID, DR_CR_UID, DR_TD_UID, DR_CONTENT, DR_STATUS,
// DR_FAILURE_REASON, DR_TIMESTAMPTZ.
// If the column order ever changes here it must be updated in scanRow as well.
const (
	// sqlCreate inserts a new distribution request inside the caller's
	// transaction and returns the database-generated DR_UID and DR_TIMESTAMPTZ.
	// DR_TD_UID may be NULL (plain pass-through). DR_CONTENT and
	// DR_FAILURE_REASON default to NULL while the request is PENDING.
	sqlCreate = `
		INSERT INTO DR_DISTRIBUTION_REQUEST
			(DR_D_UID, DR_CR_UID, DR_TD_UID, DR_STATUS)
		VALUES
			($1, $2, $3, $4)
		RETURNING DR_UID, DR_TIMESTAMPTZ`

	// sqlUpdate records the outcome of a request: status, content, and failure
	// reason. RETURNING DR_UID lets us detect a missing row via pgx.ErrNoRows.
	sqlUpdate = `
		UPDATE DR_DISTRIBUTION_REQUEST
		SET    DR_STATUS = $1, DR_CONTENT = $2, DR_FAILURE_REASON = $3
		WHERE  DR_UID = $4
		RETURNING DR_UID`

	// sqlGetByUid looks up a single row by primary key (DR_UID).
	sqlGetByUid = `
		SELECT DR_UID, DR_D_UID, DR_CR_UID, DR_TD_UID, DR_CONTENT, DR_STATUS, DR_FAILURE_REASON, DR_TIMESTAMPTZ
		FROM   DR_DISTRIBUTION_REQUEST
		WHERE  DR_UID = $1`

	// sqlGetByDUid returns all requests for a distributor, oldest first.
	sqlGetByDUid = `
		SELECT DR_UID, DR_D_UID, DR_CR_UID, DR_TD_UID, DR_CONTENT, DR_STATUS, DR_FAILURE_REASON, DR_TIMESTAMPTZ
		FROM   DR_DISTRIBUTION_REQUEST
		WHERE  DR_D_UID = $1
		ORDER BY DR_TIMESTAMPTZ`

	// sqlGetByCRUid returns all requests for a collection request, oldest first.
	sqlGetByCRUid = `
		SELECT DR_UID, DR_D_UID, DR_CR_UID, DR_TD_UID, DR_CONTENT, DR_STATUS, DR_FAILURE_REASON, DR_TIMESTAMPTZ
		FROM   DR_DISTRIBUTION_REQUEST
		WHERE  DR_CR_UID = $1
		ORDER BY DR_TIMESTAMPTZ`

	// sqlGetByTDUid returns all requests for a transformed record, oldest first.
	sqlGetByTDUid = `
		SELECT DR_UID, DR_D_UID, DR_CR_UID, DR_TD_UID, DR_CONTENT, DR_STATUS, DR_FAILURE_REASON, DR_TIMESTAMPTZ
		FROM   DR_DISTRIBUTION_REQUEST
		WHERE  DR_TD_UID = $1
		ORDER BY DR_TIMESTAMPTZ`
)

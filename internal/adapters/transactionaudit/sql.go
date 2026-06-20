package transactionaudit

const (
	// sqlCreate inserts a new audit row inside the caller's transaction and
	// returns the database-generated primary key and timestamp.
	sqlCreate = `
		INSERT INTO TA_TRANSACTION_AUDIT
			(TA_USERNAME, TA_TRANSACTION_TYPE, TA_DATA, TA_CORRELATION_ID)
		VALUES
			($1, $2, $3, $4)
		RETURNING TA_UID, TA_TIMESTAMPTZ`

	// sqlGetByUser fetches audit rows for a specific user within a time window,
	// ordered newest-first with limit/offset pagination.
	sqlGetByUser = `
		SELECT TA_UID, TA_TIMESTAMPTZ, TA_USERNAME, TA_TRANSACTION_TYPE, TA_CORRELATION_ID, TA_DATA
		FROM   TA_TRANSACTION_AUDIT
		WHERE  TA_USERNAME = $1
		  AND  TA_TIMESTAMPTZ >= $2
		  AND  TA_TIMESTAMPTZ <= $3
		ORDER BY TA_TIMESTAMPTZ DESC
		LIMIT $4 OFFSET $5`

	// sqlGetByTransactionType fetches audit rows of a specific type within a
	// time window, ordered newest-first with limit/offset pagination.
	sqlGetByTransactionType = `
		SELECT TA_UID, TA_TIMESTAMPTZ, TA_USERNAME, TA_TRANSACTION_TYPE, TA_CORRELATION_ID, TA_DATA
		FROM   TA_TRANSACTION_AUDIT
		WHERE  TA_TRANSACTION_TYPE = $1
		  AND  TA_TIMESTAMPTZ >= $2
		  AND  TA_TIMESTAMPTZ <= $3
		ORDER BY TA_TIMESTAMPTZ DESC
		LIMIT $4 OFFSET $5`

	// sqlGetByCorrelationID fetches all audit rows sharing a correlation ID,
	// ordered newest-first with limit/offset pagination.
	sqlGetByCorrelationID = `
		SELECT TA_UID, TA_TIMESTAMPTZ, TA_USERNAME, TA_TRANSACTION_TYPE, TA_CORRELATION_ID, TA_DATA
		FROM   TA_TRANSACTION_AUDIT
		WHERE  TA_CORRELATION_ID = $1
		ORDER BY TA_TIMESTAMPTZ DESC
		LIMIT $2 OFFSET $3`
)

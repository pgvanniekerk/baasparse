// Package db provides shared infrastructure utilities for pgx-backed adapters.
//
// The primary purpose of this package is to allow a service-layer caller to
// attach an active pgx.Tx to a context.Context (WithTx) so that multiple
// repository calls within the same logical operation can participate in a
// single database transaction without the module (domain) layer ever importing
// pgx directly.
//
// Typical usage in a service method:
//
//	tx, _ := pool.Begin(ctx)
//	defer tx.Rollback(ctx)              // no-op after Commit
//	ctx = db.WithTx(ctx, tx)
//	_ = auditRepo.CreateTransaction(ctx, entry)   // uses the tx
//	_ = dataSourceRepo.Create(ctx, ds)            // uses the same tx
//	tx.Commit(ctx)
package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ErrNoTransaction is returned by adapter methods that require a live pgx.Tx
// in the context (e.g. CreateTransaction) when none has been stored by WithTx.
// This is a programming error: the caller must always begin a transaction and
// call WithTx before invoking transactional repository methods.
var ErrNoTransaction = errors.New("db: no transaction in context")

// txKey is the unexported context key type used to store a pgx.Tx.
// Using a package-private type prevents key collisions with other packages.
type txKey struct{}

// WithTx attaches tx to ctx and returns the new context.
// Pass the returned context to any repository method that must run within
// the same transaction.
func WithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// TxFromContext retrieves the pgx.Tx stored by WithTx.
// Returns the transaction and true if one is present; nil and false otherwise.
// Adapters call this at the start of every transactional method.
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	return tx, ok
}

// TxBeginner can begin a new database transaction.
// *pgxpool.Pool satisfies this interface, so service wrappers that manage
// transaction lifecycle (e.g. txService) accept TxBeginner rather than the
// concrete pool type, keeping infrastructure out of the module layer.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

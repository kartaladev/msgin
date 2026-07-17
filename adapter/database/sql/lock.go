package sql

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"time"
)

// LockDialect is the OPTIONAL lock/FOR-UPDATE strategy extension of LeaseDialect
// (ADR 0010 D5). It is segregated from the core LeaseDialect (interface-segregation:
// a lease-only custom LeaseDialect is not forced to implement lock SQL) and exported
// so a wire-compatible derivative can supply its own lock semantics. The
// built-ins postgres.LeaseDialect() and MySQLDialect() implement it, so
// WithStrategy(StrategyLockForUpdate) works out of the box; a custom LeaseDialect that
// does not also satisfy LockDialect is rejected at construction with
// ErrLockStrategyUnsupported.
//
// The transaction carried in a LockedRow is BEGUN by ClaimLock and COMMITTED by
// AckLock or NackLock; on any error along the way the implementation rolls it
// back so a pooled connection is never leaked. Unlike the lease strategy there
// is no lease_epoch fence and no lease-expiry predicate: the open FOR UPDATE
// transaction IS the exclusive lock, released only by commit (settle) or a
// process crash (auto-rollback).
type LockDialect interface {
	// ClaimLock begins a transaction on ctx and, within it, claims at most ONE
	// visible, unlocked row via SELECT ... FOR UPDATE SKIP LOCKED and bumps its
	// delivery_count, returning the row with the OPEN transaction carried in
	// LockedRow.Tx (to be committed by AckLock/NackLock). It returns (nil, nil)
	// when no row is claimable (rolling the transaction back first). On ANY error
	// it rolls the transaction back before returning (nil, err), so a discarded
	// claim never leaks a connection (the Poll no-partial-result / owns-cleanup
	// contract). The caller (the Source) passes a cancellation-detached context
	// (context.WithoutCancel) so graceful shutdown does not auto-rollback an
	// in-flight claim before the drain deadline (ADR 0010 D5, HIGH 3).
	ClaimLock(ctx context.Context, q Querier, table, lockedBy string) (*LockedRow, error)

	// AckLock settles lr by DELETEing its row on lr.Tx and committing the carried
	// transaction. On any error it rolls the transaction back (releasing the
	// pooled connection) and returns the error.
	AckLock(ctx context.Context, lr *LockedRow, table string) error

	// NackLock returns lr to the queue: it clears the lock and sets
	// visible_after to now()+delay on lr.Tx, then ALWAYS commits the carried
	// transaction (delay may be 0). Committing persists the delivery_count++ made
	// at claim and releases the FOR UPDATE lock — the lock strategy has no
	// business writes to undo, so Nack never rolls back a successful update. On an
	// UPDATE or commit error it rolls back (releasing the pooled connection) and
	// returns the error.
	NackLock(ctx context.Context, lr *LockedRow, table string, delay time.Duration) error
}

// LockedRow is a lock-strategy claim (ADR 0010 D5): the persisted envelope plus
// the transaction that spans claim → settle. Headers and Payload are the raw
// framed bytes (JSON headers, wire body); the runtime decodes the payload.
// DeliveryCount is the post-increment claim count (populates the
// msgin.delivery-count header). Tx is the open FOR UPDATE transaction owning the
// row; it is committed by AckLock/NackLock and MUST NOT be used after settling.
type LockedRow struct {
	ID            int64
	MsgID         string
	Headers       []byte
	Payload       []byte
	DeliveryCount int
	Tx            *stdsql.Tx
}

// Compile-time assertion that the built-in mysql dialect implements the
// segregated lock SPI, so WithStrategy(StrategyLockForUpdate) accepts it. The
// postgres dialect's identical assertion lives in its own module
// (adapter/database/sql/postgres, Plan 006 Task 4).
var _ LockDialect = mysqlDialect{}

// txBeginner is the capability a Querier must have to BEGIN a new transaction:
// *sql.DB satisfies it (its BeginTx); *sql.Tx does not (a tx cannot nest one),
// which is exactly how BeginLockTx (and the MySQL two-step Claim) tells the
// pool apart from an already-open transaction. It lives in the engine (moved
// from the MySQL dialect) so BeginLockTx is self-contained here; each dialect
// module declares its own structurally-identical local txBeginner where it
// needs one (Go interfaces are structural, so no shared type is required).
type txBeginner interface {
	BeginTx(ctx context.Context, opts *stdsql.TxOptions) (*stdsql.Tx, error)
}

// BeginLockTx is part of the LockDialect-author SPI (ADR 0010 D5, ADR 0011): it
// resolves the transaction that will carry a lock delivery from q, the Querier a
// dialect's ClaimLock receives. It is the shared helper every LockDialect
// implementation's ClaimLock calls to open its carried transaction.
//
//   - q is the pool (*sql.DB, the txBeginner path — the Source always passes the
//     pool): begins a NEW transaction on ctx.
//   - q is already an open *sql.Tx (a defensive/future path): carried directly,
//     unchanged — the caller retains ownership of committing/rolling it back.
//   - q is neither: a clear, wrapped error naming the actual type — never a
//     panic or a silently-nil transaction.
//
// Neither of the first two cases is an error. ctx drives BeginTx, so the caller
// controls the transaction's cancellation lifetime; the Source passes a
// cancellation-detached context so graceful shutdown does not auto-rollback an
// in-flight claim before the drain deadline (ADR 0010 D5 HIGH 3).
func BeginLockTx(ctx context.Context, q Querier) (*stdsql.Tx, error) {
	if b, ok := q.(txBeginner); ok {
		return b.BeginTx(ctx, nil)
	}
	if tx, ok := q.(*stdsql.Tx); ok {
		return tx, nil
	}
	return nil, fmt.Errorf(
		"msgin/sql: lock strategy ClaimLock requires a *sql.DB or *sql.Tx Querier, got %T", q)
}

// SettleLockTx is part of the LockDialect-author SPI (ADR 0010 D5, ADR 0011): it
// runs the settle statement on the carried transaction tx (opened by
// BeginLockTx) and commits it, closing the transaction exactly once. Every
// LockDialect implementation's AckLock/NackLock calls this to settle.
//
// The contract a dialect author relies on: this ALWAYS commits on success and
// ALWAYS rolls back on any error, so a dialect's AckLock/NackLock never leaks a
// pooled connection regardless of which statement or commit fails. This is the
// lock strategy's ALWAYS-COMMIT settle path (ADR 0010 D5): AckLock (DELETE) and
// NackLock (clear-lock UPDATE) both commit on success, because the strategy has
// no business writes to undo — a successful settle persists (the Ack's delete or
// the Nack's delivery_count++/visible_after) and releases the FOR UPDATE lock.
func SettleLockTx(ctx context.Context, tx *stdsql.Tx, query string, args ...any) error {
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback() // no-op after a failed commit, but explicit: never leak the conn
		return err
	}
	return nil
}
